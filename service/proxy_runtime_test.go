package service

import (
	"testing"

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

func TestSelectChannelProxyRejectsUnavailableGroupWithoutDirectFallback(t *testing.T) {
	const channelId = 99103
	group := &model.ProxyGroup{Name: "empty-runtime-test", Enabled: true, Status: model.ProxyGroupStatusAvailable}
	require.NoError(t, model.DB.Create(group).Error)
	binding := &model.ChannelProxyBinding{ChannelId: channelId, ProxyGroupId: group.Id, Enabled: true}
	require.NoError(t, model.DB.Create(binding).Error)
	InvalidateChannelProxyConfig(channelId)
	t.Cleanup(func() {
		InvalidateChannelProxyConfig(channelId)
		model.DB.Delete(binding)
		model.DB.Delete(group)
	})

	_, err := SelectChannelProxy(channelId, "")
	require.ErrorContains(t, err, "no available proxy")
}
