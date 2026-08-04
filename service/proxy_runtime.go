package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/01121531/HUICHUAN-AI/model"
)

const channelProxyConfigCacheTTL = 30 * time.Second

type ChannelProxySelection struct {
	URL             string
	ProxyId         int
	ProxyGroupId    int
	ProxyIndex      int
	stateKey        int
	cooldownSeconds int
}

type channelProxyConfigCacheEntry struct {
	config    *model.ChannelProxyRuntimeConfig
	expiresAt time.Time
}

var channelProxyConfigCache = struct {
	sync.RWMutex
	entries map[int]channelProxyConfigCacheEntry
}{entries: make(map[int]channelProxyConfigCacheEntry)}

func InvalidateChannelProxyConfig(channelId int) {
	channelProxyConfigCache.Lock()
	defer channelProxyConfigCache.Unlock()
	if channelId > 0 {
		delete(channelProxyConfigCache.entries, channelId)
		return
	}
	channelProxyConfigCache.entries = make(map[int]channelProxyConfigCacheEntry)
}

func loadChannelProxyConfig(channelId int) (*model.ChannelProxyRuntimeConfig, error) {
	now := time.Now()
	channelProxyConfigCache.RLock()
	entry, ok := channelProxyConfigCache.entries[channelId]
	channelProxyConfigCache.RUnlock()
	if ok && now.Before(entry.expiresAt) {
		return entry.config, nil
	}

	config, err := model.GetChannelProxyRuntimeConfig(channelId)
	if err != nil {
		return nil, err
	}
	channelProxyConfigCache.Lock()
	channelProxyConfigCache.entries[channelId] = channelProxyConfigCacheEntry{
		config:    config,
		expiresAt: now.Add(channelProxyConfigCacheTTL),
	}
	channelProxyConfigCache.Unlock()
	return config, nil
}

// SelectChannelProxy 优先使用稳定代理实体；没有渠道绑定时兼容原渠道 Proxy 字段。
func SelectChannelProxy(channelId int, legacyRaw string) (ChannelProxySelection, error) {
	return SelectChannelProxyWithContext(context.Background(), channelId, legacyRaw)
}

func SelectChannelProxyWithContext(ctx context.Context, channelId int, legacyRaw string) (ChannelProxySelection, error) {
	config, err := loadChannelProxyConfig(channelId)
	if err != nil {
		return ChannelProxySelection{}, err
	}
	if config == nil {
		proxyURL, index, err := ChannelProxyRotator.SelectProxy(channelId, legacyRaw)
		return ChannelProxySelection{
			URL:             proxyURL,
			ProxyIndex:      index,
			stateKey:        channelId,
			cooldownSeconds: ChannelProxyFailCooldownSeconds,
		}, err
	}

	if config.Binding == nil || !config.Binding.Enabled {
		return ChannelProxySelection{}, nil
	}
	if config.Group == nil {
		return ChannelProxySelection{}, errors.New("channel proxy group does not exist")
	}
	config, releaseSelection, err := acquireManagedProxySelection(ctx, channelId, config)
	if err != nil {
		return ChannelProxySelection{}, err
	}
	defer releaseSelection()
	if !config.Group.Enabled || config.Group.Status == model.ProxyGroupStatusDisabled {
		if config.Group.AllowDirectFallback {
			return ChannelProxySelection{ProxyGroupId: config.Group.Id}, nil
		}
		return ChannelProxySelection{}, errors.New("channel proxy group is disabled")
	}

	proxies := availableRuntimeProxies(config)
	if len(proxies) == 0 {
		if config.Group.AllowDirectFallback {
			return ChannelProxySelection{ProxyGroupId: config.Group.Id}, nil
		}
		releaseSelection()
		waitCtx, cancel := proxySwitchWaitContext(ctx, config.Group.SwitchWaitSeconds)
		defer cancel()
		gate := getProxyGroupSwitchGate(config.Group.Id)
		err := gate.waitForAvailability(waitCtx, config.Group.MaxWaitingRequests, func() (bool, error) {
			current, loadErr := model.GetChannelProxyRuntimeConfig(channelId)
			if loadErr != nil {
				return false, loadErr
			}
			if current == nil || current.Group == nil || current.Binding == nil || !current.Binding.Enabled {
				return true, nil
			}
			return current.Group.AllowDirectFallback || hasPotentialRuntimeProxy(current), nil
		})
		if err != nil {
			return ChannelProxySelection{}, err
		}
		InvalidateChannelProxyConfig(channelId)
		return SelectChannelProxyWithContext(ctx, channelId, legacyRaw)
	}

	proxyURLs := make([]string, 0, len(proxies))
	for _, proxy := range proxies {
		proxyURLs = append(proxyURLs, proxy.URL())
	}
	stateKey := -config.Group.Id
	proxyURL, index, err := ChannelProxyRotator.SelectProxyWithPolicyFromIndex(
		stateKey,
		strings.Join(proxyURLs, "\n"),
		config.Group.MaxRequests,
		config.Group.MaxDurationSeconds,
		currentProxyIndex(proxies, config.Group.CurrentProxyId),
	)
	if err != nil {
		return ChannelProxySelection{}, err
	}
	if index < 1 || index > len(proxies) {
		return ChannelProxySelection{}, errors.New("invalid channel proxy selection index")
	}
	selected := proxies[index-1]
	if selected.Id != config.Group.CurrentProxyId {
		if err := model.UpdateProxyGroupCurrentProxy(config.Group.Id, selected.Id); err != nil {
			return ChannelProxySelection{}, err
		}
		config.Group.CurrentProxyId = selected.Id
	}
	return ChannelProxySelection{
		URL:             proxyURL,
		ProxyId:         selected.Id,
		ProxyGroupId:    config.Group.Id,
		ProxyIndex:      index,
		stateKey:        stateKey,
		cooldownSeconds: config.Group.BaseCooldownSeconds,
	}, nil
}

