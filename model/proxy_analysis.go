package model

import (
	"errors"
	"math"
	"strings"

	"github.com/01121531/HUICHUAN-AI/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const ProxyLogAnalysisCursorName = "managed_proxy_logs"

// ProxyLogAnalysis records one idempotent health decision derived from a
// generic usage/error log. It intentionally contains no proxy credentials.
type ProxyLogAnalysis struct {
	Id                   int64   `json:"id" gorm:"primaryKey"`
	AnalysisKey          string  `json:"analysis_key" gorm:"type:varchar(64);uniqueIndex"`
	LogId                int     `json:"log_id"`
	RequestId            string  `json:"request_id" gorm:"type:varchar(64);index"`
	LogType              int     `json:"log_type" gorm:"index"`
	LogCreatedAt         int64   `json:"log_created_at" gorm:"bigint;index:idx_proxy_analysis_order,priority:1"`
	ProxyId              int     `json:"proxy_id" gorm:"index;index:idx_proxy_analysis_order,priority:2"`
	ProxyGroupId         int     `json:"proxy_group_id" gorm:"index"`
	ChannelId            int     `json:"channel_id" gorm:"index"`
	IsStream             bool    `json:"is_stream"`
	UseTimeSeconds       int     `json:"use_time_seconds"`
	CompletionTokens     int     `json:"completion_tokens"`
	FirstResponseTimeMs  int     `json:"first_response_time_ms"`
	TokensPerSecond      float64 `json:"tokens_per_second"`
	Counted              bool    `json:"counted" gorm:"index"`
	IsTimeout            bool    `json:"is_timeout" gorm:"index"`
	FirstResponseTimeout bool    `json:"first_response_timeout"`
	DurationTimeout      bool    `json:"duration_timeout"`
	ThroughputTimeout    bool    `json:"throughput_timeout"`
	Reason               string  `json:"reason" gorm:"type:varchar(255)"`
	CreatedAt            int64   `json:"created_at" gorm:"bigint"`
}

func (ProxyLogAnalysis) TableName() string { return "proxy_log_analyses" }

func (analysis *ProxyLogAnalysis) BeforeCreate(_ *gorm.DB) error {
	if analysis.CreatedAt == 0 {
		analysis.CreatedAt = common.GetTimestamp()
	}
	return nil
}

// ProxyLogAnalysisCursor tracks the durable high-water mark in the generic
// log store. request_id is used as the ClickHouse-compatible tie breaker.
type ProxyLogAnalysisCursor struct {
	Name          string `json:"name" gorm:"type:varchar(64);primaryKey"`
	LastCreatedAt int64  `json:"last_created_at" gorm:"bigint;default:0"`
	LastRequestId string `json:"last_request_id" gorm:"type:varchar(64);default:''"`
	LastLogId     int    `json:"last_log_id" gorm:"default:0"`
	LastLogType   int    `json:"last_log_type" gorm:"default:0"`
	UpdatedAt     int64  `json:"updated_at" gorm:"bigint"`
}

func (ProxyLogAnalysisCursor) TableName() string { return "proxy_log_analysis_cursors" }

// ProxyStateEvent is the credential-free audit trail for automatic and manual
// proxy state transitions.
type ProxyStateEvent struct {
	Id           int64  `json:"id" gorm:"primaryKey"`
	ProxyId      int    `json:"proxy_id" gorm:"index"`
	ProxyGroupId int    `json:"proxy_group_id" gorm:"index"`
	AnalysisId   int64  `json:"analysis_id" gorm:"index"`
	EventType    string `json:"event_type" gorm:"type:varchar(64);index"`
	FromStatus   string `json:"from_status" gorm:"type:varchar(32)"`
	ToStatus     string `json:"to_status" gorm:"type:varchar(32)"`
	Reason       string `json:"reason" gorm:"type:varchar(255)"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint;index"`
}

func (ProxyStateEvent) TableName() string { return "proxy_state_events" }

func (event *ProxyStateEvent) BeforeCreate(_ *gorm.DB) error {
	if event.CreatedAt == 0 {
		event.CreatedAt = common.GetTimestamp()
	}
	return nil
}

type ProxyLogAnalysisApplyResult struct {
	Inserted          bool `json:"inserted"`
	Paused            bool `json:"paused"`
	SwitchRequired    bool `json:"switch_required"`
	ProxyId           int  `json:"proxy_id"`
	ProxyGroupId      int  `json:"proxy_group_id"`
	SwitchWaitSeconds int  `json:"switch_wait_seconds"`
}

func HasEnabledManagedProxies() bool {
	var count int64
	return DB.Model(&Proxy{}).Where("enabled = ?", true).Count(&count).Error == nil && count > 0
}

func ListProxyLogAnalyses(proxyId int, limit int) ([]*ProxyLogAnalysis, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	query := DB.Model(&ProxyLogAnalysis{})
	if proxyId > 0 {
		query = query.Where("proxy_id = ?", proxyId)
	}
	var analyses []*ProxyLogAnalysis
	err := query.Order("log_created_at desc, id desc").Limit(limit).Find(&analyses).Error
	return analyses, err
}

func ListProxyStateEvents(proxyId int, limit int) ([]*ProxyStateEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	query := DB.Model(&ProxyStateEvent{})
	if proxyId > 0 {
		query = query.Where("proxy_id = ?", proxyId)
	}
	var events []*ProxyStateEvent
	err := query.Order("created_at desc, id desc").Limit(limit).Find(&events).Error
	return events, err
}

func GetProxyLogAnalysisCursor() (*ProxyLogAnalysisCursor, error) {
	var cursor ProxyLogAnalysisCursor
	err := DB.Where("name = ?", ProxyLogAnalysisCursorName).First(&cursor).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &ProxyLogAnalysisCursor{Name: ProxyLogAnalysisCursorName}, nil
	}
	return &cursor, err
}

