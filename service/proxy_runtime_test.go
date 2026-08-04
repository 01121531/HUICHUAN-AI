package service

import (
	"context"
	"testing"
	"time"

	"github.com/01121531/HUICHUAN-AI/model"
	"github.com/stretchr/testify/require"
)

func TestSelectChannelProxyUsesStableDatabaseIds(t *testing.T) {
	const channelId = 99101
	group := &model.ProxyGroup{
		Name:                "runtime-selection-test",
		Enabled:             true,
		Status:              model.ProxyGroupStatusAvailable,
		MaxRequests:         1,
		MaxDurationSeconds:  3600,
		BaseCooldownSeconds: 60,
	}
	require.NoError(t, model.DB.Create(group).Error)
	first := &model.Proxy{GroupId: group.Id, Name: "first", Protocol: "http", Host: "127.0.0.1", Port: 18080, Enabled: true, Status: model.ProxyStatusAvailable, Sort: 1}
	second := &model.Proxy{GroupId: group.Id, Name: "second", Protocol: "socks5", Host: "127.0.0.2", Port: 11080, Enabled: true, Status: model.ProxyStatusAvailable, Sort: 2}
	require.NoError(t, model.DB.Create(first).Error)
	require.NoError(t, model.DB.Create(second).Error)
	require.NoError(t, model.DB.Model(group).Update("current_proxy_id", second.Id).Error)
	binding := &model.ChannelProxyBinding{ChannelId: channelId, ProxyGroupId: group.Id, Enabled: true}
	require.NoError(t, model.DB.Create(binding).Error)
	InvalidateChannelProxyConfig(channelId)
	t.Cleanup(func() {
		InvalidateChannelProxyConfig(channelId)
		model.DB.Delete(binding)
		model.DB.Where("group_id = ?", group.Id).Delete(&model.Proxy{})
		model.DB.Delete(group)
	})

	selection, err := SelectChannelProxy(channelId, "http://legacy.invalid:8080")
	require.NoError(t, err)
	require.Equal(t, group.Id, selection.ProxyGroupId)
	require.Equal(t, second.Id, selection.ProxyId)
	require.Equal(t, "socks5://127.0.0.2:11080", selection.URL)

	selection, err = SelectChannelProxy(channelId, "http://legacy.invalid:8080")
	require.NoError(t, err)
	require.Equal(t, first.Id, selection.ProxyId)
	require.Equal(t, "http://127.0.0.1:18080", selection.URL)
}

func TestSelectChannelProxyWaitsForGroupSwitchAndUsesNewProxy(t *testing.T) {
	resetProxyGroupSwitchGatesForTest()
	require.NoError(t, model.DB.AutoMigrate(&model.ProxyGroupWaiter{}))
	const channelId = 99103
	group := &model.ProxyGroup{
		Name: "runtime-switch-wait-test", Enabled: true, Status: model.ProxyGroupStatusAvailable,
		SwitchWaitSeconds: 2, MaxWaitingRequests: 10,
	}
	require.NoError(t, model.DB.Create(group).Error)
	first := &model.Proxy{GroupId: group.Id, Name: "first", Protocol: "http", Host: "127.0.0.1", Port: 18083, Enabled: true, Status: model.ProxyStatusCooling, Sort: 1}
	second := &model.Proxy{GroupId: group.Id, Name: "second", Protocol: "http", Host: "127.0.0.2", Port: 18084, Enabled: true, Status: model.ProxyStatusAvailable, Sort: 2}
	require.NoError(t, model.DB.Create(first).Error)
	require.NoError(t, model.DB.Create(second).Error)
	require.NoError(t, model.DB.Model(group).UpdateColumns(map[string]interface{}{"current_proxy_id": first.Id, "status": model.ProxyGroupStatusAvailable}).Error)
	acquired, _, err := model.TryAcquireProxyGroupSwitchLease(group.Id, first.Id, "remote-instance", 45)
	require.NoError(t, err)
	require.True(t, acquired)
	binding := &model.ChannelProxyBinding{ChannelId: channelId, ProxyGroupId: group.Id, Enabled: true}
	require.NoError(t, model.DB.Create(binding).Error)
	InvalidateChannelProxyConfig(channelId)
	t.Cleanup(func() {
		_ = model.AbortProxyGroupSwitch(group.Id, "remote-instance")
		InvalidateChannelProxyConfig(channelId)
		model.DB.Delete(binding)
		model.DB.Where("group_id = ?", group.Id).Delete(&model.Proxy{})
		model.DB.Delete(group)
		resetProxyGroupSwitchGatesForTest()
	})

	result := make(chan struct {
		selection ChannelProxySelection
		err       error
	}, 1)
	go func() {
		selection, selectErr := SelectChannelProxyWithContext(context.Background(), channelId, "")
		result <- struct {
			selection ChannelProxySelection
			err       error
		}{selection: selection, err: selectErr}
	}()
	select {
	case <-result:
		t.Fatal("selection completed while the group was switching")
	case <-time.After(30 * time.Millisecond):
	}
	require.Eventually(t, func() bool {
		metrics, metricsErr := model.ListProxyGroupWaitMetrics(time.Now().Unix())
		return metricsErr == nil && metrics[group.Id].WaitingRequests == 1
	}, time.Second, 10*time.Millisecond)
	nextProxyId, err := model.CompleteProxyGroupSwitch(group.Id, first.Id, "remote-instance")
	require.NoError(t, err)
	require.Equal(t, second.Id, nextProxyId)

	selected := <-result
	require.NoError(t, selected.err)
	require.Equal(t, second.Id, selected.selection.ProxyId)
	require.Eventually(t, func() bool {
		metrics, metricsErr := model.ListProxyGroupWaitMetrics(time.Now().Unix())
		return metricsErr == nil && metrics[group.Id].WaitingRequests == 0
	}, time.Second, 10*time.Millisecond)
}

