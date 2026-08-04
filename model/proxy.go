package model

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/01121531/HUICHUAN-AI/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ProxyGroupStatusAvailable = "available"
	ProxyGroupStatusSwitching = "switching"
	ProxyGroupStatusDisabled  = "disabled"

	ProxyStatusAvailable   = "available"
	ProxyStatusWatching    = "watching"
	ProxyStatusPaused      = "paused"
	ProxyStatusCooling     = "cooling"
	ProxyStatusRecovering  = "recovering"
	ProxyStatusUnavailable = "unavailable"
	ProxyStatusDisabled    = "disabled"
)

const (
	DefaultProxyMaxRequests            = 500
	DefaultProxyMaxDurationSeconds     = 1800
	DefaultProxySwitchWaitSeconds      = 30
	DefaultProxyMaxWaitingRequests     = 500
	DefaultProxyHealthCheckInterval    = 300
	DefaultProxyHealthFailureThreshold = 2
	DefaultProxyConsecutiveTimeouts    = 3
	DefaultProxyWindowSize             = 10
	DefaultProxyWindowTimeoutRatio     = 0.6
	DefaultProxyBaseCooldownSeconds    = 600
	DefaultProxyMaxCooldownSeconds     = 7200
	DefaultProxyRecoverySuccessCount   = 2
)

// ProxyGroup 保存一组代理的切换策略和当前运行状态。
type ProxyGroup struct {
	Id                          int     `json:"id" gorm:"primaryKey"`
	Name                        string  `json:"name" gorm:"type:varchar(128);not null;index"`
	Enabled                     bool    `json:"enabled" gorm:"index"`
	CurrentProxyId              int     `json:"current_proxy_id" gorm:"default:0;index"`
	Status                      string  `json:"status" gorm:"type:varchar(32);default:'available';index"`
	SwitchLockOwner             string  `json:"-" gorm:"type:varchar(128);default:'';index"`
	SwitchLockUntil             int64   `json:"switch_lock_until" gorm:"bigint;default:0;index"`
	MaxRequests                 int     `json:"max_requests" gorm:"default:500"`
	MaxDurationSeconds          int     `json:"max_duration_seconds" gorm:"default:1800"`
	SwitchWaitSeconds           int     `json:"switch_wait_seconds" gorm:"default:30"`
	MaxWaitingRequests          int     `json:"max_waiting_requests" gorm:"default:500"`
	HealthCheckInterval         int     `json:"health_check_interval" gorm:"default:300"`
	HealthFailureThreshold      int     `json:"health_failure_threshold" gorm:"default:2"`
	ConsecutiveTimeoutThreshold int     `json:"consecutive_timeout_threshold" gorm:"default:3"`
	WindowSize                  int     `json:"window_size" gorm:"default:10"`
	WindowTimeoutRatio          float64 `json:"window_timeout_ratio" gorm:"default:0.6"`
	BaseCooldownSeconds         int     `json:"base_cooldown_seconds" gorm:"default:600"`
	MaxCooldownSeconds          int     `json:"max_cooldown_seconds" gorm:"default:7200"`
	RecoverySuccessCount        int     `json:"recovery_success_count" gorm:"default:2"`
	AllowDirectFallback         bool    `json:"allow_direct_fallback" gorm:"default:false"`
	CreatedAt                   int64   `json:"created_at" gorm:"bigint"`
	UpdatedAt                   int64   `json:"updated_at" gorm:"bigint"`
}

func (ProxyGroup) TableName() string { return "proxy_groups" }