func SaveProxyLogAnalysisCursor(createdAt int64, requestId string, logId int, logType int) error {
	now := common.GetTimestamp()
	cursor := &ProxyLogAnalysisCursor{
		Name:          ProxyLogAnalysisCursorName,
		LastCreatedAt: createdAt,
		LastRequestId: requestId,
		LastLogId:     logId,
		LastLogType:   logType,
		UpdatedAt:     now,
	}
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "name"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"last_created_at": createdAt,
			"last_request_id": requestId,
			"last_log_id":     logId,
			"last_log_type":   logType,
			"updated_at":      now,
		}),
	}).Create(cursor).Error
}

// ListProxyLogsForAnalysis reads the generic log store without assuming that
// its database is the same as the main database. The ordering works for both
// relational logs and ClickHouse, whose physical log id may be zero.
func ListProxyLogsForAnalysis(cursor *ProxyLogAnalysisCursor, maxCreatedAt int64, limit int) ([]*Log, error) {
	if cursor == nil {
		cursor = &ProxyLogAnalysisCursor{}
	}
	if limit <= 0 {
		limit = 500
	}
	var logs []*Log
	err := LOG_DB.Model(&Log{}).
		Where("proxy_id > 0").
		Where("type IN ?", []int{LogTypeConsume, LogTypeError}).
		Where("created_at <= ?", maxCreatedAt).
		Where("created_at > ? OR (created_at = ? AND (request_id > ? OR (request_id = ? AND (id > ? OR (id = ? AND type > ?)))))", cursor.LastCreatedAt, cursor.LastCreatedAt, cursor.LastRequestId, cursor.LastRequestId, cursor.LastLogId, cursor.LastLogId, cursor.LastLogType).
		Order("created_at asc, request_id asc, id asc, type asc").
		Limit(limit).
		Find(&logs).Error
	return logs, err
}

func HasPendingProxyLogsForAnalysis(maxCreatedAt int64) bool {
	cursor, err := GetProxyLogAnalysisCursor()
	if err != nil {
		return false
	}
	logs, err := ListProxyLogsForAnalysis(cursor, maxCreatedAt, 1)
	return err == nil && len(logs) > 0
}

