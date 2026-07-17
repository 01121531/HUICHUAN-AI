package model

import (
	"errors"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/types"
	"gorm.io/gorm"
)

type UsageLogExportFilter struct {
	UserID             int      `json:"user_id,omitempty"`
	LogType            int      `json:"type,omitempty"`
	StartTimestamp     int64    `json:"start_timestamp,omitempty"`
	EndTimestamp       int64    `json:"end_timestamp,omitempty"`
	ModelName          string   `json:"model_name,omitempty"`
	Username           string   `json:"username,omitempty"`
	TokenName          string   `json:"token_name,omitempty"`
	ChannelID          int      `json:"channel,omitempty"`
	Group              string   `json:"group,omitempty"`
	RequestID          string   `json:"request_id,omitempty"`
	UpstreamRequestID  string   `json:"upstream_request_id,omitempty"`
	SelectedIDs        []int    `json:"selected_ids,omitempty"`
	SelectedRequestIDs []string `json:"selected_request_ids,omitempty"`
	SnapshotMaxID      int      `json:"snapshot_max_id,omitempty"`
	SnapshotMaxCreated int64    `json:"snapshot_max_created_at,omitempty"`
	SnapshotMaxRequest string   `json:"snapshot_max_request_id,omitempty"`
	SnapshotEmpty      bool     `json:"snapshot_empty,omitempty"`
}

type UsageLogExportCursor struct {
	ID                 int
	CreatedAt          int64
	RequestID          string
	EmptyRequestOffset int
}

func usageLogExportQuery(filter UsageLogExportFilter) (*gorm.DB, error) {
	tx := LOG_DB.Model(&Log{})
	if filter.SnapshotEmpty {
		return tx.Where("1 = 0"), nil
	}
	var err error
	if filter.UserID > 0 {
		tx = tx.Where("logs.user_id = ?", filter.UserID)
	}
	if filter.LogType != LogTypeUnknown {
		tx = tx.Where("logs.type = ?", filter.LogType)
	}
	if tx, err = applyExplicitLogTextFilter(tx, "logs.model_name", filter.ModelName); err != nil {
		return nil, err
	}
	if tx, err = applyExplicitLogTextFilter(tx, "logs.username", filter.Username); err != nil {
		return nil, err
	}
	if filter.TokenName != "" {
		tx = tx.Where("logs.token_name = ?", filter.TokenName)
	}
	if filter.ChannelID != 0 {
		tx = tx.Where("logs.channel_id = ?", filter.ChannelID)
	}
	if filter.Group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", filter.Group)
	}
	if filter.RequestID != "" {
		tx = tx.Where("logs.request_id = ?", filter.RequestID)
	}
	if filter.UpstreamRequestID != "" {
		tx = tx.Where("logs.upstream_request_id = ?", filter.UpstreamRequestID)
	}
	if filter.StartTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", filter.StartTimestamp)
	}
	if filter.EndTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", filter.EndTimestamp)
	}

	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		if len(filter.SelectedRequestIDs) > 0 {
			tx = tx.Where("logs.request_id IN ?", filter.SelectedRequestIDs)
		} else if len(filter.SelectedIDs) > 0 {
			return nil, errors.New("ClickHouse selected export requires request_id")
		}
	} else if len(filter.SelectedIDs) > 0 {
		tx = tx.Where("logs.id IN ?", filter.SelectedIDs)
		if len(filter.SelectedRequestIDs) > 0 {
			tx = tx.Where("logs.request_id IN ?", filter.SelectedRequestIDs)
		}
	} else if len(filter.SelectedRequestIDs) > 0 {
		tx = tx.Where("logs.request_id IN ?", filter.SelectedRequestIDs)
	}
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		if filter.SnapshotMaxCreated > 0 || filter.SnapshotMaxRequest != "" {
			tx = tx.Where(
				"(logs.created_at < ?) OR (logs.created_at = ? AND logs.request_id <= ?)",
				filter.SnapshotMaxCreated,
				filter.SnapshotMaxCreated,
				filter.SnapshotMaxRequest,
			)
		}
	} else if filter.SnapshotMaxID > 0 {
		tx = tx.Where("logs.id <= ?", filter.SnapshotMaxID)
	}
	return tx, nil
}

func FreezeUsageLogExportFilter(filter UsageLogExportFilter) (UsageLogExportFilter, error) {
	filter.SnapshotMaxID = 0
	filter.SnapshotMaxCreated = 0
	filter.SnapshotMaxRequest = ""
	filter.SnapshotEmpty = false
	tx, err := usageLogExportQuery(filter)
	if err != nil {
		return filter, err
	}
	order := "logs.id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = "logs.created_at desc, logs.request_id desc"
	}
	var rows []*Log
	if err = tx.Order(order).Limit(1).Find(&rows).Error; err != nil {
		return filter, err
	}
	if len(rows) == 0 {
		filter.SnapshotEmpty = true
		return filter, nil
	}
	last := rows[0]
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		filter.SnapshotMaxCreated = last.CreatedAt
		filter.SnapshotMaxRequest = last.RequestId
	} else {
		filter.SnapshotMaxID = last.Id
	}
	return filter, nil
}