// Proxy 保存稳定代理 ID 和运行指标。认证信息不参与 JSON 序列化。
type Proxy struct {
	Id                     int     `json:"id" gorm:"primaryKey"`
	GroupId                int     `json:"group_id" gorm:"not null;index"`
	Name                   string  `json:"name" gorm:"type:varchar(128);not null"`
	Protocol               string  `json:"protocol" gorm:"type:varchar(16);not null"`
	Host                   string  `json:"host" gorm:"type:varchar(255);not null"`
	Port                   int     `json:"port" gorm:"not null"`
	Username               string  `json:"username" gorm:"type:varchar(255)"`
	Password               string  `json:"-" gorm:"type:varchar(512)"`
	Enabled                bool    `json:"enabled" gorm:"index"`
	Status                 string  `json:"status" gorm:"type:varchar(32);default:'available';index"`
	Sort                   int     `json:"sort" gorm:"default:0;index"`
	LastExitIp             string  `json:"last_exit_ip" gorm:"type:varchar(64);default:''"`
	ExpectedExitIp         string  `json:"expected_exit_ip" gorm:"type:varchar(64);default:''"`
	LastCheckAt            int64   `json:"last_check_at" gorm:"bigint;default:0"`
	LastCheckLatencyMs     int     `json:"last_check_latency_ms" gorm:"default:0"`
	HealthFailures         int     `json:"health_failures" gorm:"default:0"`
	ConsecutiveTimeouts    int     `json:"consecutive_timeouts" gorm:"default:0"`
	RecoveryFailures       int     `json:"recovery_failures" gorm:"default:0"`
	RecoverySuccesses      int     `json:"recovery_successes" gorm:"default:0"`
	RecoveryProbeRemaining int     `json:"recovery_probe_remaining" gorm:"default:0"`
	CooldownUntil          int64   `json:"cooldown_until" gorm:"bigint;default:0;index"`
	LastUsedAt             int64   `json:"last_used_at" gorm:"bigint;default:0"`
	TotalRequests          int64   `json:"total_requests" gorm:"bigint;default:0"`
	TotalTimeouts          int64   `json:"total_timeouts" gorm:"bigint;default:0"`
	WindowSamples          int     `json:"window_samples" gorm:"default:0"`
	WindowTimeouts         int     `json:"window_timeouts" gorm:"default:0"`
	WindowTimeoutRatio     float64 `json:"window_timeout_ratio" gorm:"default:0"`
	LastAnalyzedAt         int64   `json:"last_analyzed_at" gorm:"bigint;default:0"`
	HealthEpochAt          int64   `json:"health_epoch_at" gorm:"bigint;default:0"`
	LastFrtMs              int     `json:"last_frt_ms" gorm:"default:0"`
	LastTps                float64 `json:"last_tps" gorm:"default:0"`
	LastTimeoutReason      string  `json:"last_timeout_reason" gorm:"type:varchar(255);default:''"`
	LastHealthError        string  `json:"last_health_error" gorm:"type:varchar(64);default:''"`
	CreatedAt              int64   `json:"created_at" gorm:"bigint"`
	UpdatedAt              int64   `json:"updated_at" gorm:"bigint"`
}

func (Proxy) TableName() string { return "proxies" }

// ChannelProxyBinding 将渠道唯一绑定到一个代理组。
type ChannelProxyBinding struct {
	Id           int   `json:"id" gorm:"primaryKey"`
	ChannelId    int   `json:"channel_id" gorm:"not null;uniqueIndex"`
	ProxyGroupId int   `json:"proxy_group_id" gorm:"not null;index"`
	Enabled      bool  `json:"enabled" gorm:"index"`
	CreatedAt    int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt    int64 `json:"updated_at" gorm:"bigint"`
}

func (ChannelProxyBinding) TableName() string { return "channel_proxy_bindings" }

type ChannelProxyRuntimeConfig struct {
	Binding *ChannelProxyBinding
	Group   *ProxyGroup
	Proxies []*Proxy
}

type ChannelProxyBindingView struct {
	Id             int    `json:"id"`
	ChannelId      int    `json:"channel_id"`
	ChannelName    string `json:"channel_name"`
	ProxyGroupId   int    `json:"proxy_group_id"`
	ProxyGroupName string `json:"proxy_group_name"`
	Enabled        bool   `json:"enabled"`
}

