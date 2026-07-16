package model

import (
	"errors"
	"strings"

	"github.com/01121531/HUICHUAN-AI/common"
	"gorm.io/gorm"
)

const (
	DatasetCaptureAuditActionView     = "view"
	DatasetCaptureAuditActionDownload = "download"

	DatasetCaptureAuditOutcomePrepared  = "prepared"
	DatasetCaptureAuditOutcomeDelivered = "delivered"
	DatasetCaptureAuditOutcomeFailed    = "failed"
)

// DatasetCaptureAccessAudit is one disclosure event. Item rows preserve the
// exact capture metadata even if the source capture is deleted later.
type DatasetCaptureAccessAudit struct {
	ID               uint64 `gorm:"primaryKey"`
	EventID          string `gorm:"size:64;uniqueIndex;not null"`
	Action           string `gorm:"size:16;index;not null"`
	Outcome          string `gorm:"size:16;index;not null"`
	OperatorUserID   int    `gorm:"index;not null"`
	OperatorUsername string `gorm:"size:255;index;not null"`
	OperatorRole     int    `gorm:"not null"`
	AuthMethod       string `gorm:"size:32;not null"`
	IP               string `gorm:"size:64"`
	Node             string `gorm:"size:128;index;not null"`
	SelectionMode    string `gorm:"size:32"`
	RecordCount      int    `gorm:"not null"`
	UserCount        int    `gorm:"not null"`
	Bytes            int64  `gorm:"not null"`
	StartTime        int64
	EndTime          int64
	Models           string `gorm:"type:text"`
	TokenIDs         string `gorm:"type:text"`
	Groups           string `gorm:"type:text"`
	ChannelIDs       string `gorm:"type:text"`
	UsernameFilter   string `gorm:"size:200"`
	CreatedAt        int64  `gorm:"index;not null"`
	CompletedAt      int64
}

type DatasetCaptureAccessAuditItem struct {
	ID               uint64 `gorm:"primaryKey"`
	AuditID          uint64 `gorm:"index;not null"`
	CaptureID        string `gorm:"size:24;index;not null"`
	UserID           int    `gorm:"index;not null"`
	Username         string `gorm:"size:255;not null"`
	TokenID          int    `gorm:"index;not null"`
	TokenName        string `gorm:"size:255;not null"`
	UserGroup        string `gorm:"size:64"`
	EffectiveModel   string `gorm:"size:255;index"`
	ChannelID        int    `gorm:"index"`
	SessionID        string `gorm:"size:16;index"`
	CaptureCreatedAt int64  `gorm:"index"`
}

type DatasetCaptureAccessAuditInput struct {
	Action           string
	OperatorUserID   int
	OperatorUsername string
	OperatorRole     int
	AuthMethod       string
	IP               string
	Node             string
	SelectionMode    string
	Bytes            int64
	StartTime        int64
	EndTime          int64
	Models           []string
	TokenIDs         []int
	Groups           []string
	ChannelIDs       []int
	UsernameFilter   string
	Records          []DatasetCaptureRecordSummary
}

type DatasetCaptureAccessAuditFilter struct {
	Action    string
	Admin     string
	Outcome   string
	StartTime int64
	EndTime   int64
}

type DatasetCaptureAccessAuditEntry struct {
	EventID          string `json:"event_id"`
	Action           string `json:"action"`
	Outcome          string `json:"outcome"`
	OperatorUserID   int    `json:"operator_user_id"`
	OperatorUsername string `json:"operator_username"`
	OperatorRole     int    `json:"operator_role"`
	AuthMethod       string `json:"auth_method"`
	IP               string `json:"ip"`
	Node             string `json:"node"`
	SelectionMode    string `json:"selection_mode"`
	RecordCount      int    `json:"record_count"`
	UserCount        int    `json:"user_count"`
	Bytes            int64  `json:"bytes"`
	CreatedAt        int64  `json:"created_at"`
	CompletedAt      int64  `json:"completed_at"`
	CaptureID        string `json:"capture_id"`
	UserID           int    `json:"user_id"`
	Username         string `json:"username"`
	TokenID          int    `json:"token_id"`
	TokenName        string `json:"token_name"`
	UserGroup        string `json:"user_group"`
	EffectiveModel   string `json:"effective_model"`
	ChannelID        int    `json:"channel_id"`
	SessionID        string `json:"session_id"`
	CaptureCreatedAt int64  `json:"capture_created_at"`
}

