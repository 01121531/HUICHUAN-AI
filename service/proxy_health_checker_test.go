package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/model"
	"github.com/stretchr/testify/require"
)

func TestCheckManagedProxyHealthReadsAndValidatesExitIP(t *testing.T) {
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ip":"203.0.113.9"}`))
	}))
	defer proxyServer.Close()
	parsed, err := url.Parse(proxyServer.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)
	t.Setenv("PROXY_HEALTH_CHECK_URL", "http://health-check.invalid/ip")
	t.Setenv("PROXY_HEALTH_CHECK_TIMEOUT_SECONDS", "2")

	proxy := &model.Proxy{
		Protocol: "http", Host: parsed.Hostname(), Port: port,
		ExpectedExitIp: "203.0.113.0/24,198.51.100.1",
	}
	outcome := checkManagedProxyHealth(context.Background(), proxy)
	require.True(t, outcome.Success)
	require.Equal(t, "203.0.113.9", outcome.ExitIp)
	require.Empty(t, outcome.FailureReason)

	proxy.ExpectedExitIp = "198.51.100.1"
	outcome = checkManagedProxyHealth(context.Background(), proxy)
	require.False(t, outcome.Success)
	require.Equal(t, "exit_ip_mismatch", outcome.FailureReason)
}

func TestProxyHealthFailuresMarkUnavailableAndSwitch(t *testing.T) {
	cleanupProxyAnalyzerTestData(t)
	resetProxyGroupSwitchGatesForTest()
	group, first, second := seedProxyAnalyzerGroup(t, 3, 10, 0.6)
	group.HealthFailureThreshold = 2
	require.NoError(t, model.DB.Model(group).UpdateColumn("health_failure_threshold", 2).Error)

	transition, err := model.ApplyProxyHealthCheckResult(first.Id, false, 120, "", "request_failed")
	require.NoError(t, err)
	require.Equal(t, model.ProxyStatusAvailable, transition.ToStatus)
	require.False(t, transition.SwitchRequired)
	transition, err = model.ApplyProxyHealthCheckResult(first.Id, false, 130, "", "request_failed")
	require.NoError(t, err)
	require.Equal(t, model.ProxyStatusUnavailable, transition.ToStatus)
	require.True(t, transition.SwitchRequired)
	nextProxyId, err := switchManagedProxyGroup(context.Background(), group.Id, first.Id, transition.SwitchWaitSeconds)
	require.NoError(t, err)
	require.Equal(t, second.Id, nextProxyId)

	require.NoError(t, model.DB.First(first, first.Id).Error)
	require.Equal(t, model.ProxyStatusUnavailable, first.Status)
	require.Equal(t, 2, first.HealthFailures)
	require.Equal(t, "request_failed", first.LastHealthError)
	require.NoError(t, model.DB.First(group, group.Id).Error)
	require.Equal(t, second.Id, group.CurrentProxyId)
}

func TestFullProxyHealthFailureImmediatelyMarksUnavailable(t *testing.T) {
	cleanupProxyAnalyzerTestData(t)
	group, first, _ := seedProxyAnalyzerGroup(t, 3, 10, 0.6)
	require.NoError(t, model.DB.Model(group).UpdateColumn("health_failure_threshold", 9).Error)

	transition, err := model.ApplyProxyFullHealthCheckResult(first.Id, false, 120, "", "request_failed")
	require.NoError(t, err)
	require.Equal(t, model.ProxyStatusUnavailable, transition.ToStatus)
	require.True(t, transition.SwitchRequired)
	require.NoError(t, model.DB.First(first, first.Id).Error)
	require.Equal(t, model.ProxyStatusUnavailable, first.Status)
	require.Equal(t, 1, first.HealthFailures)
}

func TestFullProxyHealthSuccessImmediatelyRestoresAutoDisabledProxy(t *testing.T) {
	cleanupProxyAnalyzerTestData(t)
	_, first, _ := seedProxyAnalyzerGroup(t, 3, 10, 0.6)
	require.NoError(t, model.DB.Model(first).UpdateColumns(map[string]interface{}{
		"status":                   model.ProxyStatusUnavailable,
		"health_failures":          2,
		"last_health_error":        "request_failed",
		"recovery_failures":        1,
		"recovery_probe_remaining": 2,
	}).Error)

	transition, err := model.ApplyProxyFullHealthCheckResult(first.Id, true, 85, "203.0.113.9", "")
	require.NoError(t, err)
	require.Equal(t, model.ProxyStatusAvailable, transition.ToStatus)
	require.False(t, transition.ProbeRequired)
	require.NoError(t, model.DB.First(first, first.Id).Error)
	require.Equal(t, model.ProxyStatusAvailable, first.Status)
	require.Zero(t, first.HealthFailures)
	require.Zero(t, first.RecoveryFailures)
	require.Empty(t, first.LastHealthError)
}

