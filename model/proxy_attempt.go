package model

import (
	"strings"

	"github.com/01121531/HUICHUAN-AI/common"
	"gorm.io/gorm"
)

const (
	ProxyAttemptResultSuccess         = "success"
	ProxyAttemptResultHTTPError       = "http_error"
	ProxyAttemptResultNetworkError    = "network_error"
	ProxyAttemptResultInvalidResponse = "invalid_response"
)

// ProxyUpstreamAttempt records one actual client.Do call. It is deliberately
// stored in the main database so it remains available even when generic logs
// are written to ClickHouse.
type ProxyUpstreamAttempt struct {
	Id                int64  `json:"id" gorm:"primaryKey"`
	RequestId         string `json:"request_id" gorm:"type:varchar(128);not null;index:idx_proxy_attempt_request_sequence,priority:1;index"`
	AttemptSequence   int    `json:"attempt_sequence" gorm:"not null;index:idx_proxy_attempt_request_sequence,priority:2"`
	RetryIndex        int    `json:"retry_index" gorm:"default:0;index"`
	ChannelId         int    `json:"channel_id" gorm:"default:0;index"`
	ProxyId           int    `json:"proxy_id" gorm:"default:0;index"`
	ProxyGroupId      int    `json:"proxy_group_id" gorm:"default:0;index"`
	ProxyIndex        int    `json:"proxy_index" gorm:"default:0"`
	StartedAtMs       int64  `json:"started_at_ms" gorm:"bigint;index"`
	DurationMs        int    `json:"duration_ms" gorm:"default:0"`
	HttpStatus        int    `json:"http_status" gorm:"default:0"`
	Result            string `json:"result" gorm:"type:varchar(32);not null;index"`
	FailureReason     string `json:"failure_reason" gorm:"type:varchar(64);default:''"`
	UpstreamRequestId string `json:"upstream_request_id" gorm:"type:varchar(128);default:''"`
	IsStream          bool   `json:"is_stream" gorm:"default:false"`
	CreatedAt         int64  `json:"created_at" gorm:"bigint;index"`
}

func (ProxyUpstreamAttempt) TableName() string { return "proxy_upstream_attempts" }

func (attempt *ProxyUpstreamAttempt) BeforeCreate(_ *gorm.DB) error {
	if attempt.CreatedAt == 0 {
		attempt.CreatedAt = common.GetTimestamp()
	}
	attempt.RequestId = strings.TrimSpace(attempt.RequestId)
	attempt.FailureReason = strings.TrimSpace(attempt.FailureReason)
	attempt.UpstreamRequestId = strings.TrimSpace(attempt.UpstreamRequestId)
	attempt.RequestId = truncateProxyAttemptValue(attempt.RequestId, 128)
	attempt.FailureReason = truncateProxyAttemptValue(attempt.FailureReason, 64)
	attempt.UpstreamRequestId = truncateProxyAttemptValue(attempt.UpstreamRequestId, 128)
	return nil
}

func CreateProxyUpstreamAttempt(attempt *ProxyUpstreamAttempt) error {
	if attempt == nil {
		return nil
	}
	attempt.RequestId = strings.TrimSpace(attempt.RequestId)
	if attempt.RequestId == "" || attempt.AttemptSequence <= 0 {
		return nil
	}
	return DB.Create(attempt).Error
}

func ListProxyUpstreamAttempts(proxyId int, requestId string, limit int) ([]*ProxyUpstreamAttempt, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := DB.Model(&ProxyUpstreamAttempt{})
	if proxyId > 0 {
		query = query.Where("proxy_id = ?", proxyId)
	}
	if requestId = strings.TrimSpace(requestId); requestId != "" {
		query = query.Where("request_id = ?", requestId)
	}
	var attempts []*ProxyUpstreamAttempt
	err := query.Order("created_at desc, request_id desc, attempt_sequence desc, id desc").Limit(limit).Find(&attempts).Error
	return attempts, err
}

func truncateProxyAttemptValue(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes])
}