func CountUsageLogsForExport(filter UsageLogExportFilter) (int64, error) {
	tx, err := usageLogExportQuery(filter)
	if err != nil {
		return 0, err
	}
	var total int64
	err = tx.Count(&total).Error
	return total, err
}

func ListUsageLogsForExport(filter UsageLogExportFilter, limit int) ([]*Log, error) {
	tx, err := usageLogExportQuery(filter)
	if err != nil {
		return nil, err
	}
	order := "logs.id asc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = "logs.created_at asc, logs.request_id asc"
	}
	var logs []*Log
	if err = tx.Order(order).Limit(limit).Find(&logs).Error; err != nil {
		return nil, err
	}
	if err = FillLogChannelNames(logs); err != nil {
		return logs, err
	}
	return logs, nil
}

func ListUsageLogsForExportCursor(filter UsageLogExportFilter, cursor UsageLogExportCursor, limit int) ([]*Log, UsageLogExportCursor, error) {
	tx, err := usageLogExportQuery(filter)
	if err != nil {
		return nil, cursor, err
	}
	if limit <= 0 {
		limit = common.UsageLogExportBatchSize()
	}
	order := "logs.id asc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = "logs.created_at asc, logs.request_id asc"
		if cursor.EmptyRequestOffset > 0 {
			var emptyRequestLogs []*Log
			err = tx.
				Where("logs.created_at = ? AND logs.request_id = ?", cursor.CreatedAt, "").
				Order(order).
				Offset(cursor.EmptyRequestOffset).
				Limit(limit).
				Find(&emptyRequestLogs).Error
			if err != nil {
				return nil, cursor, err
			}
			if len(emptyRequestLogs) > 0 {
				cursor.EmptyRequestOffset += len(emptyRequestLogs)
				if len(emptyRequestLogs) < limit {
					cursor.EmptyRequestOffset = 0
				}
				if err = FillLogChannelNames(emptyRequestLogs); err != nil {
					return emptyRequestLogs, cursor, err
				}
				return emptyRequestLogs, cursor, nil
			}
			cursor.EmptyRequestOffset = 0
		}
		if cursor.CreatedAt > 0 || cursor.RequestID != "" {
			tx = tx.Where(
				"(logs.created_at > ?) OR (logs.created_at = ? AND logs.request_id > ?)",
				cursor.CreatedAt,
				cursor.CreatedAt,
				cursor.RequestID,
			)
		}
	} else if cursor.ID > 0 {
		tx = tx.Where("logs.id > ?", cursor.ID)
	}

	var logs []*Log
	if err = tx.Order(order).Limit(limit).Find(&logs).Error; err != nil {
		return nil, cursor, err
	}
	if err = FillLogChannelNames(logs); err != nil {
		return logs, cursor, err
	}
	if len(logs) > 0 {
		last := logs[len(logs)-1]
		cursor.ID = last.Id
		cursor.CreatedAt = last.CreatedAt
		cursor.RequestID = last.RequestId
		cursor.EmptyRequestOffset = 0
		if common.UsingLogDatabase(common.DatabaseTypeClickHouse) && last.RequestId == "" {
			for index := len(logs) - 1; index >= 0; index-- {
				if logs[index].CreatedAt != last.CreatedAt || logs[index].RequestId != "" {
					break
				}
				cursor.EmptyRequestOffset++
			}
		}
	}
	return logs, cursor, nil
}

func FillLogChannelNames(logs []*Log) error {
	channelIDs := types.NewSet[int]()
	for _, log := range logs {
		if log != nil && log.ChannelId != 0 {
			channelIDs.Add(log.ChannelId)
		}
	}
	if channelIDs.Len() == 0 {
		return nil
	}
	var channels []struct {
		ID   int    `gorm:"column:id"`
		Name string `gorm:"column:name"`
	}
	if err := DB.Table("channels").Select("id, name").Where("id IN ?", channelIDs.Items()).Find(&channels).Error; err != nil {
		return err
	}
	channelNames := make(map[int]string, len(channels))
	for _, channel := range channels {
		channelNames[channel.ID] = channel.Name
	}
	for _, log := range logs {
		if log != nil {
			log.ChannelName = channelNames[log.ChannelId]
		}
	}
	return nil
}