func TestListAllEnabledProxyHealthChecksIncludesAutoDisabledOnly(t *testing.T) {
	cleanupProxyAnalyzerTestData(t)
	group, first, second := seedProxyAnalyzerGroup(t, 3, 10, 0.6)
	require.NoError(t, model.DB.Model(first).UpdateColumn("status", model.ProxyStatusUnavailable).Error)
	require.NoError(t, model.DB.Model(second).UpdateColumn("status", model.ProxyStatusPaused).Error)

	targets, err := model.ListAllEnabledProxyHealthChecks(group.Id)
	require.NoError(t, err)
	require.Len(t, targets, 1)
	require.Equal(t, first.Id, targets[0].Proxy.Id)
}

func TestProxyDailyHealthCheckUsesConfiguredTime(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	mapWasNil := common.OptionMap == nil
	if mapWasNil {
		common.OptionMap = make(map[string]string)
	}
	previousEnabled, hadEnabled := common.OptionMap[model.ProxyDailyHealthCheckEnabledOption]
	previousTime, hadTime := common.OptionMap[model.ProxyDailyHealthCheckTimeOption]
	common.OptionMap[model.ProxyDailyHealthCheckEnabledOption] = "true"
	common.OptionMap[model.ProxyDailyHealthCheckTimeOption] = "13:30"
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if mapWasNil {
			common.OptionMap = nil
			return
		}
		if hadEnabled {
			common.OptionMap[model.ProxyDailyHealthCheckEnabledOption] = previousEnabled
		} else {
			delete(common.OptionMap, model.ProxyDailyHealthCheckEnabledOption)
		}
		if hadTime {
			common.OptionMap[model.ProxyDailyHealthCheckTimeOption] = previousTime
		} else {
			delete(common.OptionMap, model.ProxyDailyHealthCheckTimeOption)
		}
	})

	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	before := time.Date(2026, 8, 6, 13, 29, 0, 0, location)
	after := time.Date(2026, 8, 6, 13, 31, 0, 0, location)
	require.False(t, model.IsProxyDailyHealthCheckDue(before, 0))
	require.True(t, model.IsProxyDailyHealthCheckDue(after, 0))
	require.False(t, model.IsProxyDailyHealthCheckDue(after, time.Date(2026, 8, 6, 13, 30, 0, 0, location).Unix()))
	require.True(t, model.IsProxyDailyHealthCheckDue(after, time.Date(2026, 8, 5, 13, 30, 0, 0, location).Unix()))
}

func TestRunProxyHealthCheckTaskProcessesDueProxy(t *testing.T) {
	cleanupProxyAnalyzerTestData(t)
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ip":"203.0.113.10"}`))
	}))
	defer proxyServer.Close()
	parsed, err := url.Parse(proxyServer.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)
	t.Setenv("PROXY_HEALTH_CHECK_URL", "http://health-check.invalid/ip")
	t.Setenv("PROXY_HEALTH_CHECK_TIMEOUT_SECONDS", "2")

	group := &model.ProxyGroup{Name: "health-task", Enabled: true, HealthCheckInterval: 300}
	require.NoError(t, model.DB.Create(group).Error)
	proxy := &model.Proxy{
		GroupId: group.Id, Name: "health-task-proxy", Protocol: "http",
		Host: parsed.Hostname(), Port: port, Enabled: true, Status: model.ProxyStatusAvailable,
		ExpectedExitIp: "203.0.113.10",
	}
	require.NoError(t, model.DB.Create(proxy).Error)
	summary, err := RunProxyHealthCheckTask(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, summary.Checked)
	require.Equal(t, 1, summary.Healthy)
	require.NoError(t, model.DB.First(proxy, proxy.Id).Error)
	require.Equal(t, "203.0.113.10", proxy.LastExitIp)
	require.Greater(t, proxy.LastCheckAt, int64(0))
}

func TestRunProxyFullHealthCheckTaskReportsProgress(t *testing.T) {
	cleanupProxyAnalyzerTestData(t)
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ip":"203.0.113.11"}`))
	}))
	defer proxyServer.Close()
	parsed, err := url.Parse(proxyServer.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)
	t.Setenv("PROXY_HEALTH_CHECK_URL", "http://health-check.invalid/ip")
	t.Setenv("PROXY_HEALTH_CHECK_TIMEOUT_SECONDS", "2")

	group := &model.ProxyGroup{Name: "health-progress", Enabled: true}
	require.NoError(t, model.DB.Create(group).Error)
	require.NoError(t, model.DB.Create(&model.Proxy{
		GroupId: group.Id, Name: "health-progress-proxy", Protocol: "http",
		Host: parsed.Hostname(), Port: port, Enabled: true, Status: model.ProxyStatusAvailable,
	}).Error)

	progressUpdates := make([][2]int, 0, 2)
	summary, err := RunProxyFullHealthCheckTaskWithProgress(
		context.Background(),
		group.Id,
		func(processed, total int) {
			progressUpdates = append(progressUpdates, [2]int{processed, total})
		},
	)
	require.NoError(t, err)
	require.Equal(t, 1, summary.Checked)
	require.Equal(t, [][2]int{{0, 1}, {1, 1}}, progressUpdates)
}

