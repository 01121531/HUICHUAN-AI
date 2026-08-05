package model

import (
	"errors"
	"math"

	"github.com/01121531/HUICHUAN-AI/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ProxyHealthCheckTarget struct {
	Proxy *Proxy
	Group *ProxyGroup
}

type ProxyHealthTransitionResult struct {
	ProxyId           int    `json:"proxy_id"`
	ProxyGroupId      int    `json:"proxy_group_id"`
	FromStatus        string `json:"from_status"`
	ToStatus          string `json:"to_status"`
	SwitchRequired    bool   `json:"switch_required"`
	ProbeRequired     bool   `json:"probe_required"`
	SwitchWaitSeconds int    `json:"switch_wait_seconds"`
}

func ListDueProxyHealthChecks(now int64, limit int) ([]*ProxyHealthCheckTarget, error) {
	if limit <= 0 {
		limit = 100
	}
	var groups []*ProxyGroup
	if err := DB.Where("enabled = ? AND status <> ?", true, ProxyGroupStatusDisabled).Order("id asc").Find(&groups).Error; err != nil {
		return nil, err
	}
	targets := make([]*ProxyHealthCheckTarget, 0, min(limit, len(groups)))
	for _, group := range groups {
		interval := group.HealthCheckInterval
		if interval <= 0 {
			interval = DefaultProxyHealthCheckInterval
		}
		var proxies []*Proxy
		if err := DB.Where("group_id = ? AND enabled = ? AND status NOT IN ?", group.Id, true, []string{ProxyStatusDisabled, ProxyStatusPaused}).
			Where("(status = ? AND cooldown_until > 0 AND cooldown_until <= ?) OR (status <> ? AND (last_check_at = 0 OR last_check_at + ? <= ?))", ProxyStatusCooling, now, ProxyStatusCooling, interval, now).
			Order("last_check_at asc, id asc").
			Limit(limit - len(targets)).
			Find(&proxies).Error; err != nil {
			return nil, err
		}
		for _, proxy := range proxies {
			targets = append(targets, &ProxyHealthCheckTarget{Proxy: proxy, Group: group})
			if len(targets) >= limit {
				return targets, nil
			}
		}
	}
	return targets, nil
}

func HasDueProxyHealthChecks(now int64) bool {
	targets, err := ListDueProxyHealthChecks(now, 1)
	return err == nil && len(targets) > 0
}

// ListAllEnabledProxyHealthChecks returns every proxy that is eligible for an
// automatic connection test. Manually paused/disabled proxies are excluded,
// while automatically unavailable proxies remain eligible for recovery tests.
func ListAllEnabledProxyHealthChecks(groupId int) ([]*ProxyHealthCheckTarget, error) {
	var groups []*ProxyGroup
	query := DB.Where("enabled = ? AND status <> ?", true, ProxyGroupStatusDisabled)
	if groupId > 0 {
		query = query.Where("id = ?", groupId)
	}
	if err := query.Order("id asc").Find(&groups).Error; err != nil {
		return nil, err
	}
	targets := make([]*ProxyHealthCheckTarget, 0)
	for _, group := range groups {
		var proxies []*Proxy
		if err := DB.Where("group_id = ? AND enabled = ? AND status NOT IN ?", group.Id, true, []string{ProxyStatusDisabled, ProxyStatusPaused}).
			Order("sort asc, id asc").Find(&proxies).Error; err != nil {
			return nil, err
		}
		for _, proxy := range proxies {
			targets = append(targets, &ProxyHealthCheckTarget{Proxy: proxy, Group: group})
		}
	}
	return targets, nil
}

func ApplyProxyHealthCheckResult(proxyId int, success bool, latencyMs int, exitIp string, failureReason string) (ProxyHealthTransitionResult, error) {
	return applyProxyHealthCheckResult(proxyId, success, latencyMs, exitIp, failureReason, false)
}

// ApplyProxyFullHealthCheckResult immediately marks a failed proxy unavailable.
// Full scheduled checks and administrator-triggered checks use this stricter
// behavior so an unreachable address is removed from request selection at once.
func ApplyProxyFullHealthCheckResult(proxyId int, success bool, latencyMs int, exitIp string, failureReason string) (ProxyHealthTransitionResult, error) {
	return applyProxyHealthCheckResult(proxyId, success, latencyMs, exitIp, failureReason, true)
}