func hasPotentialRuntimeProxy(config *model.ChannelProxyRuntimeConfig) bool {
	for _, proxy := range config.Proxies {
		if proxy == nil || !proxy.Enabled {
			continue
		}
		if proxy.Status == "" || proxy.Status == model.ProxyStatusAvailable || proxy.Status == model.ProxyStatusWatching {
			return true
		}
		if proxy.Id == config.Group.CurrentProxyId && proxy.Status == model.ProxyStatusRecovering && proxy.RecoveryProbeRemaining > 0 {
			return true
		}
	}
	return false
}

func acquireManagedProxySelection(ctx context.Context, channelId int, config *model.ChannelProxyRuntimeConfig) (*model.ChannelProxyRuntimeConfig, func(), error) {
	group := config.Group
	waitCtx, cancel := proxySwitchWaitContext(ctx, group.SwitchWaitSeconds)
	gate := getProxyGroupSwitchGate(group.Id)
	for {
		if group.Status == model.ProxyGroupStatusSwitching {
			err := gate.waitForAvailability(waitCtx, group.MaxWaitingRequests, func() (bool, error) {
				current, loadErr := model.GetChannelProxyRuntimeConfig(channelId)
				if loadErr != nil {
					return false, loadErr
				}
				if current == nil || current.Group == nil {
					return true, nil
				}
				if current.Group.Status == model.ProxyGroupStatusSwitching && current.Group.SwitchLockUntil > 0 && current.Group.SwitchLockUntil <= time.Now().Unix() {
					recovered, recoverErr := model.RecoverExpiredProxyGroupSwitch(current.Group.Id, time.Now().Unix())
					if recoverErr != nil {
						return false, recoverErr
					}
					if recovered {
						InvalidateChannelProxyConfig(channelId)
						return true, nil
					}
				}
				return current.Group.Status != model.ProxyGroupStatusSwitching, nil
			})
			if err != nil {
				cancel()
				return nil, nil, err
			}
			current, err := model.GetChannelProxyRuntimeConfig(channelId)
			if err != nil {
				cancel()
				return nil, nil, err
			}
			if current == nil || current.Group == nil {
				cancel()
				return nil, nil, errors.New("channel proxy group does not exist")
			}
			config, group = current, current.Group
			continue
		}

		release, err := gate.acquireSelection(waitCtx, group.MaxWaitingRequests)
		if err != nil {
			cancel()
			return nil, nil, err
		}
		current, err := model.GetChannelProxyRuntimeConfig(channelId)
		if err != nil {
			release()
			cancel()
			return nil, nil, err
		}
		if current == nil || current.Group == nil {
			release()
			cancel()
			return nil, nil, errors.New("channel proxy group does not exist")
		}
		if current.Group.Status == model.ProxyGroupStatusSwitching {
			release()
			config, group = current, current.Group
			continue
		}
		return current, func() {
			release()
			cancel()
		}, nil
	}
}

func availableRuntimeProxies(config *model.ChannelProxyRuntimeConfig) []*model.Proxy {
	for _, proxy := range config.Proxies {
		if proxy == nil || proxy.Id != config.Group.CurrentProxyId || !proxy.Enabled || proxy.Status != model.ProxyStatusRecovering {
			continue
		}
		claimed, err := model.ClaimRecoveringProxyProbe(proxy.Id)
		if err == nil && claimed {
			return []*model.Proxy{proxy}
		}
		break
	}
	available := make([]*model.Proxy, 0, len(config.Proxies))
	for _, proxy := range config.Proxies {
		if proxy == nil || !proxy.Enabled {
			continue
		}
		if proxy.Status != "" && proxy.Status != model.ProxyStatusAvailable && proxy.Status != model.ProxyStatusWatching {
			continue
		}
		available = append(available, proxy)
	}
	return available
}

func currentProxyIndex(proxies []*model.Proxy, currentProxyId int) int {
	for i, proxy := range proxies {
		if proxy.Id == currentProxyId {
			return i
		}
	}
	return 0
}

func MarkChannelProxyFailed(selection ChannelProxySelection) {
	if selection.ProxyIndex <= 0 {
		return
	}
	ChannelProxyRotator.MarkProxyFailedWithCooldown(
		selection.stateKey,
		selection.ProxyIndex,
		selection.cooldownSeconds,
	)
}