func TestCoolingProxyUsesRealProbeLogsBeforeRecovery(t *testing.T) {
	cleanupProxyAnalyzerTestData(t)
	resetProxyGroupSwitchGatesForTest()
	group, first, _ := seedProxyAnalyzerGroup(t, 3, 10, 0.6)
	require.NoError(t, model.DB.Model(first).UpdateColumns(map[string]interface{}{
		"status": model.ProxyStatusCooling, "cooldown_until": common.GetTimestamp() - 1,
	}).Error)

	transition, err := model.ApplyProxyHealthCheckResult(first.Id, true, 80, "203.0.113.8", "")
	require.NoError(t, err)
	require.Equal(t, model.ProxyStatusRecovering, transition.ToStatus)
	require.True(t, transition.ProbeRequired)
	require.NoError(t, switchManagedProxyGroupTo(context.Background(), group.Id, first.Id, transition.SwitchWaitSeconds))

	now := common.GetTimestamp()
	for index := 0; index < 2; index++ {
		analysis := proxyLogAnalysisFromLog(&model.Log{
			Type: model.LogTypeConsume, CreatedAt: now + int64(index),
			RequestId: fmt.Sprintf("proxy-recovery-success-%d", index),
			ProxyId:   first.Id, ProxyGroupId: group.Id, CompletionTokens: 20, UseTime: 1,
		})
		applyResult, applyErr := model.ApplyProxyLogAnalysis(analysis)
		require.NoError(t, applyErr)
		require.True(t, applyResult.Inserted)
	}
	require.NoError(t, model.DB.First(first, first.Id).Error)
	require.Equal(t, model.ProxyStatusAvailable, first.Status)
	require.Zero(t, first.RecoveryFailures)
	require.Zero(t, first.RecoverySuccesses)
	require.Zero(t, first.RecoveryProbeRemaining)
}

func TestProxyGroupStartsOnlyOneRecoveryProbeAtATime(t *testing.T) {
	cleanupProxyAnalyzerTestData(t)
	group, first, second := seedProxyAnalyzerGroup(t, 3, 10, 0.6)
	now := common.GetTimestamp()
	require.NoError(t, model.DB.Model(&model.Proxy{}).Where("id IN ?", []int{first.Id, second.Id}).UpdateColumns(map[string]interface{}{
		"status": model.ProxyStatusCooling, "cooldown_until": now - 1,
	}).Error)

	firstTransition, err := model.ApplyProxyHealthCheckResult(first.Id, true, 80, "203.0.113.41", "")
	require.NoError(t, err)
	require.True(t, firstTransition.ProbeRequired)
	require.Equal(t, model.ProxyStatusRecovering, firstTransition.ToStatus)

	secondTransition, err := model.ApplyProxyHealthCheckResult(second.Id, true, 90, "203.0.113.42", "")
	require.NoError(t, err)
	require.False(t, secondTransition.ProbeRequired)
	require.Equal(t, model.ProxyStatusCooling, secondTransition.ToStatus)
	require.NoError(t, model.DB.First(second, second.Id).Error)
	require.Equal(t, model.ProxyStatusCooling, second.Status)
	require.Greater(t, second.CooldownUntil, now)
	require.NoError(t, model.DB.First(group, group.Id).Error)
}

func TestRecoveryRedLogReturnsToExponentialCooldown(t *testing.T) {
	cleanupProxyAnalyzerTestData(t)
	group, first, _ := seedProxyAnalyzerGroup(t, 3, 10, 0.6)
	require.NoError(t, model.DB.Model(first).UpdateColumns(map[string]interface{}{
		"status": model.ProxyStatusCooling, "cooldown_until": common.GetTimestamp() - 1,
	}).Error)
	transition, err := model.ApplyProxyHealthCheckResult(first.Id, true, 80, "203.0.113.8", "")
	require.NoError(t, err)
	require.Equal(t, model.ProxyStatusRecovering, transition.ToStatus)

	now := common.GetTimestamp()
	analysis := proxyLogAnalysisFromLog(&model.Log{
		Type: model.LogTypeConsume, CreatedAt: now, RequestId: "proxy-recovery-red",
		ProxyId: first.Id, ProxyGroupId: group.Id, CompletionTokens: 20, UseTime: 30,
	})
	applyResult, err := model.ApplyProxyLogAnalysis(analysis)
	require.NoError(t, err)
	require.True(t, applyResult.Paused)
	require.NoError(t, model.DB.First(first, first.Id).Error)
	require.Equal(t, model.ProxyStatusCooling, first.Status)
	require.Equal(t, 1, first.RecoveryFailures)
	require.GreaterOrEqual(t, first.CooldownUntil-now, int64(1200))
}

func TestParseProxyHealthExitIP(t *testing.T) {
	require.Equal(t, "2001:db8::1", parseProxyHealthExitIP([]byte(`{"ip":"2001:db8::1"}`)))
	require.Equal(t, "198.51.100.7", parseProxyHealthExitIP([]byte("198.51.100.7")))
	require.Empty(t, parseProxyHealthExitIP([]byte("not-an-ip")))
}