func applyProxyHealthCheckResult(proxyId int, success bool, latencyMs int, exitIp string, failureReason string, disableImmediately bool) (ProxyHealthTransitionResult, error) {
	result := ProxyHealthTransitionResult{ProxyId: proxyId}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var proxy Proxy
		if err := tx.First(&proxy, proxyId).Error; err != nil {
			return err
		}
		var group ProxyGroup
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&group, proxy.GroupId).Error; err != nil {
			return err
		}
		now := common.GetTimestamp()
		result.ProxyGroupId = group.Id
		result.FromStatus = proxy.Status
		result.ToStatus = proxy.Status
		result.SwitchWaitSeconds = group.SwitchWaitSeconds
		updates := map[string]interface{}{
			"last_check_at":         now,
			"last_check_latency_ms": latencyMs,
			"last_exit_ip":          exitIp,
			"updated_at":            now,
		}
		if success {
			updates["health_failures"] = 0
			updates["last_health_error"] = ""
			if proxy.Enabled && (proxy.Status == ProxyStatusUnavailable || (proxy.Status == ProxyStatusCooling && proxy.CooldownUntil <= now)) {
				var recoveringCount int64
				if err := tx.Model(&Proxy{}).
					Where("group_id = ? AND id <> ? AND enabled = ? AND status = ?", group.Id, proxy.Id, true, ProxyStatusRecovering).
					Count(&recoveringCount).Error; err != nil {
					return err
				}
				if recoveringCount > 0 {
					if proxy.Status == ProxyStatusCooling {
						interval := group.HealthCheckInterval
						if interval <= 0 {
							interval = DefaultProxyHealthCheckInterval
						}
						updates["cooldown_until"] = now + int64(interval)
					}
				} else {
					probeCount := group.RecoverySuccessCount
					if probeCount <= 0 {
						probeCount = DefaultProxyRecoverySuccessCount
					}
					result.ToStatus = ProxyStatusRecovering
					result.ProbeRequired = true
					updates["status"] = result.ToStatus
					updates["recovery_successes"] = 0
					updates["recovery_probe_remaining"] = probeCount
					updates["cooldown_until"] = 0
					updates["health_epoch_at"] = now
					updates["consecutive_timeouts"] = 0
					updates["window_samples"] = 0
					updates["window_timeouts"] = 0
					updates["window_timeout_ratio"] = 0
				}
			}
		} else {
			failures := proxy.HealthFailures + 1
			updates["health_failures"] = failures
			updates["last_health_error"] = failureReason
			if disableImmediately && proxy.Enabled && proxy.Status != ProxyStatusDisabled && proxy.Status != ProxyStatusPaused {
				result.ToStatus = ProxyStatusUnavailable
				updates["status"] = result.ToStatus
				updates["cooldown_until"] = 0
				updates["recovery_successes"] = 0
				updates["recovery_probe_remaining"] = 0
			} else if proxy.Status == ProxyStatusCooling || proxy.Status == ProxyStatusRecovering {
				recoveryFailures := proxy.RecoveryFailures + 1
				result.ToStatus = ProxyStatusCooling
				updates["status"] = result.ToStatus
				updates["recovery_failures"] = recoveryFailures
				updates["recovery_successes"] = 0
				updates["recovery_probe_remaining"] = 0
				updates["cooldown_until"] = now + int64(proxyCooldownSeconds(&group, recoveryFailures))
			} else {
				threshold := group.HealthFailureThreshold
				if threshold <= 0 {
					threshold = DefaultProxyHealthFailureThreshold
				}
				if failures >= threshold && proxy.Status != ProxyStatusDisabled {
					result.ToStatus = ProxyStatusUnavailable
					updates["status"] = result.ToStatus
				}
			}
		}
		if result.ToStatus != result.FromStatus && group.CurrentProxyId == proxy.Id &&
			(result.ToStatus == ProxyStatusCooling || result.ToStatus == ProxyStatusUnavailable) {
			result.SwitchRequired = true
		}
		if err := tx.Model(&Proxy{}).Where("id = ?", proxy.Id).UpdateColumns(updates).Error; err != nil {
			return err
		}
		if result.ToStatus != result.FromStatus {
			eventType := "health_status_changed"
			if result.ToStatus == ProxyStatusRecovering {
				eventType = "recovery_started"
			} else if result.ToStatus == ProxyStatusCooling {
				eventType = "recovery_failed"
			} else if result.ToStatus == ProxyStatusUnavailable {
				eventType = "health_unavailable"
			}
			return tx.Create(&ProxyStateEvent{
				ProxyId: proxy.Id, ProxyGroupId: group.Id, EventType: eventType,
				FromStatus: result.FromStatus, ToStatus: result.ToStatus, Reason: failureReason,
			}).Error
		}
		return nil
	})
	return result, err
}