// ApplyProxyLogAnalysis inserts one decision exactly once and recalculates the
// current consecutive/window metrics from durable analysis rows.
func ApplyProxyLogAnalysis(analysis *ProxyLogAnalysis) (ProxyLogAnalysisApplyResult, error) {
	result := ProxyLogAnalysisApplyResult{}
	if analysis == nil || analysis.AnalysisKey == "" || analysis.ProxyId <= 0 {
		return result, errors.New("invalid proxy log analysis")
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		createResult := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(analysis)
		if createResult.Error != nil {
			return createResult.Error
		}
		if createResult.RowsAffected == 0 {
			return nil
		}
		result.Inserted = true

		var proxy Proxy
		if err := tx.First(&proxy, analysis.ProxyId).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		var group ProxyGroup
		if err := tx.First(&group, proxy.GroupId).Error; err != nil {
			return err
		}
		result.ProxyId = proxy.Id
		result.ProxyGroupId = group.Id
		result.SwitchWaitSeconds = group.SwitchWaitSeconds

		updates := map[string]interface{}{
			"total_requests":   gorm.Expr("total_requests + 1"),
			"last_analyzed_at": analysis.LogCreatedAt,
			"last_frt_ms":      analysis.FirstResponseTimeMs,
			"last_tps":         analysis.TokensPerSecond,
			"updated_at":       common.GetTimestamp(),
		}
		if analysis.IsTimeout {
			updates["total_timeouts"] = gorm.Expr("total_timeouts + 1")
			updates["last_timeout_reason"] = analysis.Reason
		}
		if !analysis.Counted {
			return tx.Model(&Proxy{}).Where("id = ?", proxy.Id).UpdateColumns(updates).Error
		}

		windowSize := group.WindowSize
		if windowSize <= 0 {
			windowSize = DefaultProxyWindowSize
		}
		var latest []*ProxyLogAnalysis
		analysisQuery := tx.Where("proxy_id = ? AND counted = ?", proxy.Id, true)
		if proxy.HealthEpochAt > 0 {
			analysisQuery = analysisQuery.Where("log_created_at >= ?", proxy.HealthEpochAt)
		}
		if err := analysisQuery.
			Order("log_created_at desc, id desc").
			Limit(windowSize).
			Find(&latest).Error; err != nil {
			return err
		}
		consecutive := 0
		windowTimeouts := 0
		for index, item := range latest {
			if item.IsTimeout {
				windowTimeouts++
				if index == consecutive {
					consecutive++
				}
			}
		}
		windowRatio := 0.0
		if len(latest) > 0 {
			windowRatio = float64(windowTimeouts) / float64(len(latest))
		}
		updates["consecutive_timeouts"] = consecutive
		updates["window_samples"] = len(latest)
		updates["window_timeouts"] = windowTimeouts
		updates["window_timeout_ratio"] = windowRatio

		consecutiveThreshold := group.ConsecutiveTimeoutThreshold
		if consecutiveThreshold <= 0 {
			consecutiveThreshold = DefaultProxyConsecutiveTimeouts
		}
		windowThreshold := group.WindowTimeoutRatio
		if windowThreshold <= 0 {
			windowThreshold = DefaultProxyWindowTimeoutRatio
		}
		shouldPause := consecutive >= consecutiveThreshold ||
			(len(latest) >= windowSize && windowRatio+1e-9 >= windowThreshold)

		fromStatus := proxy.Status
		if proxy.Enabled && fromStatus == ProxyStatusRecovering {
			if analysis.IsTimeout {
				recoveryFailures := proxy.RecoveryFailures + 1
				updates["status"] = ProxyStatusCooling
				updates["recovery_failures"] = recoveryFailures
				updates["recovery_successes"] = 0
				updates["recovery_probe_remaining"] = 0
				updates["cooldown_until"] = common.GetTimestamp() + int64(proxyCooldownSeconds(&group, recoveryFailures))
				result.Paused = true
			} else {
				required := group.RecoverySuccessCount
				if required <= 0 {
					required = DefaultProxyRecoverySuccessCount
				}
				successes := proxy.RecoverySuccesses + 1
				updates["recovery_successes"] = successes
				if successes >= required {
					updates["status"] = ProxyStatusAvailable
					updates["recovery_failures"] = 0
					updates["recovery_successes"] = 0
					updates["recovery_probe_remaining"] = 0
					updates["cooldown_until"] = 0
				}
			}
		} else if proxy.Enabled && (fromStatus == "" || fromStatus == ProxyStatusAvailable || fromStatus == ProxyStatusWatching) {
			if shouldPause {
				updates["status"] = ProxyStatusCooling
				cooldownSeconds := group.BaseCooldownSeconds
				if cooldownSeconds <= 0 {
					cooldownSeconds = DefaultProxyBaseCooldownSeconds
				}
				updates["cooldown_until"] = common.GetTimestamp() + int64(cooldownSeconds)
				result.Paused = true
			} else if consecutive > 0 {
				updates["status"] = ProxyStatusWatching
			} else {
				updates["status"] = ProxyStatusAvailable
			}
		}
		if err := tx.Model(&Proxy{}).Where("id = ?", proxy.Id).UpdateColumns(updates).Error; err != nil {
			return err
		}

		toStatus, _ := updates["status"].(string)
		if toStatus != "" && toStatus != fromStatus {
			eventType := "health_status_changed"
			if fromStatus == ProxyStatusRecovering && toStatus == ProxyStatusAvailable {
				eventType = "recovery_succeeded"
			} else if fromStatus == ProxyStatusRecovering && toStatus == ProxyStatusCooling {
				eventType = "recovery_failed"
			} else if result.Paused {
				eventType = "auto_paused"
			}
			if err := tx.Create(&ProxyStateEvent{
				ProxyId: proxy.Id, ProxyGroupId: group.Id, AnalysisId: analysis.Id,
				EventType: eventType, FromStatus: fromStatus, ToStatus: toStatus, Reason: analysis.Reason,
			}).Error; err != nil {
				return err
			}
		}
		if !result.Paused || group.CurrentProxyId != proxy.Id {
			return nil
		}
		result.SwitchRequired = true
		return tx.Model(&ProxyGroup{}).Where("id = ?", group.Id).UpdateColumns(map[string]interface{}{
			"status":     ProxyGroupStatusSwitching,
			"updated_at": common.GetTimestamp(),
		}).Error
	})
	return result, err
}