func BeginDatasetCaptureAccessAudit(input DatasetCaptureAccessAuditInput) (string, error) {
	if input.Action != DatasetCaptureAuditActionView && input.Action != DatasetCaptureAuditActionDownload {
		return "", errors.New("invalid dataset capture audit action")
	}
	if input.OperatorUserID <= 0 || strings.TrimSpace(input.OperatorUsername) == "" || len(input.Records) == 0 {
		return "", errors.New("dataset capture audit is incomplete")
	}
	userIDs := make(map[int]struct{})
	for _, record := range input.Records {
		userIDs[record.UserID] = struct{}{}
	}
	event := DatasetCaptureAccessAudit{
		EventID: common.NewRequestId(), Action: input.Action,
		Outcome:        DatasetCaptureAuditOutcomePrepared,
		OperatorUserID: input.OperatorUserID, OperatorUsername: input.OperatorUsername,
		OperatorRole: input.OperatorRole, AuthMethod: input.AuthMethod, IP: input.IP,
		Node: input.Node, SelectionMode: input.SelectionMode,
		RecordCount: len(input.Records), UserCount: len(userIDs), Bytes: input.Bytes,
		StartTime: input.StartTime, EndTime: input.EndTime,
		Models:         common.MapToJsonStr(map[string]interface{}{"values": input.Models}),
		TokenIDs:       common.MapToJsonStr(map[string]interface{}{"values": input.TokenIDs}),
		Groups:         common.MapToJsonStr(map[string]interface{}{"values": input.Groups}),
		ChannelIDs:     common.MapToJsonStr(map[string]interface{}{"values": input.ChannelIDs}),
		UsernameFilter: input.UsernameFilter, CreatedAt: common.GetTimestamp(),
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		items := make([]DatasetCaptureAccessAuditItem, 0, len(input.Records))
		for _, record := range input.Records {
			items = append(items, DatasetCaptureAccessAuditItem{
				AuditID: event.ID, CaptureID: record.CaptureID,
				UserID: record.UserID, Username: record.Username,
				TokenID: record.TokenID, TokenName: record.TokenName,
				UserGroup: record.UserGroup, EffectiveModel: record.EffectiveModel,
				ChannelID: record.ChannelID, SessionID: record.SessionID,
				CaptureCreatedAt: record.CapturedAt,
			})
		}
		return tx.CreateInBatches(items, 500).Error
	})
	if err != nil {
		return "", err
	}
	return event.EventID, nil
}

func CompleteDatasetCaptureAccessAudit(eventID, outcome string) error {
	if outcome != DatasetCaptureAuditOutcomeDelivered && outcome != DatasetCaptureAuditOutcomeFailed {
		return errors.New("invalid dataset capture audit outcome")
	}
	result := DB.Model(&DatasetCaptureAccessAudit{}).
		Where("event_id = ? AND outcome = ?", eventID, DatasetCaptureAuditOutcomePrepared).
		Updates(map[string]interface{}{"outcome": outcome, "completed_at": common.GetTimestamp()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("dataset capture audit event is not pending")
	}
	return nil
}

func ListDatasetCaptureAccessAudits(filter DatasetCaptureAccessAuditFilter, page, pageSize int) ([]DatasetCaptureAccessAuditEntry, int64, error) {
	query := DB.Table("dataset_capture_access_audit_items AS item").
		Joins("JOIN dataset_capture_access_audits AS audit ON audit.id = item.audit_id")
	if filter.Action != "" {
		query = query.Where("audit.action = ?", filter.Action)
	}
	if filter.Outcome != "" {
		query = query.Where("audit.outcome = ?", filter.Outcome)
	}
	if filter.StartTime > 0 {
		query = query.Where("audit.created_at >= ?", filter.StartTime)
	}
	if filter.EndTime > 0 {
		query = query.Where("audit.created_at <= ?", filter.EndTime)
	}
	if admin := strings.TrimSpace(filter.Admin); admin != "" {
		query = query.Where("audit.operator_username LIKE ?", "%"+admin+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	entries := make([]DatasetCaptureAccessAuditEntry, 0, pageSize)
	err := query.Select(`
		audit.event_id, audit.action, audit.outcome, audit.operator_user_id,
		audit.operator_username, audit.operator_role, audit.auth_method, audit.ip,
		audit.node, audit.selection_mode, audit.record_count, audit.user_count,
		audit.bytes, audit.created_at, audit.completed_at,
		item.capture_id, item.user_id, item.username, item.token_id, item.token_name,
		item.user_group, item.effective_model, item.channel_id, item.session_id,
		item.capture_created_at`).
		Order("audit.created_at DESC, audit.id DESC, item.id ASC").
		Offset((page - 1) * pageSize).Limit(pageSize).Scan(&entries).Error
	return entries, total, err
}
