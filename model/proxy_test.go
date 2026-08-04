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
	require.Equal(t, 10, group.WindowSize)
	require.InDelta(t, 0.6, group.WindowTimeoutRatio, 0.0001)
	require.Equal(t, 600, group.BaseCooldownSeconds)
	require.Equal(t, 7200, group.MaxCooldownSeconds)
}

func TestParseProxyURLRejectsUnsupportedProtocol(t *testing.T) {
	_, err := ParseProxyURL("socks4://127.0.0.1:1080")
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