// AutoDisableProxyAfterRequestFailure immediately removes a managed proxy from
// request selection after a real upstream network failure. Manual disablement
// remains separate so the health task can retest this proxy after cooldown.
func AutoDisableProxyAfterRequestFailure(proxyId int) (bool, error) {
	disabled := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var proxy Proxy
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&proxy, proxyId).Error; err != nil {
			return err
		}
		if !proxy.Enabled || proxy.Status == ProxyStatusDisabled || proxy.Status == ProxyStatusPaused ||
			proxy.Status == ProxyStatusCooling || proxy.Status == ProxyStatusUnavailable {
			return nil
		}
		var group ProxyGroup
		if err := tx.First(&group, proxy.GroupId).Error; err != nil {
			return err
		}
		now := common.GetTimestamp()
		recoveryFailures := proxy.RecoveryFailures
		if proxy.Status == ProxyStatusRecovering {
			recoveryFailures++
		}
		if err := tx.Model(&Proxy{}).Where("id = ?", proxy.Id).UpdateColumns(map[string]interface{}{
			"status":                   ProxyStatusCooling,
			"cooldown_until":           now + int64(proxyCooldownSeconds(&group, recoveryFailures)),
			"recovery_failures":        recoveryFailures,
			"recovery_successes":       0,
			"recovery_probe_remaining": 0,
			"last_health_error":        "request_failed",
			"updated_at":               now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Create(&ProxyStateEvent{
			ProxyId: proxy.Id, ProxyGroupId: group.Id, EventType: "auto_paused",
			FromStatus: proxy.Status, ToStatus: ProxyStatusCooling, Reason: "request_failed",
		}).Error; err != nil {
			return err
		}
		disabled = true
		return nil
	})
	return disabled, err
}

func proxyCooldownSeconds(group *ProxyGroup, recoveryFailures int) int {
	base := group.BaseCooldownSeconds
	if base <= 0 {
		base = DefaultProxyBaseCooldownSeconds
	}
	maximum := group.MaxCooldownSeconds
	if maximum < base {
		maximum = max(base, DefaultProxyMaxCooldownSeconds)
	}
	if recoveryFailures < 0 {
		recoveryFailures = 0
	}
	multiplier := math.Pow(2, float64(recoveryFailures))
	cooldown := int(float64(base) * multiplier)
	if cooldown > maximum || cooldown < 0 {
		return maximum
	}
	return cooldown
}

func ClaimRecoveringProxyProbe(proxyId int) (bool, error) {
	result := DB.Model(&Proxy{}).
		Where("id = ? AND enabled = ? AND status = ? AND recovery_probe_remaining > 0", proxyId, true, ProxyStatusRecovering).
		UpdateColumn("recovery_probe_remaining", gorm.Expr("recovery_probe_remaining - 1"))
	return result.RowsAffected == 1, result.Error
}

func GetProxyHealthTarget(proxyId int) (*ProxyHealthCheckTarget, error) {
	proxy, err := GetProxyById(proxyId)
	if err != nil {
		return nil, err
	}
	group, err := GetProxyGroupById(proxy.GroupId)
	if err != nil {
		return nil, err
	}
	return &ProxyHealthCheckTarget{Proxy: proxy, Group: group}, nil
}

type ProxyManualActionResult struct {
	ProxyId           int
	ProxyGroupId      int
	SwitchRequired    bool
	SwitchWaitSeconds int
}

func SetProxyManualPaused(proxyId int, paused bool) (ProxyManualActionResult, error) {
	result := ProxyManualActionResult{ProxyId: proxyId}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var proxy Proxy
		if err := tx.First(&proxy, proxyId).Error; err != nil {
			return err
		}
		var group ProxyGroup
		if err := tx.First(&group, proxy.GroupId).Error; err != nil {
			return err
		}
		result.ProxyGroupId = group.Id
		result.SwitchWaitSeconds = group.SwitchWaitSeconds
		toStatus := ProxyStatusUnavailable
		eventType := "manual_recovery_requested"
		if paused {
			toStatus = ProxyStatusPaused
			eventType = "manual_paused"
		}
		now := common.GetTimestamp()
		updates := map[string]interface{}{
			"status":                   toStatus,
			"cooldown_until":           0,
			"recovery_successes":       0,
			"recovery_probe_remaining": 0,
			"updated_at":               now,
		}
		if !paused {
			updates["health_failures"] = 0
			updates["last_health_error"] = ""
			updates["consecutive_timeouts"] = 0
			updates["window_samples"] = 0
			updates["window_timeouts"] = 0
			updates["window_timeout_ratio"] = 0
			updates["health_epoch_at"] = now
		}
		if err := tx.Model(&Proxy{}).Where("id = ?", proxy.Id).UpdateColumns(updates).Error; err != nil {
			return err
		}
		if paused && group.CurrentProxyId == proxy.Id {
			result.SwitchRequired = true
		}
		if proxy.Status != toStatus {
			return tx.Create(&ProxyStateEvent{
				ProxyId: proxy.Id, ProxyGroupId: group.Id, EventType: eventType,
				FromStatus: proxy.Status, ToStatus: toStatus, Reason: "manual",
			}).Error
		}
		return nil
	})
	return result, err
}

func PrepareManualProxyGroupSwitch(groupId int) (int, int, error) {
	currentProxyId := 0
	waitSeconds := DefaultProxySwitchWaitSeconds
	err := DB.Transaction(func(tx *gorm.DB) error {
		var group ProxyGroup
		if err := tx.First(&group, groupId).Error; err != nil {
			return err
		}
		currentProxyId = group.CurrentProxyId
		waitSeconds = group.SwitchWaitSeconds
		if currentProxyId <= 0 {
			return errors.New("proxy group has no current proxy")
		}
		return nil
	})
	return currentProxyId, waitSeconds, err
}

func IsProxyNotFound(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