func (group *ProxyGroup) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if group.CreatedAt == 0 {
		group.CreatedAt = now
	}
	group.UpdatedAt = now
	applyProxyGroupDefaults(group)
	return nil
}

func (group *ProxyGroup) BeforeUpdate(_ *gorm.DB) error {
	group.UpdatedAt = common.GetTimestamp()
	applyProxyGroupDefaults(group)
	return nil
}

func (proxy *Proxy) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if proxy.CreatedAt == 0 {
		proxy.CreatedAt = now
	}
	proxy.UpdatedAt = now
	return validateProxy(proxy)
}

func (proxy *Proxy) BeforeUpdate(_ *gorm.DB) error {
	proxy.UpdatedAt = common.GetTimestamp()
	return validateProxy(proxy)
}

func (binding *ChannelProxyBinding) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if binding.CreatedAt == 0 {
		binding.CreatedAt = now
	}
	binding.UpdatedAt = now
	return nil
}

func (binding *ChannelProxyBinding) BeforeUpdate(_ *gorm.DB) error {
	binding.UpdatedAt = common.GetTimestamp()
	return nil
}

func applyProxyGroupDefaults(group *ProxyGroup) {
	if group.Status == "" {
		group.Status = ProxyGroupStatusAvailable
	}
	if group.MaxRequests <= 0 {
		group.MaxRequests = DefaultProxyMaxRequests
	}
	if group.MaxDurationSeconds <= 0 {
		group.MaxDurationSeconds = DefaultProxyMaxDurationSeconds
	}
	if group.SwitchWaitSeconds <= 0 {
		group.SwitchWaitSeconds = DefaultProxySwitchWaitSeconds
	}
	if group.MaxWaitingRequests <= 0 {
		group.MaxWaitingRequests = DefaultProxyMaxWaitingRequests
	}
	if group.HealthCheckInterval <= 0 {
		group.HealthCheckInterval = DefaultProxyHealthCheckInterval
	}
	if group.HealthFailureThreshold <= 0 {
		group.HealthFailureThreshold = DefaultProxyHealthFailureThreshold
	}
	if group.ConsecutiveTimeoutThreshold <= 0 {
		group.ConsecutiveTimeoutThreshold = DefaultProxyConsecutiveTimeouts
	}
	if group.WindowSize <= 0 {
		group.WindowSize = DefaultProxyWindowSize
	}
	if group.WindowTimeoutRatio <= 0 {
		group.WindowTimeoutRatio = DefaultProxyWindowTimeoutRatio
	}
	if group.BaseCooldownSeconds <= 0 {
		group.BaseCooldownSeconds = DefaultProxyBaseCooldownSeconds
	}
	if group.MaxCooldownSeconds < group.BaseCooldownSeconds {
		group.MaxCooldownSeconds = DefaultProxyMaxCooldownSeconds
		if group.MaxCooldownSeconds < group.BaseCooldownSeconds {
			group.MaxCooldownSeconds = group.BaseCooldownSeconds
		}
	}
	if group.RecoverySuccessCount <= 0 {
		group.RecoverySuccessCount = DefaultProxyRecoverySuccessCount
	}
}

func validateProxy(proxy *Proxy) error {
	proxy.Protocol = strings.ToLower(strings.TrimSpace(proxy.Protocol))
	proxy.Host = strings.TrimSpace(proxy.Host)
	if proxy.Name == "" {
		proxy.Name = proxy.Host
	}
	switch proxy.Protocol {
	case "http", "https", "socks4", "socks5", "socks5h":
	default:
		return fmt.Errorf("unsupported proxy protocol: %s", proxy.Protocol)
	}
	if proxy.Host == "" {
		return errors.New("proxy host is required")
	}
	if proxy.Port < 1 || proxy.Port > 65535 {
		return errors.New("proxy port must be between 1 and 65535")
	}
	if proxy.Protocol == "socks4" && proxy.Password != "" {
		return errors.New("SOCKS4 supports a user ID but not password authentication")
	}
	if proxy.Status == "" {
		proxy.Status = ProxyStatusAvailable
	}
	return nil
}

