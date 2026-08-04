package service

import (
	"context"
	"errors"
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

func TestProxySwitchGateWaitsForActiveSelection(t *testing.T) {
	resetProxyGroupSwitchGatesForTest()
	gate := getProxyGroupSwitchGate(70001)
	release, err := gate.acquireSelection(context.Background(), 10)
	require.NoError(t, err)

	switchStarted := make(chan struct{})
	switchFinished := make(chan error, 1)
	go func() {
		close(switchStarted)
		owner, beginErr := gate.beginSwitch(context.Background())
		if beginErr == nil && owner {
			gate.finishSwitch()
		}
		switchFinished <- beginErr
	}()
	<-switchStarted
	select {
	case <-switchFinished:
		t.Fatal("switch completed before the active selector released")
	case <-time.After(30 * time.Millisecond):
	}
	release()
	require.NoError(t, <-switchFinished)
}

func TestProxyGroupSwitchLeaseHasOneOwnerAndRejectsStaleCompletion(t *testing.T) {
	cleanupProxyAnalyzerTestData(t)
	group, first, second := seedProxyAnalyzerGroup(t, 3, 10, 0.6)

	acquired, state, err := model.TryAcquireProxyGroupSwitchLease(group.Id, first.Id, "instance-a", 45)
	require.NoError(t, err)
	require.True(t, acquired)
	require.Equal(t, "instance-a", state.SwitchLockOwner)

	acquired, state, err = model.TryAcquireProxyGroupSwitchLease(group.Id, first.Id, "instance-b", 45)
	require.NoError(t, err)
	require.False(t, acquired)
	require.Equal(t, "instance-a", state.SwitchLockOwner)

	require.NoError(t, model.DB.Model(group).UpdateColumn("switch_lock_until", common.GetTimestamp()-1).Error)
	acquired, state, err = model.TryAcquireProxyGroupSwitchLease(group.Id, first.Id, "instance-b", 45)
	require.NoError(t, err)
	require.True(t, acquired)
	require.Equal(t, "instance-b", state.SwitchLockOwner)

	_, err = model.CompleteProxyGroupSwitch(group.Id, first.Id, "instance-a")
	require.ErrorIs(t, err, model.ErrProxyGroupSwitchLeaseLost)
	nextProxyId, err := model.CompleteProxyGroupSwitch(group.Id, first.Id, "instance-b")
	require.NoError(t, err)
	require.Equal(t, second.Id, nextProxyId)
	require.NoError(t, model.DB.First(group, group.Id).Error)
	require.Equal(t, second.Id, group.CurrentProxyId)
	require.Equal(t, model.ProxyGroupStatusAvailable, group.Status)
	require.Empty(t, group.SwitchLockOwner)
	require.Zero(t, group.SwitchLockUntil)
}

func TestExpiredProxyGroupSwitchLeaseIsRecovered(t *testing.T) {
	cleanupProxyAnalyzerTestData(t)
	group, first, _ := seedProxyAnalyzerGroup(t, 3, 10, 0.6)
	acquired, _, err := model.TryAcquireProxyGroupSwitchLease(group.Id, first.Id, "stopped-instance", 45)
	require.NoError(t, err)
	require.True(t, acquired)
	require.NoError(t, model.DB.Model(group).UpdateColumn("switch_lock_until", common.GetTimestamp()-1).Error)

	recovered, err := model.RecoverExpiredProxyGroupSwitches(common.GetTimestamp())
	require.NoError(t, err)
	require.EqualValues(t, 1, recovered)
	require.NoError(t, model.DB.First(group, group.Id).Error)
	require.Equal(t, model.ProxyGroupStatusAvailable, group.Status)
	require.Empty(t, group.SwitchLockOwner)
	require.Zero(t, group.SwitchLockUntil)
}

func TestManualPauseResumeAndSwitch(t *testing.T) {
	cleanupProxyAnalyzerTestData(t)
	resetProxyGroupSwitchGatesForTest()
	group, first, second := seedProxyAnalyzerGroup(t, 3, 10, 0.6)
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ip":"203.0.113.31"}`))
	}))
	defer proxyServer.Close()
	parsed, err := url.Parse(proxyServer.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)
	t.Setenv("PROXY_HEALTH_CHECK_URL", "http://health-check.invalid/ip")
	t.Setenv("PROXY_HEALTH_CHECK_TIMEOUT_SECONDS", "2")
	require.NoError(t, model.DB.Model(first).UpdateColumns(map[string]interface{}{
		"host": parsed.Hostname(), "port": port, "expected_exit_ip": "203.0.113.31",
	}).Error)

	require.NoError(t, SetManagedProxyPaused(context.Background(), first.Id, true))
	require.NoError(t, model.DB.First(first, first.Id).Error)
	require.Equal(t, model.ProxyStatusPaused, first.Status)
	require.NoError(t, model.DB.First(group, group.Id).Error)
	require.Equal(t, second.Id, group.CurrentProxyId)

	require.NoError(t, SetManagedProxyPaused(context.Background(), first.Id, false))
	require.NoError(t, model.DB.First(first, first.Id).Error)
	require.Equal(t, model.ProxyStatusRecovering, first.Status)
	require.Equal(t, model.DefaultProxyRecoverySuccessCount, first.RecoveryProbeRemaining)
	require.NoError(t, model.DB.First(group, group.Id).Error)
	require.Equal(t, first.Id, group.CurrentProxyId)

	nextProxyId, err := SwitchManagedProxyGroupNow(context.Background(), group.Id)
	require.NoError(t, err)
	require.Equal(t, second.Id, nextProxyId)
}

func TestProxySwitchGateEnforcesQueueLimitAndTimeout(t *testing.T) {
	resetProxyGroupSwitchGatesForTest()
	gate := getProxyGroupSwitchGate(70002)
	owner, err := gate.beginSwitch(context.Background())
	require.NoError(t, err)
	require.True(t, owner)
	defer gate.finishSwitch()

	firstResult := make(chan error, 1)
	firstCtx, firstCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer firstCancel()
	go func() {
		_, acquireErr := gate.acquireSelection(firstCtx, 1)
		firstResult <- acquireErr
	}()
	require.Eventually(t, func() bool {
		gate.mu.Lock()
		defer gate.mu.Unlock()
		return gate.waitingRequests == 1
	}, time.Second, time.Millisecond)

	_, err = gate.acquireSelection(context.Background(), 1)
	require.ErrorIs(t, err, ErrProxySwitchQueueFull)
	require.ErrorIs(t, <-firstResult, ErrProxySwitchTimeout)
}

func TestProxySwitchGateRespectsClientCancellation(t *testing.T) {
	resetProxyGroupSwitchGatesForTest()
	gate := getProxyGroupSwitchGate(70003)
	owner, err := gate.beginSwitch(context.Background())
	require.NoError(t, err)
	require.True(t, owner)
	defer gate.finishSwitch()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = gate.acquireSelection(ctx, 10)
	require.True(t, errors.Is(err, context.Canceled))
}