func TestSelectChannelProxyEnforcesDatabaseGlobalWaitLimit(t *testing.T) {
	resetProxyGroupSwitchGatesForTest()
	require.NoError(t, model.DB.AutoMigrate(&model.ProxyGroupWaiter{}))
	const channelId = 99108
	group := &model.ProxyGroup{
		Name: "runtime-global-wait-limit-test", Enabled: true, Status: model.ProxyGroupStatusAvailable,
		SwitchWaitSeconds: 2, MaxWaitingRequests: 1,
	}
	require.NoError(t, model.DB.Create(group).Error)
	proxy := &model.Proxy{GroupId: group.Id, Name: "first", Protocol: "http", Host: "127.0.0.1", Port: 18088, Enabled: true, Status: model.ProxyStatusAvailable}
	require.NoError(t, model.DB.Create(proxy).Error)
	require.NoError(t, model.DB.Model(group).UpdateColumn("current_proxy_id", proxy.Id).Error)
	acquired, _, err := model.TryAcquireProxyGroupSwitchLease(group.Id, proxy.Id, "remote-global-limit", 45)
	require.NoError(t, err)
	require.True(t, acquired)
	binding := &model.ChannelProxyBinding{ChannelId: channelId, ProxyGroupId: group.Id, Enabled: true}
	require.NoError(t, model.DB.Create(binding).Error)
	InvalidateChannelProxyConfig(channelId)
	t.Cleanup(func() {
		_ = model.AbortProxyGroupSwitch(group.Id, "remote-global-limit")
		InvalidateChannelProxyConfig(channelId)
		model.DB.Where("group_id = ?", group.Id).Delete(&model.ProxyGroupWaiter{})
		model.DB.Delete(binding)
		model.DB.Delete(proxy)
		model.DB.Delete(group)
		resetProxyGroupSwitchGatesForTest()
	})

	firstDone := make(chan error, 1)
	firstCtx, firstCancel := context.WithTimeout(context.Background(), time.Second)
	defer firstCancel()
	go func() {
		_, selectErr := SelectChannelProxyWithContext(firstCtx, channelId, "")
		firstDone <- selectErr
	}()
	require.Eventually(t, func() bool {
		metrics, metricsErr := model.ListProxyGroupWaitMetrics(time.Now().Unix())
		return metricsErr == nil && metrics[group.Id].WaitingRequests == 1
	}, time.Second, 10*time.Millisecond)

	_, err = SelectChannelProxyWithContext(context.Background(), channelId, "")
	require.ErrorIs(t, err, ErrProxySwitchQueueFull)

	require.NoError(t, model.AbortProxyGroupSwitch(group.Id, "remote-global-limit"))
	require.NoError(t, <-firstDone)
}

