package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/model"
	"github.com/stretchr/testify/require"
)

func TestEvaluateProxyLogTimingMatchesGenericLogRedRules(t *testing.T) {
	tests := []struct {
		name    string
		log     *model.Log
		timeout bool
		counted bool
		reason  string
	}{
		{name: "stream first response below boundary", log: &model.Log{Type: model.LogTypeConsume, IsStream: true, Other: `{"frt":9999}`}, counted: true},
		{name: "stream first response boundary", log: &model.Log{Type: model.LogTypeConsume, IsStream: true, Other: `{"frt":10000}`}, timeout: true, counted: true, reason: "first_response"},
		{name: "non stream ignores frt", log: &model.Log{Type: model.LogTypeConsume, Other: `{"frt":50000}`}, counted: true},
		{name: "small output below duration boundary", log: &model.Log{Type: model.LogTypeConsume, CompletionTokens: 99, UseTime: 29}, counted: true},
		{name: "small output duration boundary", log: &model.Log{Type: model.LogTypeConsume, CompletionTokens: 99, UseTime: 30}, timeout: true, counted: true, reason: "duration"},
		{name: "large output tps below boundary", log: &model.Log{Type: model.LogTypeConsume, CompletionTokens: 100, UseTime: 7}, timeout: true, counted: true, reason: "throughput"},
		{name: "large output tps boundary", log: &model.Log{Type: model.LogTypeConsume, CompletionTokens: 105, UseTime: 7}, counted: true},
		{name: "zero duration has no throughput judgment", log: &model.Log{Type: model.LogTypeConsume, CompletionTokens: 100, UseTime: 0}, counted: true},
		{name: "fast business error does not reset streak", log: &model.Log{Type: model.LogTypeError, UseTime: 1}, counted: false},
		{name: "slow error is health evidence", log: &model.Log{Type: model.LogTypeError, UseTime: 30}, timeout: true, counted: true, reason: "duration"},
		{name: "string frt is accepted", log: &model.Log{Type: model.LogTypeConsume, IsStream: true, Other: `{"frt":"10000"}`}, timeout: true, counted: true, reason: "first_response"},
		{name: "malformed other is safe", log: &model.Log{Type: model.LogTypeConsume, IsStream: true, Other: `{bad`}, counted: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := EvaluateProxyLogTiming(test.log)
			require.Equal(t, test.timeout, result.Timeout)
			require.Equal(t, test.counted, result.Counted)
			require.Equal(t, test.reason, result.Reason)
		})
	}
}

func TestCalculateProxyRequestHealthScore(t *testing.T) {
	tests := []struct {
		name     string
		analysis *model.ProxyLogAnalysis
		expected int
	}{
		{name: "first response boundary", analysis: &model.ProxyLogAnalysis{IsStream: true, FirstResponseTimeMs: 10_000, CompletionTokens: 100, TokensPerSecond: 30}, expected: 60},
		{name: "small output duration boundary", analysis: &model.ProxyLogAnalysis{UseTimeSeconds: 30, CompletionTokens: 99}, expected: 60},
		{name: "throughput boundary", analysis: &model.ProxyLogAnalysis{CompletionTokens: 100, TokensPerSecond: 15}, expected: 60},
		{name: "excellent request caps at one hundred", analysis: &model.ProxyLogAnalysis{IsStream: true, FirstResponseTimeMs: 1, CompletionTokens: 100, TokensPerSecond: 60}, expected: 100},
		{name: "slow throughput falls below threshold", analysis: &model.ProxyLogAnalysis{CompletionTokens: 100, TokensPerSecond: 10}, expected: 40},
		{name: "composite request uses lowest score", analysis: &model.ProxyLogAnalysis{IsStream: true, FirstResponseTimeMs: 5_000, CompletionTokens: 20, UseTimeSeconds: 25}, expected: 67},
		{name: "timeout score is capped", analysis: &model.ProxyLogAnalysis{CompletionTokens: 100, TokensPerSecond: 30, IsTimeout: true}, expected: 59},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.expected, CalculateProxyRequestHealthScore(test.analysis))
		})
	}
}

