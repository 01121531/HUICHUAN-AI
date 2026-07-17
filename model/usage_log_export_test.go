package model

import (
	"testing"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUsageLogExportQueryAppliesFiltersAndSelection(t *testing.T) {
	truncateTables(t)

	logs := []*Log{
		{
			UserId: 1, CreatedAt: 100, Type: LogTypeConsume, Username: "alice",
			TokenName: "token-a", ModelName: "gpt-4o", ChannelId: 10, Group: "team-a",
			RequestId: "request-a", UpstreamRequestId: "upstream-a",
		},
		{
			UserId: 1, CreatedAt: 200, Type: LogTypeError, Username: "alice",
			TokenName: "token-b", ModelName: "claude-3", ChannelId: 20, Group: "team-b",
			RequestId: "request-b", UpstreamRequestId: "upstream-b",
		},
		{
			UserId: 2, CreatedAt: 300, Type: LogTypeConsume, Username: "bob",
			TokenName: "token-a", ModelName: "gpt-4o", ChannelId: 10, Group: "team-a",
			RequestId: "request-c", UpstreamRequestId: "upstream-c",
		},
	}
	for _, log := range logs {
		require.NoError(t, createLog(log))
	}

	tests := []struct {
		name   string
		filter UsageLogExportFilter
		want   []string
	}{
		{name: "user scope", filter: UsageLogExportFilter{UserID: 1}, want: []string{"request-a", "request-b"}},
		{name: "type", filter: UsageLogExportFilter{LogType: LogTypeError}, want: []string{"request-b"}},
		{name: "time range", filter: UsageLogExportFilter{StartTimestamp: 150, EndTimestamp: 250}, want: []string{"request-b"}},
		{name: "model", filter: UsageLogExportFilter{ModelName: "gpt-4o"}, want: []string{"request-a", "request-c"}},
		{name: "username", filter: UsageLogExportFilter{Username: "bob"}, want: []string{"request-c"}},
		{name: "token", filter: UsageLogExportFilter{TokenName: "token-b"}, want: []string{"request-b"}},
		{name: "channel", filter: UsageLogExportFilter{ChannelID: 20}, want: []string{"request-b"}},
		{name: "group", filter: UsageLogExportFilter{Group: "team-b"}, want: []string{"request-b"}},
		{name: "request id", filter: UsageLogExportFilter{RequestID: "request-a"}, want: []string{"request-a"}},
		{name: "upstream request id", filter: UsageLogExportFilter{UpstreamRequestID: "upstream-c"}, want: []string{"request-c"}},
		{
			name: "selected id and request id must match the same row",
			filter: UsageLogExportFilter{
				SelectedIDs:        []int{logs[0].Id, logs[1].Id},
				SelectedRequestIDs: []string{"request-b"},
			},
			want: []string{"request-b"},
		},
		{name: "missing selection", filter: UsageLogExportFilter{SelectedIDs: []int{999999}}, want: []string{}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ListUsageLogsForExport(test.filter, 100)
			require.NoError(t, err)
			requestIDs := make([]string, 0, len(got))
			for _, log := range got {
				requestIDs = append(requestIDs, log.RequestId)
			}
			assert.Equal(t, test.want, requestIDs)

			count, err := CountUsageLogsForExport(test.filter)
			require.NoError(t, err)
			assert.Equal(t, int64(len(test.want)), count)
		})
	}
}

