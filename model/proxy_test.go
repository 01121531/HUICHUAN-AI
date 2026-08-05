package model

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/constant"
	"github.com/01121531/HUICHUAN-AI/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestParseProxyURLList(t *testing.T) {
	proxies, err := ParseProxyURLList("socks5://user:pass@127.0.0.1:1080\nhttp://10.0.0.2:8080,socks5://user:pass@127.0.0.1:1080")
	require.NoError(t, err)
	require.Len(t, proxies, 2)
	require.Equal(t, "socks5", proxies[0].Protocol)
	require.Equal(t, "user", proxies[0].Username)
	require.Equal(t, "pass", proxies[0].Password)
	require.Equal(t, "socks5://user:pass@127.0.0.1:1080", proxies[0].URL())
	require.Equal(t, "http://10.0.0.2:8080", proxies[1].URL())
}

func TestProxyGroupDefaultsMatchDesign(t *testing.T) {
	group := &ProxyGroup{}
	applyProxyGroupDefaults(group)
	require.Equal(t, 3, group.ConsecutiveTimeoutThreshold)
	require.Equal(t, 2, group.HealthFailureThreshold)
	require.Equal(t, 10, group.WindowSize)
	require.InDelta(t, 0.6, group.WindowTimeoutRatio, 0.0001)
	require.Equal(t, 600, group.BaseCooldownSeconds)
	require.Equal(t, 7200, group.MaxCooldownSeconds)
}

func TestUpdateProxyGroupPreservesActiveSwitchLease(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&ProxyGroup{}))
	group := &ProxyGroup{
		Name: "lease-preserve", Enabled: true, Status: ProxyGroupStatusSwitching,
		CurrentProxyId: 123, SwitchLockOwner: "instance-a", SwitchLockUntil: common.GetTimestamp() + 60,
	}
	require.NoError(t, DB.Create(group).Error)
	t.Cleanup(func() { DB.Delete(group) })

	group.Name = "lease-preserve-updated"
	group.MaxRequests = 88
	require.NoError(t, UpdateProxyGroup(group))
	var stored ProxyGroup
	require.NoError(t, DB.First(&stored, group.Id).Error)
	require.Equal(t, "lease-preserve-updated", stored.Name)
	require.Equal(t, 88, stored.MaxRequests)
	require.Equal(t, ProxyGroupStatusSwitching, stored.Status)
	require.Equal(t, 123, stored.CurrentProxyId)
	require.Equal(t, "instance-a", stored.SwitchLockOwner)
	require.Equal(t, group.SwitchLockUntil, stored.SwitchLockUntil)

	stored.Enabled = false
	require.NoError(t, UpdateProxyGroup(&stored))
	require.NoError(t, DB.First(&stored, group.Id).Error)
	require.Equal(t, ProxyGroupStatusDisabled, stored.Status)
	require.Empty(t, stored.SwitchLockOwner)
	require.Zero(t, stored.SwitchLockUntil)
}

func TestParseProxyURLSupportsSOCKS4UserId(t *testing.T) {
	proxy, err := ParseProxyURL("socks4://user@127.0.0.1:1080")
	require.NoError(t, err)
	require.Equal(t, "socks4", proxy.Protocol)
	require.Equal(t, "user", proxy.Username)
	require.Empty(t, proxy.Password)
	require.Equal(t, "socks4://user@127.0.0.1:1080", proxy.URL())
}

func TestParseProxyURLRejectsSOCKS4Password(t *testing.T) {
	_, err := ParseProxyURL("socks4://user:password@127.0.0.1:1080")
	require.ErrorContains(t, err, "not password authentication")
}

func TestParseProxyURLRejectsUnsupportedProtocol(t *testing.T) {
	_, err := ParseProxyURL("socks6://127.0.0.1:1080")
	require.ErrorContains(t, err, "unsupported proxy protocol")
}

func TestProxyJSONNeverContainsPassword(t *testing.T) {
	data, err := json.Marshal(&Proxy{Id: 1, Username: "visible-user", Password: "secret-password"})
	require.NoError(t, err)
	require.Contains(t, string(data), "visible-user")
	require.NotContains(t, string(data), "secret-password")
	require.NotContains(t, string(data), "password")
}

