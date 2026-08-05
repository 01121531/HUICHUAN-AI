package model

import (
	"errors"

	"github.com/01121531/HUICHUAN-AI/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrProxyGroupSwitchLeaseLost = errors.New("proxy group switch lease lost")

// ProxyGroupSwitchLeaseState coordinates one switch across every instance
// sharing the same database. The owner token is never exposed through JSON.
type ProxyGroupSwitchLeaseState struct {
	CurrentProxyId  int
	Status          string
	SwitchLockOwner string
	SwitchLockUntil int64
}

func TryAcquireProxyGroupSwitchLease(groupId int, expectedCurrentProxyId int, owner string, leaseSeconds int) (bool, ProxyGroupSwitchLeaseState, error) {
	state := ProxyGroupSwitchLeaseState{}
	if groupId <= 0 || owner == "" {
		return false, state, errors.New("invalid proxy group switch lease")
	}
	if leaseSeconds <= 0 {
		leaseSeconds = DefaultProxySwitchWaitSeconds + 15
	}
	now := common.GetTimestamp()
	query := DB.Model(&ProxyGroup{}).
		Where("id = ? AND enabled = ? AND status <> ?", groupId, true, ProxyGroupStatusDisabled).
		Where("(status <> ? OR switch_lock_owner = '' OR switch_lock_owner IS NULL OR switch_lock_until <= ?)", ProxyGroupStatusSwitching, now)
	if expectedCurrentProxyId > 0 {
		query = query.Where("current_proxy_id = ?", expectedCurrentProxyId)
	}
	result := query.UpdateColumns(map[string]interface{}{
		"status":            ProxyGroupStatusSwitching,
		"switch_lock_owner": owner,
		"switch_lock_until": now + int64(leaseSeconds),
		"updated_at":        now,
	})
	if result.Error != nil {
		return false, state, result.Error
	}
	group, err := GetProxyGroupById(groupId)
	if err != nil {
		return false, state, err
	}
	state = proxyGroupSwitchLeaseState(group)
	return result.RowsAffected == 1 && group.SwitchLockOwner == owner, state, nil
}

func GetProxyGroupSwitchLeaseState(groupId int) (ProxyGroupSwitchLeaseState, error) {
	group, err := GetProxyGroupById(groupId)
	if err != nil {
		return ProxyGroupSwitchLeaseState{}, err
	}
	return proxyGroupSwitchLeaseState(group), nil
}

func proxyGroupSwitchLeaseState(group *ProxyGroup) ProxyGroupSwitchLeaseState {
	if group == nil {
		return ProxyGroupSwitchLeaseState{}
	}
	return ProxyGroupSwitchLeaseState{
		CurrentProxyId: group.CurrentProxyId, Status: group.Status,
		SwitchLockOwner: group.SwitchLockOwner, SwitchLockUntil: group.SwitchLockUntil,
	}
}

func CompleteProxyGroupSwitch(groupId int, failedProxyId int, owner string) (int, error) {
	nextProxyId := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		var group ProxyGroup
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&group, groupId).Error; err != nil {
			return err
		}
		if group.SwitchLockOwner != owner || group.SwitchLockUntil < common.GetTimestamp() {
			return ErrProxyGroupSwitchLeaseLost
		}
		var failed Proxy
		if err := tx.First(&failed, failedProxyId).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		} else {
			var err error
			nextProxyId, err = selectNextAvailableProxyId(tx, &failed)
			if err != nil {
				return err
			}
		}
		result := tx.Model(&ProxyGroup{}).
			Where("id = ? AND switch_lock_owner = ?", groupId, owner).
			UpdateColumns(map[string]interface{}{
				"current_proxy_id":  nextProxyId,
				"status":            ProxyGroupStatusAvailable,
				"switch_lock_owner": "",
				"switch_lock_until": 0,
				"updated_at":        common.GetTimestamp(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrProxyGroupSwitchLeaseLost
		}
		return nil
	})
	return nextProxyId, err
}

func CompleteProxyGroupRecoverySwitch(groupId int, targetProxyId int, owner string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		now := common.GetTimestamp()
		var group ProxyGroup
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&group, groupId).Error; err != nil {
			return err
		}
		if group.SwitchLockOwner != owner || group.SwitchLockUntil < now {
			return ErrProxyGroupSwitchLeaseLost
		}
		var target Proxy
		if err := tx.First(&target, targetProxyId).Error; err != nil {
			return err
		}
		if target.GroupId != groupId || !target.Enabled || target.Status != ProxyStatusRecovering {
			return errors.New("proxy is not ready for recovery probing")
		}
		result := tx.Model(&ProxyGroup{}).
			Where("id = ? AND switch_lock_owner = ?", groupId, owner).
			UpdateColumns(map[string]interface{}{
				"current_proxy_id":  targetProxyId,
				"status":            ProxyGroupStatusAvailable,
				"switch_lock_owner": "",
				"switch_lock_until": 0,
				"updated_at":        now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrProxyGroupSwitchLeaseLost
		}
		return nil
	})
}

func AbortProxyGroupSwitch(groupId int, owner string) error {
	return DB.Model(&ProxyGroup{}).
		Where("id = ? AND switch_lock_owner = ?", groupId, owner).
		UpdateColumns(map[string]interface{}{
			"status":            ProxyGroupStatusAvailable,
			"switch_lock_owner": "",
			"switch_lock_until": 0,
			"updated_at":        common.GetTimestamp(),
		}).Error
}

func RecoverExpiredProxyGroupSwitch(groupId int, now int64) (bool, error) {
	result := DB.Model(&ProxyGroup{}).
		Where("id = ? AND status = ?", groupId, ProxyGroupStatusSwitching).
		Where("(switch_lock_until > 0 AND switch_lock_until <= ?) OR switch_lock_owner = '' OR switch_lock_owner IS NULL", now).
		UpdateColumns(map[string]interface{}{
			"status":            ProxyGroupStatusAvailable,
			"switch_lock_owner": "",
			"switch_lock_until": 0,
			"updated_at":        now,
		})
	return result.RowsAffected == 1, result.Error
}

func RecoverExpiredProxyGroupSwitches(now int64) (int64, error) {
	result := DB.Model(&ProxyGroup{}).
		Where("status = ?", ProxyGroupStatusSwitching).
		Where("(switch_lock_until > 0 AND switch_lock_until <= ?) OR switch_lock_owner = '' OR switch_lock_owner IS NULL", now).
		UpdateColumns(map[string]interface{}{
			"status":            ProxyGroupStatusAvailable,
			"switch_lock_owner": "",
			"switch_lock_until": 0,
			"updated_at":        now,
		})
	return result.RowsAffected, result.Error
}

func HasExpiredProxyGroupSwitches(now int64) bool {
	var count int64
	err := DB.Model(&ProxyGroup{}).
		Where("status = ?", ProxyGroupStatusSwitching).
		Where("(switch_lock_until > 0 AND switch_lock_until <= ?) OR switch_lock_owner = '' OR switch_lock_owner IS NULL", now).
		Limit(1).
		Count(&count).Error
	return err == nil && count > 0
}