func TestUsageLogExportQueryExcludesDeletedRows(t *testing.T) {
	truncateTables(t)

	log := &Log{
		UserId: 1, CreatedAt: 100, Type: LogTypeConsume, Username: "alice",
		RequestId: "deleted-request",
	}
	require.NoError(t, createLog(log))
	require.NoError(t, LOG_DB.Delete(&Log{}, log.Id).Error)

	got, err := ListUsageLogsForExport(UsageLogExportFilter{
		SelectedIDs:        []int{log.Id},
		SelectedRequestIDs: []string{log.RequestId},
	}, 100)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestFreezeUsageLogExportFilterExcludesRowsCreatedLater(t *testing.T) {
	truncateTables(t)
	first := &Log{UserId: 1, CreatedAt: 100, Type: LogTypeConsume, RequestId: "snapshot-first"}
	require.NoError(t, createLog(first))

	filter, err := FreezeUsageLogExportFilter(UsageLogExportFilter{UserID: 1})
	require.NoError(t, err)
	require.Equal(t, first.Id, filter.SnapshotMaxID)
	require.NoError(t, createLog(&Log{
		UserId: 1, CreatedAt: 101, Type: LogTypeConsume, RequestId: "snapshot-later",
	}))

	logs, err := ListUsageLogsForExport(filter, 100)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, "snapshot-first", logs[0].RequestId)

	empty, err := FreezeUsageLogExportFilter(UsageLogExportFilter{UserID: 999})
	require.NoError(t, err)
	require.True(t, empty.SnapshotEmpty)
	require.NoError(t, createLog(&Log{
		UserId: 999, CreatedAt: 102, Type: LogTypeConsume, RequestId: "must-not-appear",
	}))
	count, err := CountUsageLogsForExport(empty)
	require.NoError(t, err)
	assert.Zero(t, count)
}

func TestListUsageLogsForExportCursorDoesNotRepeatRows(t *testing.T) {
	truncateTables(t)

	for index := 0; index < 7; index++ {
		require.NoError(t, createLog(&Log{
			UserId:    1,
			CreatedAt: int64(100 + index),
			Type:      LogTypeConsume,
			RequestId: "cursor-" + string(rune('a'+index)),
		}))
	}

	cursor := UsageLogExportCursor{}
	var requestIDs []string
	for {
		batch, next, err := ListUsageLogsForExportCursor(
			UsageLogExportFilter{UserID: 1},
			cursor,
			3,
		)
		require.NoError(t, err)
		if len(batch) == 0 {
			break
		}
		for _, log := range batch {
			requestIDs = append(requestIDs, log.RequestId)
		}
		assert.Greater(t, next.ID, cursor.ID)
		cursor = next
	}

	assert.Equal(t, []string{
		"cursor-a", "cursor-b", "cursor-c", "cursor-d",
		"cursor-e", "cursor-f", "cursor-g",
	}, requestIDs)
}

func TestClickHouseUsageLogExportCursorHandlesLegacyEmptyRequestIDs(t *testing.T) {
	truncateTables(t)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeClickHouse)
	t.Cleanup(func() {
		common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	})

	for index := 0; index < 7; index++ {
		require.NoError(t, LOG_DB.Create(&Log{
			UserId: 1, CreatedAt: 100, Type: LogTypeConsume, RequestId: "",
		}).Error)
	}
	for index := 0; index < 2; index++ {
		require.NoError(t, LOG_DB.Create(&Log{
			UserId: 1, CreatedAt: 100, Type: LogTypeConsume,
			RequestId: "stable-" + string(rune('a'+index)),
		}).Error)
	}
	require.NoError(t, LOG_DB.Create(&Log{
		UserId: 1, CreatedAt: 101, Type: LogTypeConsume, RequestId: "next-time",
	}).Error)

	cursor := UsageLogExportCursor{}
	var exported []*Log
	for {
		batch, next, err := ListUsageLogsForExportCursor(
			UsageLogExportFilter{UserID: 1},
			cursor,
			3,
		)
		require.NoError(t, err)
		if len(batch) == 0 {
			break
		}
		exported = append(exported, batch...)
		cursor = next
	}

	require.Len(t, exported, 10)
	emptyCount := 0
	for _, log := range exported {
		if log.RequestId == "" {
			emptyCount++
		}
	}
	assert.Equal(t, 7, emptyCount)
	assert.Equal(t, "next-time", exported[len(exported)-1].RequestId)
}
