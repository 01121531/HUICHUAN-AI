package channel

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	common2 "github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/constant"
	"github.com/01121531/HUICHUAN-AI/model"
	relaycommon "github.com/01121531/HUICHUAN-AI/relay/common"
	"github.com/01121531/HUICHUAN-AI/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDoRequestRecordsEveryManagedProxyUpstreamAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, model.DB.AutoMigrate(
		&model.ProxyGroup{}, &model.Proxy{}, &model.ChannelProxyBinding{}, &model.ProxyUpstreamAttempt{},
	))
	var calls atomic.Int32
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		call := calls.Add(1)
		if call == 3 {
			connection, _, hijackErr := w.(http.Hijacker).Hijack()
			if hijackErr == nil {
				_ = connection.Close()
			}
			return
		}
		w.Header().Set(common2.RequestIdKey, "upstream-"+strconv.Itoa(int(call)))
		if call == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer proxyServer.Close()
	parsed, err := url.Parse(proxyServer.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)

	const channelId = 99301
	group := &model.ProxyGroup{Name: "attempt-test", Enabled: true, Status: model.ProxyGroupStatusAvailable}
	require.NoError(t, model.DB.Create(group).Error)
	proxy := &model.Proxy{
		GroupId: group.Id, Name: "attempt-proxy", Protocol: "http",
		Host: parsed.Hostname(), Port: port, Enabled: true, Status: model.ProxyStatusAvailable,
	}
	require.NoError(t, model.DB.Create(proxy).Error)
	require.NoError(t, model.DB.Model(group).UpdateColumn("current_proxy_id", proxy.Id).Error)
	binding := &model.ChannelProxyBinding{ChannelId: channelId, ProxyGroupId: group.Id, Enabled: true}
	require.NoError(t, model.DB.Create(binding).Error)
	service.InvalidateChannelProxyConfig(channelId)
	t.Cleanup(func() {
		service.InvalidateChannelProxyConfig(channelId)
		service.ResetProxyClientCache()
		model.DB.Where("request_id = ?", "proxy-attempt-request").Delete(&model.ProxyUpstreamAttempt{})
		model.DB.Delete(binding)
		model.DB.Delete(proxy)
		model.DB.Delete(group)
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{}"))
	info := &relaycommon.RelayInfo{
		RequestId:   "proxy-attempt-request",
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelId},
	}
	for retryIndex := 0; retryIndex < 3; retryIndex++ {
		info.RetryIndex = retryIndex
		req, requestErr := http.NewRequest(http.MethodPost, "http://upstream.invalid/v1/chat/completions", io.NopCloser(strings.NewReader("{}")))
		require.NoError(t, requestErr)
		resp, requestErr := doRequest(c, req, info)
		if retryIndex < 2 {
			require.NoError(t, requestErr)
			require.NotNil(t, resp)
			_ = resp.Body.Close()
		} else {
			require.Error(t, requestErr)
			require.Nil(t, resp)
		}
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{}"))
	}

	attempts, err := model.ListProxyUpstreamAttempts(proxy.Id, "proxy-attempt-request", 10)
	require.NoError(t, err)
	require.Len(t, attempts, 3)
	require.Equal(t, 3, attempts[0].AttemptSequence)
	require.Equal(t, 2, attempts[0].RetryIndex)
	require.Equal(t, model.ProxyAttemptResultNetworkError, attempts[0].Result)
	require.Equal(t, "transport_error", attempts[0].FailureReason)
	require.Equal(t, 2, attempts[1].AttemptSequence)
	require.Equal(t, 1, attempts[1].RetryIndex)
	require.Equal(t, http.StatusOK, attempts[1].HttpStatus)
	require.Equal(t, model.ProxyAttemptResultSuccess, attempts[1].Result)
	require.Equal(t, "upstream-2", attempts[1].UpstreamRequestId)
	require.Equal(t, 1, attempts[2].AttemptSequence)
	require.Equal(t, 0, attempts[2].RetryIndex)
	require.Equal(t, http.StatusServiceUnavailable, attempts[2].HttpStatus)
	require.Equal(t, model.ProxyAttemptResultHTTPError, attempts[2].Result)
	require.Equal(t, "http_status_503", attempts[2].FailureReason)
	require.Equal(t, proxy.Id, common2.GetContextKeyInt(c, constant.ContextKeyProxyId))
}

func TestProcessHeaderOverride_ChannelTestSkipsPassthroughRules(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"*": "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Empty(t, headers)
}

func TestProcessHeaderOverride_ChannelTestSkipsClientHeaderPlaceholder(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Upstream-Trace": "{client_header:X-Trace-Id}",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	_, ok := headers["x-upstream-trace"]
	require.False(t, ok)
}

func TestProcessHeaderOverride_NonTestKeepsClientHeaderPlaceholder(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Upstream-Trace": "{client_header:X-Trace-Id}",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "trace-123", headers["x-upstream-trace"])
}

func TestProcessHeaderOverride_RuntimeOverrideIsFinalHeaderMap(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		IsChannelTest:             false,
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"x-static":  "runtime-value",
			"x-runtime": "runtime-only",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Static": "legacy-value",
				"X-Legacy": "legacy-only",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "runtime-value", headers["x-static"])
	require.Equal(t, "runtime-only", headers["x-runtime"])
	_, exists := headers["x-legacy"]
	require.False(t, exists)
}

func TestProcessHeaderOverride_PassthroughSkipsAcceptEncoding(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")
	ctx.Request.Header.Set("Accept-Encoding", "gzip")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"*": "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "trace-123", headers["x-trace-id"])

	_, hasAcceptEncoding := headers["accept-encoding"]
	require.False(t, hasAcceptEncoding)
}

func TestProcessHeaderOverride_PassHeadersTemplateSetsRuntimeHeaders(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set("Originator", "Codex CLI")
	ctx.Request.Header.Set("Session_id", "sess-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		RequestHeaders: map[string]string{
			"Originator": "Codex CLI",
			"Session_id": "sess-123",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ParamOverride: map[string]any{
				"operations": []any{
					map[string]any{
						"mode":  "pass_headers",
						"value": []any{"Originator", "Session_id", "X-Codex-Beta-Features"},
					},
				},
			},
			HeadersOverride: map[string]any{
				"X-Static": "legacy-value",
			},
		},
	}

	_, err := relaycommon.ApplyParamOverrideWithRelayInfo([]byte(`{"model":"gpt-4.1"}`), info)
	require.NoError(t, err)
	require.True(t, info.UseRuntimeHeadersOverride)
	require.Equal(t, "Codex CLI", info.RuntimeHeadersOverride["originator"])
	require.Equal(t, "sess-123", info.RuntimeHeadersOverride["session_id"])
	_, exists := info.RuntimeHeadersOverride["x-codex-beta-features"]
	require.False(t, exists)
	require.Equal(t, "legacy-value", info.RuntimeHeadersOverride["x-static"])

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "Codex CLI", headers["originator"])
	require.Equal(t, "sess-123", headers["session_id"])
	_, exists = headers["x-codex-beta-features"]
	require.False(t, exists)

	upstreamReq := httptest.NewRequest(http.MethodPost, "https://example.com/v1/responses", nil)
	applyHeaderOverrideToRequest(upstreamReq, headers)
	require.Equal(t, "Codex CLI", upstreamReq.Header.Get("Originator"))
	require.Equal(t, "sess-123", upstreamReq.Header.Get("Session_id"))
	require.Empty(t, upstreamReq.Header.Get("X-Codex-Beta-Features"))
}