func TestMigrateLegacyChannelProxyIsIdempotent(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&ProxyGroup{}, &Proxy{}, &ChannelProxyBinding{}))

	channel := &Channel{Id: 99001, Name: "proxy-migration-test"}
	channel.SetSetting(dto.ChannelSettings{Proxy: "socks5://u:p@127.0.0.1:1080\nhttp://127.0.0.2:8080"})
	require.NoError(t, DB.Create(channel).Error)
	t.Cleanup(func() {
		var binding ChannelProxyBinding
		if err := DB.Where("channel_id = ?", channel.Id).First(&binding).Error; err == nil {
			DB.Where("group_id = ?", binding.ProxyGroupId).Delete(&Proxy{})
			DB.Delete(&ProxyGroup{}, binding.ProxyGroupId)
			DB.Delete(&binding)
		}
		DB.Delete(&Channel{}, channel.Id)
	})

	require.NoError(t, migrateLegacyChannelProxy(channel))
	require.NoError(t, migrateLegacyChannelProxy(channel))

	config, err := GetChannelProxyRuntimeConfig(channel.Id)
	require.NoError(t, err)
	require.NotNil(t, config)
	require.True(t, config.Binding.Enabled)
	require.True(t, config.Group.Enabled)
	require.Equal(t, DefaultProxyMaxRequests, config.Group.MaxRequests)
	require.Len(t, config.Proxies, 2)
	require.NotZero(t, config.Proxies[0].Id)
	require.Equal(t, config.Proxies[0].Id, config.Group.CurrentProxyId)

	var bindingCount int64
	require.NoError(t, DB.Model(&ChannelProxyBinding{}).Where("channel_id = ?", channel.Id).Count(&bindingCount).Error)
	require.EqualValues(t, 1, bindingCount)
}

func TestSetChannelProxyGroupClearsLegacyProxyAndCanUnbind(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Channel{}, &ProxyGroup{}, &ChannelProxyBinding{}))

	group := &ProxyGroup{Name: "pool-selection-test", Enabled: true, Status: ProxyGroupStatusAvailable}
	require.NoError(t, DB.Create(group).Error)
	channel := &Channel{Name: "pool-selection-channel", Type: 1, Key: "test-key", Models: "gpt-4o", Group: "default"}
	channel.SetSetting(dto.ChannelSettings{Proxy: "http://127.0.0.1:8080"})
	require.NoError(t, DB.Create(channel).Error)
	t.Cleanup(func() {
		DB.Where("channel_id = ?", channel.Id).Delete(&ChannelProxyBinding{})
		DB.Delete(&Channel{}, channel.Id)
		DB.Delete(&ProxyGroup{}, group.Id)
	})

	require.NoError(t, SetChannelProxyGroup(channel.Id, group.Id))
	require.Equal(t, group.Id, mustChannelProxyGroupId(t, channel.Id))

	stored, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	require.Empty(t, stored.GetSetting().Proxy)

	require.NoError(t, SetChannelProxyGroup(channel.Id, 0))
	require.Zero(t, mustChannelProxyGroupId(t, channel.Id))
	stored, err = GetChannelById(channel.Id, true)
	require.NoError(t, err)
	require.Empty(t, stored.GetSetting().Proxy)
}

func TestListProxyPoolOptionsReturnsPoolsAndEnabledProxyCounts(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&ProxyGroup{}, &Proxy{}))

	enabledGroup := &ProxyGroup{Name: "enabled-pool-option", Enabled: true, Status: ProxyGroupStatusAvailable}
	disabledGroup := &ProxyGroup{Name: "disabled-pool-option", Enabled: false, Status: ProxyGroupStatusDisabled}
	require.NoError(t, DB.Create(enabledGroup).Error)
	require.NoError(t, DB.Create(disabledGroup).Error)
	require.NoError(t, DB.Create(&Proxy{GroupId: enabledGroup.Id, Name: "enabled-proxy", Protocol: "http", Host: "127.0.0.1", Port: 8080, Enabled: true}).Error)
	require.NoError(t, DB.Create(&Proxy{GroupId: enabledGroup.Id, Name: "disabled-proxy", Protocol: "http", Host: "127.0.0.2", Port: 8080, Enabled: false}).Error)
	t.Cleanup(func() {
		DB.Where("group_id IN ?", []int{enabledGroup.Id, disabledGroup.Id}).Delete(&Proxy{})
		DB.Delete(&ProxyGroup{}, []int{enabledGroup.Id, disabledGroup.Id})
	})

	options, err := ListProxyPoolOptions()
	require.NoError(t, err)
	var selected *ProxyPoolOption
	var disabled *ProxyPoolOption
	for _, option := range options {
		if option.Id == enabledGroup.Id {
			selected = option
		}
		if option.Id == disabledGroup.Id {
			disabled = option
		}
	}
	require.NotNil(t, selected)
	require.EqualValues(t, 1, selected.ProxyCount)
	require.NotNil(t, disabled)
	require.False(t, disabled.Enabled)
	require.Zero(t, disabled.ProxyCount)
}

