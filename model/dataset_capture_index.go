package model

import (
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/pkg/datasetcapture"
	"gorm.io/gorm/clause"
)

type DatasetCaptureIndex struct {
	ID             uint64 `gorm:"primaryKey"`
	CaptureID      string `gorm:"size:24;uniqueIndex;not null"`
	Node           string `gorm:"size:128;index;not null"`
	FileID         string `gorm:"size:24;uniqueIndex:idx_dataset_capture_file_row,priority:1;index;not null"`
	Row            int64  `gorm:"uniqueIndex:idx_dataset_capture_file_row,priority:2;not null"`
	UserID         int    `gorm:"index;not null"`
	TokenID        int    `gorm:"index;not null"`
	TokenScope     string `gorm:"size:32;index;not null"`
	UserGroup      string `gorm:"size:64;index"`
	RequestedModel string `gorm:"size:255;index"`
	EffectiveModel string `gorm:"size:255;index;not null"`
	ChannelID      int    `gorm:"index;not null"`
	SessionID      string `gorm:"size:16;index;not null"`
	CapturedAt     int64  `gorm:"index;not null"`
	RecordSize     int64  `gorm:"not null"`
}

func NewDatasetCaptureIndex(result datasetcapture.WriteResult) DatasetCaptureIndex {
	record := result.Record
	return DatasetCaptureIndex{
		CaptureID:      result.CaptureID,
		Node:           result.Node,
		FileID:         result.FileID,
		Row:            result.Row,
		UserID:         numericCaptureScope(record.Storage.UserKey),
		TokenID:        numericCaptureScope(record.Storage.TokenKey),
		TokenScope:     record.Storage.TokenKey,
		UserGroup:      record.Storage.UserGroup,
		RequestedModel: record.Storage.RequestedModel,
		EffectiveModel: record.Model,
		ChannelID:      record.Storage.ChannelID,
		SessionID:      record.SessionID,
		CapturedAt:     captureTimestamp(record.CreatedAt),
		RecordSize:     result.Bytes,
	}
}

func UpsertDatasetCaptureIndex(index DatasetCaptureIndex) error {
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "capture_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"node", "file_id", "row", "user_id", "token_id", "token_scope",
			"user_group", "requested_model", "effective_model", "channel_id",
			"session_id", "captured_at", "record_size",
		}),
	}).Create(&index).Error
}

func BackfillDatasetCaptureIndex(index DatasetCaptureIndex) error {
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "capture_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"node", "file_id", "row", "user_id", "token_id", "token_scope",
			"effective_model", "session_id", "captured_at", "record_size",
		}),
	}).Create(&index).Error
}

func DeleteStaleDatasetCaptureIndices(node string, activeFileIDs []string) error {
	query := DB.Where("node = ?", node)
	if len(activeFileIDs) > 0 {
		query = query.Where("file_id NOT IN ?", activeFileIDs)
	}
	return query.Delete(&DatasetCaptureIndex{}).Error
}

func DeleteDatasetCaptureIndicesAfterRow(fileID string, lastRow int64) error {
	return DB.Where("file_id = ? AND row > ?", fileID, lastRow).Delete(&DatasetCaptureIndex{}).Error
}

func DeleteDatasetCaptureIndicesByFile(node, fileID string) (int64, error) {
	result := DB.Where("node = ? AND file_id = ?", node, fileID).Delete(&DatasetCaptureIndex{})
	return result.RowsAffected, result.Error
}

func numericCaptureScope(value string) int {
	id, err := strconv.Atoi(value)
	if err != nil || id < 1 {
		return 0
	}
	return id
}

func captureTimestamp(value *string) int64 {
	if value == nil {
		return 0
	}
	parsed, err := time.Parse(time.RFC3339Nano, *value)
	if err != nil {
		return 0
	}
	return parsed.Unix()
}
