package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/model"
)

const (
	ProxyFirstResponseTimeoutMilliseconds = 10_000
	ProxySmallOutputTokenThreshold        = 100
	ProxySmallOutputTimeoutSeconds        = 30
	ProxyMinimumHealthyTokensPerSecond    = 15
	proxyLogAnalysisBatchSize             = 500
	proxyLogAnalysisMaxBatches            = 10
	proxyLogAnalysisSafetyLagSeconds      = 10
)

type ProxyLogTimingEvaluation struct {
	Counted              bool    `json:"counted"`
	Timeout              bool    `json:"timeout"`
	FirstResponseTimeout bool    `json:"first_response_timeout"`
	DurationTimeout      bool    `json:"duration_timeout"`
	ThroughputTimeout    bool    `json:"throughput_timeout"`
	FirstResponseTimeMs  int     `json:"first_response_time_ms"`
	TokensPerSecond      float64 `json:"tokens_per_second"`
	Reason               string  `json:"reason"`
}

type ProxyLogAnalysisSummary struct {
	Scanned    int   `json:"scanned"`
	Inserted   int   `json:"inserted"`
	Duplicates int   `json:"duplicates"`
	Timeouts   int   `json:"timeouts"`
	Paused     int   `json:"paused"`
	Switched   int   `json:"switched"`
	CursorAt   int64 `json:"cursor_at"`
}

// EvaluateProxyLogTiming exactly mirrors the red timing rules used by the
// generic log UI. Non-red error logs are recorded for dedup/audit but do not
// reset health streaks because business errors are not proxy health evidence.
func EvaluateProxyLogTiming(log *model.Log) ProxyLogTimingEvaluation {
	if log == nil {
		return ProxyLogTimingEvaluation{}
	}
	frtMilliseconds := proxyLogFirstResponseMilliseconds(log.Other)
	firstResponseTimeout := log.IsStream && frtMilliseconds >= ProxyFirstResponseTimeoutMilliseconds
	durationTimeout := log.CompletionTokens < ProxySmallOutputTokenThreshold && log.UseTime >= ProxySmallOutputTimeoutSeconds
	throughput := 0.0
	throughputTimeout := false
	if log.CompletionTokens >= ProxySmallOutputTokenThreshold && log.UseTime > 0 {
		throughput = float64(log.CompletionTokens) / float64(log.UseTime)
		throughputTimeout = throughput < ProxyMinimumHealthyTokensPerSecond
	}
	timedOut := firstResponseTimeout || durationTimeout || throughputTimeout
	reasons := make([]string, 0, 3)
	if firstResponseTimeout {
		reasons = append(reasons, "first_response")
	}
	if durationTimeout {
		reasons = append(reasons, "duration")
	}
	if throughputTimeout {
		reasons = append(reasons, "throughput")
	}
	return ProxyLogTimingEvaluation{
		Counted:              log.Type == model.LogTypeConsume || timedOut,
		Timeout:              timedOut,
		FirstResponseTimeout: firstResponseTimeout,
		DurationTimeout:      durationTimeout,
		ThroughputTimeout:    throughputTimeout,
		FirstResponseTimeMs:  frtMilliseconds,
		TokensPerSecond:      model.RoundProxyMetric(throughput),
		Reason:               model.NormalizedProxyTimeoutReason(reasons...),
	}
}

func proxyLogFirstResponseMilliseconds(other string) int {
	if strings.TrimSpace(other) == "" {
		return 0
	}
	var values map[string]interface{}
	decoder := json.NewDecoder(strings.NewReader(other))
	decoder.UseNumber()
	if err := decoder.Decode(&values); err != nil {
		return 0
	}
	value, exists := values["frt"]
	if !exists {
		return 0
	}
	switch typed := value.(type) {
	case json.Number:
		parsed, err := strconv.ParseFloat(string(typed), 64)
		if err == nil && parsed > 0 {
			return int(parsed)
		}
	case float64:
		if typed > 0 {
			return int(typed)
		}
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err == nil && parsed > 0 {
			return int(parsed)
		}
	}
	return 0
}