func TestBatchInsertAndDeleteChannelsKeepsProxyBindingsConsistent(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Channel{}, &Ability{}, &ProxyGroup{}, &ChannelProxyBinding{}))

	group := &ProxyGroup{Name: "batch-channel-pool", Enabled: true, Status: ProxyGroupStatusAvailable}
	require.NoError(t, DB.Create(group).Error)
	channels := []Channel{
		{Name: "batch-pool-channel-1", Type: 1, Key: "key-1", Models: "gpt-4o", Group: "default"},
		{Name: "batch-pool-channel-2", Type: 1, Key: "key-2", Models: "gpt-4o-mini", Group: "default"},
	}
	t.Cleanup(func() {
		var stored []Channel
		DB.Where("name IN ?", []string{"batch-pool-channel-1", "batch-pool-channel-2"}).Find(&stored)
		ids := make([]int, 0, len(stored))
		for _, channel := range stored {
			ids = append(ids, channel.Id)
		}
		if len(ids) > 0 {
			DB.Where("channel_id IN ?", ids).Delete(&ChannelProxyBinding{})
			DB.Where("channel_id IN ?", ids).Delete(&Ability{})
			DB.Delete(&Channel{}, ids)
		}
		DB.Delete(&ProxyGroup{}, group.Id)
	})

	require.NoError(t, BatchInsertChannelsWithProxyGroup(channels, &group.Id))
	var stored []Channel
	require.NoError(t, DB.Where("name IN ?", []string{"batch-pool-channel-1", "batch-pool-channel-2"}).Order("id asc").Find(&stored).Error)
	require.Len(t, stored, 2)
	ids := make([]int, 0, len(stored))
	for _, channel := range stored {
		require.Positive(t, channel.Id)
		require.Equal(t, group.Id, mustChannelProxyGroupId(t, channel.Id))
		ids = append(ids, channel.Id)
	}

	require.NoError(t, BatchDeleteChannels(ids))
	var bindingCount int64
	require.NoError(t, DB.Model(&ChannelProxyBinding{}).Where("channel_id IN ?", ids).Count(&bindingCount).Error)
	require.Zero(t, bindingCount)
}

func TestDeleteProxyGroupAutomaticallyUnbindsChannels(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Channel{}, &ProxyGroup{}, &Proxy{}, &ChannelProxyBinding{}, &ProxyGroupWaiter{}))

	group := &ProxyGroup{Name: "delete-bound-pool", Enabled: true, Status: ProxyGroupStatusAvailable}
	require.NoError(t, DB.Create(group).Error)
	proxy := &Proxy{GroupId: group.Id, Name: "delete-bound-proxy", Protocol: "http", Host: "127.0.0.1", Port: 8080, Enabled: true}
	require.NoError(t, DB.Create(proxy).Error)
	channel := &Channel{Name: "delete-bound-channel", Type: 1, Key: "test-key", Models: "gpt-4o", Group: "default"}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, SetChannelProxyGroup(channel.Id, group.Id))
	t.Cleanup(func() {
		DB.Where("channel_id = ?", channel.Id).Delete(&ChannelProxyBinding{})
		DB.Where("group_id = ?", group.Id).Delete(&Proxy{})
		DB.Delete(&ProxyGroup{}, group.Id)
		DB.Delete(&Channel{}, channel.Id)
	})

	groups, err := ListProxyGroups()
	require.NoError(t, err)
	var listed *ProxyGroup
	for _, candidate := range groups {
		if candidate.Id == group.Id {
			listed = candidate
			break
		}
	}
	require.NotNil(t, listed)
	require.EqualValues(t, 1, listed.BoundChannelCount)

	unboundChannelCount, err := DeleteProxyGroup(group.Id)
	require.NoError(t, err)
	require.EqualValues(t, 1, unboundChannelCount)
	require.Zero(t, mustChannelProxyGroupId(t, channel.Id))

	var groupCount int64
	require.NoError(t, DB.Model(&ProxyGroup{}).Where("id = ?", group.Id).Count(&groupCount).Error)
	require.Zero(t, groupCount)
	var proxyCount int64
	require.NoError(t, DB.Model(&Proxy{}).Where("group_id = ?", group.Id).Count(&proxyCount).Error)
	require.Zero(t, proxyCount)
	var channelCount int64
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Count(&channelCount).Error)
	require.EqualValues(t, 1, channelCount)
}

func mustChannelProxyGroupId(t *testing.T, channelId int) int {
	t.Helper()
	groupId, err := GetChannelProxyGroupId(channelId)
	require.NoError(t, err)
	return groupId
}

func TestRecordConsumeLogStoresStableProxyIds(t *testing.T) {
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))
	c, _ := gin.CreateTestContext(nil)
	common.SetContextKey(c, constant.ContextKeyProxyId, 321)
	common.SetContextKey(c, constant.ContextKeyProxyGroupId, 654)
	c.Set(common.RequestIdKey, "proxy-log-test-request")
	c.Set("username", "proxy-test")

	RecordConsumeLog(c, 0, RecordConsumeLogParams{
		ChannelId:      77,
		ModelName:      "proxy-test-model",
		UseTimeSeconds: 12,
		Other:          map[string]interface{}{},
	})
	FlushDeferredUsageLogs(c)
	require.True(t, WaitForUsageLogQueue(2*time.Second))
	t.Cleanup(func() {
		LOG_DB.Where("request_id = ?", "proxy-log-test-request").Delete(&Log{})
	})

	var log Log
	require.NoError(t, LOG_DB.Where("request_id = ?", "proxy-log-test-request").First(&log).Error)
	require.Equal(t, 321, log.ProxyId)
	require.Equal(t, 654, log.ProxyGroupId)
}
