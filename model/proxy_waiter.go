package model

import (
	"errors"
	"time"

	"github.com/01121531/HUICHUAN-AI/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrProxyGroupWaitQueueFull = errors.New("proxy group waiting queue is full")

// ProxyGroupWaiter is a crash-safe, cross-instance waiting slot. The
// (group_id, slot) unique key is the global capacity guard; expires_at releases
// slots left behind by a stopped instance.
type ProxyGroupWaiter struct {
	Id        int    `json:"id" gorm:"primaryKey"`
	Token     string `json:"-" gorm:"type:varchar(64);not null;uniqueIndex"`
	GroupId   int    `json:"group_id" gorm:"not null;uniqueIndex:idx_proxy_group_waiter_slot,priority:1;index"`
	Slot      int    `json:"slot" gorm:"not null;uniqueIndex:idx_proxy_group_waiter_slot,priority:2"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;not null;index"`
	ExpiresAt int64  `json:"expires_at" gorm:"bigint;not null;index"`
}

func (ProxyGroupWaiter) TableName() string { return "proxy_group_waiters" }

func (waiter *ProxyGroupWaiter) BeforeCreate(_ *gorm.DB) error {
	if waiter.CreatedAt <= 0 {
		waiter.CreatedAt = common.GetTimestamp()
	}
	if waiter.ExpiresAt <= waiter.CreatedAt {
		waiter.ExpiresAt = waiter.CreatedAt + int64(DefaultProxySwitchWaitSeconds)
	}
	return nil
}

type ProxyGroupWaitMetric struct {
	GroupId               int   `json:"group_id"`
	WaitingRequests       int64 `json:"waiting_requests"`
	OldestWaitStartedAt   int64 `json:"oldest_wait_started_at"`
	NearestWaitDeadlineAt int64 `json:"nearest_wait_deadline_at"`
	LongestWaitDeadlineAt int64 `json:"longest_wait_deadline_at"`
}

// EnterProxyGroupWaitQueue atomically reserves one of the group's globally
// unique waiting slots. It does not rely on process memory, so every instance
// sharing the database observes the same queue limit.
func EnterProxyGroupWaitQueue(groupId int, maxWaiting int, expiresAt int64) (*ProxyGroupWaiter, error) {
	if groupId <= 0 {
		return nil, errors.New("invalid proxy group")
	}
	if maxWaiting <= 0 {
		maxWaiting = DefaultProxyMaxWaitingRequests
	}
	now := common.GetTimestamp()
	if expiresAt <= now {
		expiresAt = now + int64(DefaultProxySwitchWaitSeconds)
	}
	if err := DeleteExpiredProxyGroupWaiters(groupId, now); err != nil {
		return nil, err
	}
	token := common.GetUUID()
	for slot := 1; slot <= maxWaiting; slot++ {
		waiter := &ProxyGroupWaiter{
			Token: token, GroupId: groupId, Slot: slot, CreatedAt: now, ExpiresAt: expiresAt,
		}
		result := DB.Clauses(clause.OnConflict{DoNothing: true}).Create(waiter)
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			continue
		}
		// Some MySQL client modes report a no-op duplicate as affected. Verify
		// ownership by token before treating the slot as acquired.
		var stored ProxyGroupWaiter
		if err := DB.Where("token = ?", token).First(&stored).Error; err == nil {
			return &stored, nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	return nil, ErrProxyGroupWaitQueueFull
}

func LeaveProxyGroupWaitQueue(token string) error {
	if token == "" {
		return nil
	}
	return DB.Where("token = ?", token).Delete(&ProxyGroupWaiter{}).Error
}

func DeleteExpiredProxyGroupWaiters(groupId int, now int64) error {
	if !DB.Migrator().HasTable(&ProxyGroupWaiter{}) {
		return nil
	}
	query := DB.Where("expires_at <= ?", now)
	if groupId > 0 {
		query = query.Where("group_id = ?", groupId)
	}
	return query.Delete(&ProxyGroupWaiter{}).Error
}

func ListProxyGroupWaitMetrics(now int64) (map[int]ProxyGroupWaitMetric, error) {
	if now <= 0 {
		now = time.Now().Unix()
	}
	if !DB.Migrator().HasTable(&ProxyGroupWaiter{}) {
		return map[int]ProxyGroupWaitMetric{}, nil
	}
	var rows []ProxyGroupWaitMetric
	err := DB.Model(&ProxyGroupWaiter{}).
		Select("group_id, COUNT(*) AS waiting_requests, MIN(created_at) AS oldest_wait_started_at, MIN(expires_at) AS nearest_wait_deadline_at, MAX(expires_at) AS longest_wait_deadline_at").
		Where("expires_at > ?", now).
		Group("group_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	metrics := make(map[int]ProxyGroupWaitMetric, len(rows))
	for _, row := range rows {
		metrics[row.GroupId] = row
	}
	return metrics, nil
}

func PopulateProxyGroupWaitMetrics(groups []*ProxyGroup, now int64) error {
	metrics, err := ListProxyGroupWaitMetrics(now)
	if err != nil {
		return err
	}
	for _, group := range groups {
		if group == nil {
			continue
		}
		metric := metrics[group.Id]
		group.WaitingRequests = metric.WaitingRequests
		group.OldestWaitStartedAt = metric.OldestWaitStartedAt
		group.NearestWaitDeadlineAt = metric.NearestWaitDeadlineAt
		group.LongestWaitDeadlineAt = metric.LongestWaitDeadlineAt
		if metric.NearestWaitDeadlineAt > now {
			group.NearestWaitRemainingSeconds = metric.NearestWaitDeadlineAt - now
		}
		if metric.LongestWaitDeadlineAt > now {
			group.LongestWaitRemainingSeconds = metric.LongestWaitDeadlineAt - now
		}
	}
	return nil
}