func proxyLogAnalysisKey(log *model.Log) string {
	requestKey := strings.TrimSpace(log.RequestId)
	if requestKey == "" {
		requestKey = fmt.Sprintf("fallback:%d:%d:%d:%d:%d", log.Id, log.CreatedAt, log.ChannelId, log.UseTime, log.CompletionTokens)
	}
	source := fmt.Sprintf("%d:%s:%d", log.ProxyId, requestKey, log.Type)
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}

func proxyLogAnalysisFromLog(log *model.Log) *model.ProxyLogAnalysis {
	evaluation := EvaluateProxyLogTiming(log)
	return &model.ProxyLogAnalysis{
		AnalysisKey:          proxyLogAnalysisKey(log),
		LogId:                log.Id,
		RequestId:            log.RequestId,
		LogType:              log.Type,
		LogCreatedAt:         log.CreatedAt,
		ProxyId:              log.ProxyId,
		ProxyGroupId:         log.ProxyGroupId,
		ChannelId:            log.ChannelId,
		IsStream:             log.IsStream,
		UseTimeSeconds:       log.UseTime,
		CompletionTokens:     log.CompletionTokens,
		FirstResponseTimeMs:  evaluation.FirstResponseTimeMs,
		TokensPerSecond:      evaluation.TokensPerSecond,
		Counted:              evaluation.Counted,
		IsTimeout:            evaluation.Timeout,
		FirstResponseTimeout: evaluation.FirstResponseTimeout,
		DurationTimeout:      evaluation.DurationTimeout,
		ThroughputTimeout:    evaluation.ThroughputTimeout,
		Reason:               evaluation.Reason,
	}
}

// RunProxyLogAnalysisTask incrementally processes generic logs using a durable
// cursor and idempotency keys. The system-task lease prevents duplicate work
// across master nodes; unique analysis_key remains the final safety net.
func RunProxyLogAnalysisTask(ctx context.Context) (ProxyLogAnalysisSummary, error) {
	summary := ProxyLogAnalysisSummary{}
	cursor, err := model.GetProxyLogAnalysisCursor()
	if err != nil {
		return summary, err
	}
	maxCreatedAt := common.GetTimestamp() - proxyLogAnalysisSafetyLagSeconds
	for batch := 0; batch < proxyLogAnalysisMaxBatches; batch++ {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		logs, err := model.ListProxyLogsForAnalysis(cursor, maxCreatedAt, proxyLogAnalysisBatchSize)
		if err != nil {
			return summary, err
		}
		if len(logs) == 0 {
			break
		}
		for _, log := range logs {
			if err := ctx.Err(); err != nil {
				return summary, err
			}
			summary.Scanned++
			analysis := proxyLogAnalysisFromLog(log)
			applyResult, err := model.ApplyProxyLogAnalysis(analysis)
			if err != nil {
				return summary, err
			}
			if applyResult.Inserted {
				summary.Inserted++
				if analysis.IsTimeout {
					summary.Timeouts++
				}
				if applyResult.Paused {
					summary.Paused++
				}
				if applyResult.SwitchedToProxyId > 0 {
					summary.Switched++
				}
			} else {
				summary.Duplicates++
			}
			cursor.LastCreatedAt = log.CreatedAt
			cursor.LastRequestId = log.RequestId
			cursor.LastLogId = log.Id
			cursor.LastLogType = log.Type
			summary.CursorAt = cursor.LastCreatedAt
			if applyResult.Paused {
				InvalidateChannelProxyConfig(0)
			}
		}
		if err := model.SaveProxyLogAnalysisCursor(cursor.LastCreatedAt, cursor.LastRequestId, cursor.LastLogId, cursor.LastLogType); err != nil {
			return summary, err
		}
		if len(logs) < proxyLogAnalysisBatchSize {
			break
		}
	}
	return summary, nil
}

func ProxyLogAnalysisInterval() time.Duration { return 15 * time.Second }