func CompleteProxyGroupSwitch(groupId int, failedProxyId int) (int, error) {
	nextProxyId := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		var failed Proxy
		if err := tx.First(&failed, failedProxyId).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return tx.Model(&ProxyGroup{}).Where("id = ?", groupId).UpdateColumns(map[string]interface{}{
					"current_proxy_id": 0,
					"status":           ProxyGroupStatusAvailable,
					"updated_at":       common.GetTimestamp(),
				}).Error
			}
			return err
		}
		var err error
		nextProxyId, err = selectNextAvailableProxyId(tx, &failed)
		if err != nil {
			return err
		}
		return tx.Model(&ProxyGroup{}).Where("id = ?", groupId).UpdateColumns(map[string]interface{}{
			"current_proxy_id": nextProxyId,
			"status":           ProxyGroupStatusAvailable,
			"updated_at":       common.GetTimestamp(),
		}).Error
	})
	return nextProxyId, err
}

func AbortProxyGroupSwitch(groupId int) error {
	return DB.Model(&ProxyGroup{}).Where("id = ?", groupId).UpdateColumns(map[string]interface{}{
		"status":     ProxyGroupStatusAvailable,
		"updated_at": common.GetTimestamp(),
	}).Error
}

func selectNextAvailableProxyId(tx *gorm.DB, current *Proxy) (int, error) {
	var candidates []*Proxy
	if err := tx.Where("group_id = ? AND id <> ? AND enabled = ?", current.GroupId, current.Id, true).
		Where("status = '' OR status IN ?", []string{ProxyStatusAvailable, ProxyStatusWatching}).
		Order("sort asc, id asc").
		Find(&candidates).Error; err != nil {
		return 0, err
	}
	if len(candidates) == 0 {
		return 0, nil
	}
	for _, candidate := range candidates {
		if candidate.Sort > current.Sort || (candidate.Sort == current.Sort && candidate.Id > current.Id) {
			return candidate.Id, nil
		}
	}
	return candidates[0].Id, nil
}

func NormalizedProxyTimeoutReason(parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			filtered = append(filtered, value)
		}
	}
	return strings.Join(filtered, ",")
}

func RoundProxyMetric(value float64) float64 {
	return math.Round(value*1000) / 1000
}