func TestGetProxyTrendFiltersAndReturnsLatestChronologically(t *testing.T) {
	cleanupProxyAnalyzerTestData(t)
	group, first, second := seedProxyAnalyzerGroup(t, 3, 10, 0.6)
	otherGroup := &model.ProxyGroup{Name: "other-trend-group", Enabled: true, Status: model.ProxyGroupStatusAvailable}
	require.NoError(t, model.DB.Create(otherGroup).Error)
	otherProxy := &model.Proxy{GroupId: otherGroup.Id, Name: "other", Protocol: "http", Host: "127.0.0.3", Port: 18083, Enabled: true, Status: model.ProxyStatusAvailable}
	require.NoError(t, model.DB.Create(otherProxy).Error)

	for index := 0; index < 105; index++ {
		proxyId := first.Id
		if index%2 == 1 {
			proxyId = second.Id
		}
		require.NoError(t, model.DB.Create(&model.ProxyLogAnalysis{
			AnalysisKey: fmt.Sprintf("trend-%03d", index), LogCreatedAt: int64(1_000 + index),
			ProxyId: proxyId, ProxyGroupId: group.Id, Counted: true,
			CompletionTokens: 20, UseTimeSeconds: 1,
		}).Error)
	}
	require.NoError(t, model.DB.Create(&model.ProxyLogAnalysis{
		AnalysisKey: "trend-not-counted", LogCreatedAt: 9_999, ProxyId: first.Id,
		ProxyGroupId: group.Id, Counted: false, CompletionTokens: 20, UseTimeSeconds: 1,
	}).Error)
	require.NoError(t, model.DB.Create(&model.ProxyLogAnalysis{
		AnalysisKey: "trend-other-group", LogCreatedAt: 10_000, ProxyId: otherProxy.Id,
		ProxyGroupId: otherGroup.Id, Counted: true, CompletionTokens: 20, UseTimeSeconds: 1,
	}).Error)

	groupTrend, err := GetProxyTrend(group.Id, 0, 100)
	require.NoError(t, err)
	require.Equal(t, 100, groupTrend.SampleCount)
	require.EqualValues(t, 1_005, groupTrend.Points[0].Timestamp)
	require.EqualValues(t, 1_104, groupTrend.Points[99].Timestamp)
	for index := 1; index < len(groupTrend.Points); index++ {
		require.Less(t, groupTrend.Points[index-1].Timestamp, groupTrend.Points[index].Timestamp)
	}

	proxyTrend, err := GetProxyTrend(group.Id, first.Id, 100)
	require.NoError(t, err)
	require.Equal(t, 53, proxyTrend.SampleCount)
	for _, point := range proxyTrend.Points {
		require.Equal(t, first.Id, point.ProxyId)
	}
}

func TestProxyLogAnalyzerPausesAndSwitchesAfterThreeConsecutiveRedLogs(t *testing.T) {
	cleanupProxyAnalyzerTestData(t)
	group, first, second := seedProxyAnalyzerGroup(t, 3, 10, 0.6)
	createdAt := common.GetTimestamp() - 30
	for index := 0; index < 3; index++ {
		require.NoError(t, model.LOG_DB.Create(&model.Log{
			CreatedAt: createdAt + int64(index), Type: model.LogTypeConsume,
			RequestId: fmt.Sprintf("proxy-analyzer-consecutive-%d", index),
			ProxyId:   first.Id, ProxyGroupId: group.Id, CompletionTokens: 20, UseTime: 30,
		}).Error)
	}

	summary, err := RunProxyLogAnalysisTask(context.Background())
	require.NoError(t, err)
	require.Equal(t, 3, summary.Inserted)
	require.Equal(t, 3, summary.Timeouts)
	require.Equal(t, 1, summary.Paused)
	require.Equal(t, 1, summary.Switched)

	require.NoError(t, model.DB.First(first, first.Id).Error)
	require.Equal(t, model.ProxyStatusCooling, first.Status)
	require.Equal(t, 3, first.ConsecutiveTimeouts)
	require.EqualValues(t, 3, first.TotalRequests)
	require.EqualValues(t, 3, first.TotalTimeouts)
	require.Greater(t, first.CooldownUntil, common.GetTimestamp())
	require.NoError(t, model.DB.First(group, group.Id).Error)
	require.Equal(t, second.Id, group.CurrentProxyId)

	var eventCount int64
	require.NoError(t, model.DB.Model(&model.ProxyStateEvent{}).
		Where("proxy_id = ? AND event_type = ?", first.Id, "auto_paused").Count(&eventCount).Error)
	require.EqualValues(t, 1, eventCount)
}