func (proxy *Proxy) URL() string {
	if proxy == nil {
		return ""
	}
	u := &url.URL{
		Scheme: proxy.Protocol,
		Host:   net.JoinHostPort(proxy.Host, strconv.Itoa(proxy.Port)),
	}
	if proxy.Username != "" {
		if proxy.Password != "" {
			u.User = url.UserPassword(proxy.Username, proxy.Password)
		} else {
			u.User = url.User(proxy.Username)
		}
	}
	return u.String()
}

func ParseProxyURL(raw string) (*Proxy, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		return nil, errors.New("proxy URL must include a valid port")
	}
	proxy := &Proxy{
		Protocol: parsed.Scheme,
		Host:     parsed.Hostname(),
		Port:     port,
		Enabled:  true,
		Status:   ProxyStatusAvailable,
	}
	if parsed.User != nil {
		proxy.Username = parsed.User.Username()
		proxy.Password, _ = parsed.User.Password()
	}
	if err := validateProxy(proxy); err != nil {
		return nil, err
	}
	return proxy, nil
}

func ParseProxyURLList(raw string) ([]*Proxy, error) {
	parts := strings.FieldsFunc(strings.ReplaceAll(raw, "\r\n", "\n"), func(r rune) bool {
		return r == '\n' || r == ','
	})
	seen := make(map[string]struct{}, len(parts))
	proxies := make([]*Proxy, 0, len(parts))
	for index, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, exists := seen[part]; exists {
			continue
		}
		proxy, err := ParseProxyURL(part)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy at position %d: %w", index+1, err)
		}
		seen[part] = struct{}{}
		proxies = append(proxies, proxy)
	}
	return proxies, nil
}

