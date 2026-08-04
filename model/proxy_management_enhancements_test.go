package model

import (
	"testing"
	"time"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/stretchr/testify/require"
)

func TestResetProxyObservationCountersStartsFreshEpoch(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&ProxyGroup{}, &Proxy{}, &ProxyLogAnalysis{}, &ProxyStateEvent{}))
	group := &ProxyGroup{Name: "observation-reset-test", Enabled: true}
	require.NoError(t, DB.Create(group).Error)
	proxy := &Proxy{
		GroupId: group.Id, Name: "reset-target", Protocol: "http", Host: "127.0.0.1", Port: 18901,
		Enabled: true, Status: ProxyStatusWatching, ConsecutiveTimeouts: 2,
		WindowSamples: 7, WindowTimeouts: 4, WindowTimeoutRatio: 4.0 / 7.0,
		TotalRequests: 20, TotalTimeouts: 6, LastAnalyzedAt: common.GetTimestamp() - 1,
		LastFrtMs: 11000, LastTps: 8.5, LastTimeoutReason: "first_response",
	}
	require.NoError(t, DB.Create(proxy).Error)
	t.Cleanup(func() {
		DB.Where("proxy_id = ?", proxy.Id).Delete(&ProxyStateEvent{})
		DB.Where("proxy_id = ?", proxy.Id).Delete(&ProxyLogAnalysis{})
		DB.Delete(proxy)
		DB.Delete(group)
	})

	reset, err := ResetProxyObservationCounters(proxy.Id)
	require.NoError(t, err)
	require.Equal(t, ProxyStatusAvailable, reset.Status)
	require.Zero(t, reset.ConsecutiveTimeouts)
	require.Zero(t, reset.WindowSamples)
	require.Zero(t, reset.WindowTimeouts)
	require.Zero(t, reset.WindowTimeoutRatio)
	require.Zero(t, reset.LastAnalyzedAt)
	require.Zero(t, reset.LastFrtMs)
	require.Zero(t, reset.LastTps)
	require.Empty(t, reset.LastTimeoutReason)
	require.Positive(t, reset.HealthEpochAt)
	require.EqualValues(t, 20, reset.TotalRequests)
	require.EqualValues(t, 6, reset.TotalTimeouts)

	stale := &ProxyLogAnalysis{
		AnalysisKey: "observation-reset-stale-" + common.GetUUID(), RequestId: common.GetUUID(),
		LogType: LogTypeConsume, LogCreatedAt: reset.HealthEpochAt - 1,
		ProxyId: proxy.Id, ProxyGroupId: group.Id, Counted: true, IsTimeout: true,
		FirstResponseTimeMs: 15000, TokensPerSecond: 5, Reason: "first_response",
	}
	_, err = ApplyProxyLogAnalysis(stale)
	require.NoError(t, err)
	require.NoError(t, DB.First(reset, proxy.Id).Error)
	require.Zero(t, reset.ConsecutiveTimeouts)
	require.Zero(t, reset.WindowSamples)
	require.Zero(t, reset.LastFrtMs)
	require.Empty(t, reset.LastTimeoutReason)
	require.EqualValues(t, 21, reset.TotalRequests)
	require.EqualValues(t, 7, reset.TotalTimeouts)

	var event ProxyStateEvent
	require.NoError(t, DB.Where("proxy_id = ? AND event_type = ?", proxy.Id, "manual_observation_reset").First(&event).Error)
	require.Equal(t, ProxyStatusWatching, event.FromStatus)
	require.Equal(t, ProxyStatusAvailable, event.ToStatus)
}

func TestGenericLogsCanBeFilteredByStableProxyId(t *testing.T) {
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))
	now := time.Now().Unix()
	requestA := "proxy-filter-a-" + common.GetUUID()
	requestB := "proxy-filter-b-" + common.GetUUID()
	const userId = 98123
	const proxyA = 930001
	const proxyB = 930002
	logs := []*Log{
		{UserId: userId, CreatedAt: now, Type: LogTypeConsume, RequestId: requestA, ProxyId: proxyA, Quota: 120, PromptTokens: 10, CompletionTokens: 20},
		{UserId: userId, CreatedAt: now, Type: LogTypeConsume, RequestId: requestB, ProxyId: proxyB, Quota: 240, PromptTokens: 30, CompletionTokens: 40},
	}
	require.NoError(t, LOG_DB.Create(logs).Error)
	t.Cleanup(func() {
		LOG_DB.Where("request_id IN ?", []string{requestA, requestB}).Delete(&Log{})
	})

	adminLogs, total, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 20, 0, "", "", "", proxyA)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, adminLogs, 1)
	require.Equal(t, requestA, adminLogs[0].RequestId)

	userLogs, total, err := GetUserLogs(userId, LogTypeUnknown, 0, 0, "", "", 0, 20, "", "", "", proxyB)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, userLogs, 1)
	require.Equal(t, requestB, userLogs[0].RequestId)

	stat, err := SumUsedQuota(LogTypeConsume, now-1, now+1, "", "", "", 0, "", proxyA)
	require.NoError(t, err)
	require.Equal(t, 120, stat.Quota)
}