func TestProxyLogAnalyzerWindowTriggersAtSixOfTen(t *testing.T) {
	cleanupProxyAnalyzerTestData(t)
	group, first, second := seedProxyAnalyzerGroup(t, 99, 10, 0.6)
	createdAt := common.GetTimestamp() - 60
	red := []bool{true, false, true, false, true, false, true, false, true, true}
	for index, isRed := range red {
		useTime := 1
		if isRed {
			useTime = 30
		}
		require.NoError(t, model.LOG_DB.Create(&model.Log{
			CreatedAt: createdAt + int64(index), Type: model.LogTypeConsume,
			RequestId: fmt.Sprintf("proxy-analyzer-window-%02d", index),
			ProxyId:   first.Id, ProxyGroupId: group.Id, CompletionTokens: 20, UseTime: useTime,
		}).Error)
	}

	summary, err := RunProxyLogAnalysisTask(context.Background())
	require.NoError(t, err)
	require.Equal(t, 10, summary.Inserted)
	require.Equal(t, 6, summary.Timeouts)
	require.Equal(t, 1, summary.Paused)
	require.NoError(t, model.DB.First(first, first.Id).Error)
	require.Equal(t, 10, first.WindowSamples)
	require.Equal(t, 6, first.WindowTimeouts)
	require.InDelta(t, 0.6, first.WindowTimeoutRatio, 0.0001)
	require.Equal(t, model.ProxyStatusCooling, first.Status)
	require.NoError(t, model.DB.First(group, group.Id).Error)
	require.Equal(t, second.Id, group.CurrentProxyId)
}

func seedProxyAnalyzerGroup(t *testing.T, consecutiveThreshold int, windowSize int, ratio float64) (*model.ProxyGroup, *model.Proxy, *model.Proxy) {
	t.Helper()
	group := &model.ProxyGroup{
		Name: "proxy-analyzer-test", Enabled: true, Status: model.ProxyGroupStatusAvailable,
		ConsecutiveTimeoutThreshold: consecutiveThreshold, WindowSize: windowSize,
		WindowTimeoutRatio: ratio, BaseCooldownSeconds: 600, MaxCooldownSeconds: 7200,
	}
	require.NoError(t, model.DB.Create(group).Error)
	first := &model.Proxy{GroupId: group.Id, Name: "first", Protocol: "http", Host: "127.0.0.1", Port: 18081, Enabled: true, Status: model.ProxyStatusAvailable, Sort: 1}
	second := &model.Proxy{GroupId: group.Id, Name: "second", Protocol: "http", Host: "127.0.0.2", Port: 18082, Enabled: true, Status: model.ProxyStatusAvailable, Sort: 2}
	require.NoError(t, model.DB.Create(first).Error)
	require.NoError(t, model.DB.Create(second).Error)
	group.CurrentProxyId = first.Id
	require.NoError(t, model.DB.Model(group).Update("current_proxy_id", first.Id).Error)
	return group, first, second
}

func cleanupProxyAnalyzerTestData(t *testing.T) {
	t.Helper()
	require.NoError(t, model.DB.Exec("DELETE FROM proxy_state_events").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM proxy_log_analyses").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM proxy_log_analysis_cursors").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM logs WHERE proxy_id > 0").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM channel_proxy_bindings").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM proxies").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM proxy_groups").Error)
	InvalidateChannelProxyConfig(0)
	t.Cleanup(func() {
		_ = model.DB.Exec("DELETE FROM proxy_state_events").Error
		_ = model.DB.Exec("DELETE FROM proxy_log_analyses").Error
		_ = model.DB.Exec("DELETE FROM proxy_log_analysis_cursors").Error
		_ = model.DB.Exec("DELETE FROM logs WHERE proxy_id > 0").Error
		_ = model.DB.Exec("DELETE FROM proxies").Error
		_ = model.DB.Exec("DELETE FROM proxy_groups").Error
		InvalidateChannelProxyConfig(0)
	})
}

func TestProxyLogAnalysisInterval(t *testing.T) {
	require.Equal(t, 15*time.Second, ProxyLogAnalysisInterval())
}