func GetChannelProxyRuntimeConfig(channelId int) (*ChannelProxyRuntimeConfig, error) {
	var binding ChannelProxyBinding
	err := DB.Where("channel_id = ?", channelId).First(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var group ProxyGroup
	if err := DB.First(&group, binding.ProxyGroupId).Error; err != nil {
		return nil, err
	}
	var proxies []*Proxy
	if err := DB.Where("group_id = ?", group.Id).Order("sort asc, id asc").Find(&proxies).Error; err != nil {
		return nil, err
	}
	return &ChannelProxyRuntimeConfig{Binding: &binding, Group: &group, Proxies: proxies}, nil
}

func UpdateProxyGroupCurrentProxy(groupId int, proxyId int) error {
	return DB.Model(&ProxyGroup{}).Where("id = ?", groupId).Updates(map[string]interface{}{
		"current_proxy_id": proxyId,
		"updated_at":       common.GetTimestamp(),
	}).Error
}

func ListProxyGroups() ([]*ProxyGroup, error) {
	var groups []*ProxyGroup
	err := DB.Order("id asc").Find(&groups).Error
	return groups, err
}

func GetProxyGroupById(id int) (*ProxyGroup, error) {
	var group ProxyGroup
	if err := DB.First(&group, id).Error; err != nil {
		return nil, err
	}
	return &group, nil
}

func CreateProxyGroup(group *ProxyGroup) error {
	if group == nil || strings.TrimSpace(group.Name) == "" {
		return errors.New("proxy group name is required")
	}
	group.Name = strings.TrimSpace(group.Name)
	return DB.Create(group).Error
}

func UpdateProxyGroup(group *ProxyGroup) error {
	if group == nil || group.Id <= 0 {
		return errors.New("invalid proxy group")
	}
	if strings.TrimSpace(group.Name) == "" {
		return errors.New("proxy group name is required")
	}
	group.Name = strings.TrimSpace(group.Name)
	applyProxyGroupDefaults(group)
	updates := map[string]interface{}{
		"name":                          group.Name,
		"enabled":                       group.Enabled,
		"max_requests":                  group.MaxRequests,
		"max_duration_seconds":          group.MaxDurationSeconds,
		"switch_wait_seconds":           group.SwitchWaitSeconds,
		"max_waiting_requests":          group.MaxWaitingRequests,
		"health_check_interval":         group.HealthCheckInterval,
		"health_failure_threshold":      group.HealthFailureThreshold,
		"consecutive_timeout_threshold": group.ConsecutiveTimeoutThreshold,
		"window_size":                   group.WindowSize,
		"window_timeout_ratio":          group.WindowTimeoutRatio,
		"base_cooldown_seconds":         group.BaseCooldownSeconds,
		"max_cooldown_seconds":          group.MaxCooldownSeconds,
		"recovery_success_count":        group.RecoverySuccessCount,
		"allow_direct_fallback":         group.AllowDirectFallback,
		"updated_at":                    common.GetTimestamp(),
	}
	if !group.Enabled {
		updates["status"] = ProxyGroupStatusDisabled
		updates["switch_lock_owner"] = ""
		updates["switch_lock_until"] = 0
	} else if group.Status == ProxyGroupStatusDisabled {
		updates["status"] = ProxyGroupStatusAvailable
	}
	return DB.Model(&ProxyGroup{}).Where("id = ?", group.Id).UpdateColumns(updates).Error
}

func DeleteProxyGroup(id int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var bindingCount int64
		if err := tx.Model(&ChannelProxyBinding{}).Where("proxy_group_id = ?", id).Count(&bindingCount).Error; err != nil {
			return err
		}
		if bindingCount > 0 {
			return errors.New("proxy group is still bound to channels")
		}
		if err := tx.Where("group_id = ?", id).Delete(&Proxy{}).Error; err != nil {
			return err
		}
		return tx.Delete(&ProxyGroup{}, id).Error
	})
}

func ListProxiesByGroup(groupId int) ([]*Proxy, error) {
	var proxies []*Proxy
	err := DB.Where("group_id = ?", groupId).Order("sort asc, id asc").Find(&proxies).Error
	return proxies, err
}

func GetProxyById(id int) (*Proxy, error) {
	var proxy Proxy
	if err := DB.First(&proxy, id).Error; err != nil {
		return nil, err
	}
	return &proxy, nil
}

func CreateProxy(proxy *Proxy) error {
	if proxy == nil || proxy.GroupId <= 0 {
		return errors.New("proxy group is required")
	}
	var groupCount int64
	if err := DB.Model(&ProxyGroup{}).Where("id = ?", proxy.GroupId).Count(&groupCount).Error; err != nil {
		return err
	}
	if groupCount == 0 {
		return errors.New("proxy group does not exist")
	}
	return DB.Create(proxy).Error
}

func UpdateProxy(proxy *Proxy) error {
	if proxy == nil || proxy.Id <= 0 {
		return errors.New("invalid proxy")
	}
	var groupCount int64
	if err := DB.Model(&ProxyGroup{}).Where("id = ?", proxy.GroupId).Count(&groupCount).Error; err != nil {
		return err
	}
	if groupCount == 0 {
		return errors.New("proxy group does not exist")
	}
	return DB.Save(proxy).Error
}

func DeleteProxy(id int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var proxy Proxy
		if err := tx.First(&proxy, id).Error; err != nil {
			return err
		}
		if err := tx.Model(&ProxyGroup{}).Where("id = ? AND current_proxy_id = ?", proxy.GroupId, id).
			Updates(map[string]interface{}{"current_proxy_id": 0, "updated_at": common.GetTimestamp()}).Error; err != nil {
			return err
		}
		return tx.Delete(&proxy).Error
	})
}

