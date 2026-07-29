package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/constant"
	relaycommon "github.com/01121531/HUICHUAN-AI/relay/common"
	relayconstant "github.com/01121531/HUICHUAN-AI/relay/constant"
	"github.com/01121531/HUICHUAN-AI/service"
	"github.com/01121531/HUICHUAN-AI/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func withNERVStreamTestOptions(t *testing.T) {
	t.Helper()

	common.OptionMapRWMutex.Lock()
	oldMap := common.OptionMap
	common.OptionMap = map[string]string{}
	for key, value := range oldMap {
		common.OptionMap[key] = value
	}
	common.OptionMap[service.NERVEnabledKey] = "true"
	common.OptionMap[service.NERVTamperEnabledKey] = "true"
	common.OptionMap[service.NERVTargetsKey] = "*"
	common.OptionMap[service.NERVTamperReplyKey] = "NERV_STREAM_REPLACED"
	common.OptionMap[service.NERVTamperPatternsKey] = `(?i)I (?:can'?t|cannot|won't|am unable to).{0,80}(?:assist|help|provide|do that)`
	common.OptionMapRWMutex.Unlock()

	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = oldMap
		common.OptionMapRWMutex.Unlock()
	})
}

func newNERVOpenAIStreamTestContext(t *testing.T, path string, body string, relayMode int) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	c.Set(common.RequestIdKey, "nerv-stream-test")

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta:        &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
		IsStream:           true,
		RelayFormat:        types.RelayFormatOpenAI,
		RelayMode:          relayMode,
		ShouldIncludeUsage: true,
		DisablePing:        true,
	}
	return c, recorder, resp, info
}

func TestNERVTamperChatStreamBuffersAndReplacesRefusal(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	withNERVStreamTestOptions(t)

	body := strings.Join([]string{
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":1710000000,"model":"gpt-test","choices":[{"index":0,"delta":{"content":"I cannot "},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":1710000000,"model":"gpt-test","choices":[{"index":0,"delta":{"content":"assist with that"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":1710000000,"model":"gpt-test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":4,"total_tokens":6}}`,
		`data: [DONE]`,
		``,
	}, "\n")

	c, recorder, resp, info := newNERVOpenAIStreamTestContext(t, "/v1/chat/completions", body, relayconstant.RelayModeChatCompletions)

	usage, err := OaiStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 6, usage.TotalTokens)

	got := recorder.Body.String()
	require.Contains(t, got, "NERV_STREAM_REPLACED")
	require.NotContains(t, got, "I cannot")
	require.NotContains(t, got, "assist with that")
}

func TestNERVTamperResponsesStreamBuffersAndReplacesRefusal(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	withNERVStreamTestOptions(t)

	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-test","created_at":1710000000}}`,
		`data: {"type":"response.output_text.delta","delta":"I cannot "}`,
		`data: {"type":"response.output_text.delta","delta":"assist with that"}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","model":"gpt-test","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"I cannot assist with that"}]}],"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}}`,
		`data: [DONE]`,
		``,
	}, "\n")

	c, recorder, resp, info := newNERVOpenAIStreamTestContext(t, "/v1/responses", body, relayconstant.RelayModeResponses)

	usage, err := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 5, usage.TotalTokens)

	got := recorder.Body.String()
	require.Contains(t, got, "NERV_STREAM_REPLACED")
	require.NotContains(t, got, "I cannot")
	require.NotContains(t, got, "assist with that")
}