func TestSelectChannelProxyWaitsWhenAllProxiesUnavailable(t *testing.T) {
	resetProxyGroupSwitchGatesForTest()
	const channelId = 99104
	group := &model.ProxyGroup{
		Name: "runtime-no-proxy-wait-test", Enabled: true, Status: model.ProxyGroupStatusAvailable,
		SwitchWaitSeconds: 1, MaxWaitingRequests: 10,
	}
	require.NoError(t, model.DB.Create(group).Error)
	proxy := &model.Proxy{GroupId: group.Id, Name: "cooling", Protocol: "http", Host: "127.0.0.1", Port: 18085, Enabled: true, Status: model.ProxyStatusCooling}
	require.NoError(t, model.DB.Create(proxy).Error)
	require.NoError(t, model.DB.Model(group).UpdateColumn("current_proxy_id", proxy.Id).Error)
	binding := &model.ChannelProxyBinding{ChannelId: channelId, ProxyGroupId: group.Id, Enabled: true}
	require.NoError(t, model.DB.Create(binding).Error)
	InvalidateChannelProxyConfig(channelId)
	t.Cleanup(func() {
		InvalidateChannelProxyConfig(channelId)
		model.DB.Delete(binding)
		model.DB.Delete(proxy)
		model.DB.Delete(group)
		resetProxyGroupSwitchGatesForTest()
	})

	result := make(chan struct {
		selection ChannelProxySelection
		err       error
	}, 1)
	go func() {
		selection, selectErr := SelectChannelProxyWithContext(context.Background(), channelId, "")
		result <- struct {
			selection ChannelProxySelection
			err       error
		}{selection, selectErr}
	}()
	select {
	case <-result:
		t.Fatal("selection completed instead of waiting for availability")
	case <-time.After(30 * time.Millisecond):
	}
	require.NoError(t, model.DB.Model(proxy).UpdateColumn("status", model.ProxyStatusAvailable).Error)
	selected := <-result
	require.NoError(t, selected.err)
	require.Equal(t, proxy.Id, selected.selection.ProxyId)
}

func TestSelectChannelProxyLimitsRecoveringProxyToProbeRequests(t *testing.T) {
	resetProxyGroupSwitchGatesForTest()
	const channelId = 99105
	group := &model.ProxyGroup{Name: "runtime-recovery-probe-test", Enabled: true, Status: model.ProxyGroupStatusAvailable}
	require.NoError(t, model.DB.Create(group).Error)
	recovering := &model.Proxy{
		GroupId: group.Id, Name: "recovering", Protocol: "http", Host: "127.0.0.1", Port: 18086,
		Enabled: true, Status: model.ProxyStatusRecovering, RecoveryProbeRemaining: 2, Sort: 1,
	}
	normal := &model.Proxy{GroupId: group.Id, Name: "normal", Protocol: "http", Host: "127.0.0.2", Port: 18087, Enabled: true, Status: model.ProxyStatusAvailable, Sort: 2}
	require.NoError(t, model.DB.Create(recovering).Error)
	require.NoError(t, model.DB.Create(normal).Error)
	require.NoError(t, model.DB.Model(group).UpdateColumn("current_proxy_id", recovering.Id).Error)
	binding := &model.ChannelProxyBinding{ChannelId: channelId, ProxyGroupId: group.Id, Enabled: true}
	require.NoError(t, model.DB.Create(binding).Error)
	InvalidateChannelProxyConfig(channelId)
	t.Cleanup(func() {
		InvalidateChannelProxyConfig(channelId)
		model.DB.Delete(binding)
		model.DB.Where("group_id = ?", group.Id).Delete(&model.Proxy{})
		model.DB.Delete(group)
		resetProxyGroupSwitchGatesForTest()
	})

	first, err := SelectChannelProxy(channelId, "")
	require.NoError(t, err)
	second, err := SelectChannelProxy(channelId, "")
	require.NoError(t, err)
	third, err := SelectChannelProxy(channelId, "")
	require.NoError(t, err)
	require.Equal(t, recovering.Id, first.ProxyId)
	require.Equal(t, recovering.Id, second.ProxyId)
	require.Equal(t, normal.Id, third.ProxyId)
	require.NoError(t, model.DB.First(recovering, recovering.Id).Error)
	require.Zero(t, recovering.RecoveryProbeRemaining)
}

func TestSelectChannelProxyFallsBackToLegacyChannelSetting(t *testing.T) {
	const channelId = 99102
	InvalidateChannelProxyConfig(channelId)
	t.Cleanup(func() { InvalidateChannelProxyConfig(channelId) })

	selection, err := SelectChannelProxy(channelId, "http://127.0.0.1:28080")
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:28080", selection.URL)
	require.Equal(t, 0, selection.ProxyId)
	require.Equal(t, 0, selection.ProxyGroupId)
	require.Equal(t, 1, selection.ProxyIndex)
}

func TestSelectChannelProxyTimesOutWhenGroupHasNoAvailableProxy(t *testing.T) {
	const channelId = 99103
	group := &model.ProxyGroup{Name: "empty-runtime-test", Enabled: true, Status: model.ProxyGroupStatusAvailable, SwitchWaitSeconds: 1}
	require.NoError(t, model.DB.Create(group).Error)
	binding := &model.ChannelProxyBinding{ChannelId: channelId, ProxyGroupId: group.Id, Enabled: true}
	require.NoError(t, model.DB.Create(binding).Error)
	InvalidateChannelProxyConfig(channelId)
	t.Cleanup(func() {
		InvalidateChannelProxyConfig(channelId)
		model.DB.Delete(binding)
		model.DB.Delete(group)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := SelectChannelProxyWithContext(ctx, channelId, "")
	require.ErrorIs(t, err, ErrProxySwitchTimeout)
}