func ListChannelProxyBindings() ([]*ChannelProxyBindingView, error) {
	var bindings []*ChannelProxyBindingView
	err := DB.Table("channel_proxy_bindings AS b").
		Select("b.id, b.channel_id, channels.name AS channel_name, b.proxy_group_id, proxy_groups.name AS proxy_group_name, b.enabled").
		Joins("LEFT JOIN channels ON channels.id = b.channel_id").
		Joins("LEFT JOIN proxy_groups ON proxy_groups.id = b.proxy_group_id").
		Order("b.channel_id asc").
		Scan(&bindings).Error
	return bindings, err
}

func UpsertChannelProxyBinding(binding *ChannelProxyBinding) error {
	if binding == nil || binding.ChannelId <= 0 || binding.ProxyGroupId <= 0 {
		return errors.New("channel and proxy group are required")
	}
	var channelCount int64
	if err := DB.Model(&Channel{}).Where("id = ?", binding.ChannelId).Count(&channelCount).Error; err != nil {
		return err
	}
	if channelCount == 0 {
		return errors.New("channel does not exist")
	}
	var groupCount int64
	if err := DB.Model(&ProxyGroup{}).Where("id = ?", binding.ProxyGroupId).Count(&groupCount).Error; err != nil {
		return err
	}
	if groupCount == 0 {
		return errors.New("proxy group does not exist")
	}
	now := common.GetTimestamp()
	if binding.CreatedAt == 0 {
		binding.CreatedAt = now
	}
	binding.UpdatedAt = now
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "channel_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"proxy_group_id": binding.ProxyGroupId,
			"enabled":        binding.Enabled,
			"updated_at":     now,
		}),
	}).Create(binding).Error
}

func DeleteChannelProxyBinding(channelId int) error {
	return DB.Where("channel_id = ?", channelId).Delete(&ChannelProxyBinding{}).Error
}

// migrateLegacyChannelProxies 将现有渠道 Proxy 列表幂等迁移为稳定代理实体。
// 原渠道配置会保留，用于回滚和未绑定渠道的兼容路径。
func migrateLegacyChannelProxies() error {
	var channels []*Channel
	if err := DB.Where("setting IS NOT NULL AND setting <> ''").Find(&channels).Error; err != nil {
		return err
	}
	for _, channel := range channels {
		if err := migrateLegacyChannelProxy(channel); err != nil {
			return err
		}
	}
	return nil
}

func migrateLegacyChannelProxy(channel *Channel) error {
	if channel == nil {
		return nil
	}
	raw := channel.GetSetting().Proxy
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var count int64
	if err := DB.Model(&ChannelProxyBinding{}).Where("channel_id = ?", channel.Id).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	proxies, err := ParseProxyURLList(raw)
	if err != nil {
		common.SysLog(fmt.Sprintf("skip legacy proxy migration for channel %d: %v", channel.Id, err))
		return nil
	}
	if len(proxies) == 0 {
		return nil
	}
	if err := DB.Transaction(func(tx *gorm.DB) error {
		group := &ProxyGroup{Name: fmt.Sprintf("渠道 %d 代理组", channel.Id), Enabled: true}
		if err := tx.Create(group).Error; err != nil {
			return err
		}
		for i, proxy := range proxies {
			proxy.GroupId = group.Id
			proxy.Name = fmt.Sprintf("代理 %d", i+1)
			proxy.Sort = i
			if err := tx.Create(proxy).Error; err != nil {
				return err
			}
		}
		group.CurrentProxyId = proxies[0].Id
		if err := tx.Model(group).Update("current_proxy_id", group.CurrentProxyId).Error; err != nil {
			return err
		}
		binding := &ChannelProxyBinding{ChannelId: channel.Id, ProxyGroupId: group.Id, Enabled: true}
		return tx.Create(binding).Error
	}); err != nil {
		return fmt.Errorf("migrate proxies for channel %d: %w", channel.Id, err)
	}
	return nil
}
