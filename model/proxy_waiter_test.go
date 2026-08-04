package model

import (
	"testing"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/stretchr/testify/require"
)

func TestProxyGroupWaitQueueEnforcesGlobalLimitAndReclaimsExpiredSlot(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&ProxyGroup{}, &ProxyGroupWaiter{}))
	group := &ProxyGroup{Name: "global-wait-queue-test", Enabled: true}
	require.NoError(t, DB.Create(group).Error)
	t.Cleanup(func() {
		DB.Where("group_id = ?", group.Id).Delete(&ProxyGroupWaiter{})
		DB.Delete(group)
	})

	now := common.GetTimestamp()
	first, err := EnterProxyGroupWaitQueue(group.Id, 1, now+60)
	require.NoError(t, err)
	require.Equal(t, 1, first.Slot)

	_, err = EnterProxyGroupWaitQueue(group.Id, 1, now+60)
	require.ErrorIs(t, err, ErrProxyGroupWaitQueueFull)

	metrics, err := ListProxyGroupWaitMetrics(now)
	require.NoError(t, err)
	require.EqualValues(t, 1, metrics[group.Id].WaitingRequests)
	require.Equal(t, first.CreatedAt, metrics[group.Id].OldestWaitStartedAt)
	require.Equal(t, first.ExpiresAt, metrics[group.Id].NearestWaitDeadlineAt)

	require.NoError(t, DB.Model(first).UpdateColumn("expires_at", now-1).Error)
	second, err := EnterProxyGroupWaitQueue(group.Id, 1, now+120)
	require.NoError(t, err)
	require.Equal(t, 1, second.Slot)
	require.NotEqual(t, first.Token, second.Token)
	require.NoError(t, LeaveProxyGroupWaitQueue(second.Token))

	metrics, err = ListProxyGroupWaitMetrics(now)
	require.NoError(t, err)
	require.Zero(t, metrics[group.Id].WaitingRequests)
}

func TestListProxyGroupsIncludesLiveWaitMetrics(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&ProxyGroup{}, &ProxyGroupWaiter{}))
	group := &ProxyGroup{Name: "wait-metrics-view-test", Enabled: true}
	require.NoError(t, DB.Create(group).Error)
	now := common.GetTimestamp()
	waiter, err := EnterProxyGroupWaitQueue(group.Id, 2, now+45)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = LeaveProxyGroupWaitQueue(waiter.Token)
		DB.Delete(group)
	})

	groups, err := ListProxyGroups()
	require.NoError(t, err)
	var found *ProxyGroup
	for _, candidate := range groups {
		if candidate.Id == group.Id {
			found = candidate
			break
		}
	}
	require.NotNil(t, found)
	require.EqualValues(t, 1, found.WaitingRequests)
	require.Positive(t, found.NearestWaitRemainingSeconds)
	require.Positive(t, found.LongestWaitRemainingSeconds)
}
