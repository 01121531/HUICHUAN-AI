package service

import (
	"math"

	"github.com/01121531/HUICHUAN-AI/model"
)

type ProxyTrendPoint struct {
	Timestamp           int64   `json:"timestamp"`
	ProxyId             int     `json:"proxy_id"`
	Score               int     `json:"score"`
	KeyLatencyMs        int     `json:"key_latency_ms"`
	FirstResponseTimeMs int     `json:"first_response_time_ms"`
	TotalDurationMs     int     `json:"total_duration_ms"`
	TokensPerSecond     float64 `json:"tokens_per_second"`
	IsTimeout           bool    `json:"is_timeout"`
	Reason              string  `json:"reason"`
}

type ProxyTrendResponse struct {
	GroupId      int               `json:"group_id"`
	ProxyId      int               `json:"proxy_id"`
	Limit        int               `json:"limit"`
	SampleCount  int               `json:"sample_count"`
	CurrentScore int               `json:"current_score"`
	AverageScore float64           `json:"average_score"`
	TimeoutCount int               `json:"timeout_count"`
	Points       []ProxyTrendPoint `json:"points"`
}

func GetProxyTrend(groupId int, proxyId int, limit int) (ProxyTrendResponse, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	response := ProxyTrendResponse{
		GroupId: groupId,
		ProxyId: proxyId,
		Limit:   limit,
		Points:  make([]ProxyTrendPoint, 0),
	}
	analyses, err := model.ListProxyTrendAnalyses(groupId, proxyId, limit)
	if err != nil {
		return response, err
	}
	totalScore := 0
	for _, analysis := range analyses {
		score := CalculateProxyRequestHealthScore(analysis)
		keyLatencyMs := analysis.UseTimeSeconds * 1000
		if analysis.IsStream && analysis.FirstResponseTimeMs > 0 {
			keyLatencyMs = analysis.FirstResponseTimeMs
		}
		response.Points = append(response.Points, ProxyTrendPoint{
			Timestamp:           analysis.LogCreatedAt,
			ProxyId:             analysis.ProxyId,
			Score:               score,
			KeyLatencyMs:        keyLatencyMs,
			FirstResponseTimeMs: analysis.FirstResponseTimeMs,
			TotalDurationMs:     analysis.UseTimeSeconds * 1000,
			TokensPerSecond:     analysis.TokensPerSecond,
			IsTimeout:           analysis.IsTimeout,
			Reason:              analysis.Reason,
		})
		totalScore += score
		if analysis.IsTimeout {
			response.TimeoutCount++
		}
	}
	response.SampleCount = len(response.Points)
	if response.SampleCount > 0 {
		response.CurrentScore = response.Points[response.SampleCount-1].Score
		response.AverageScore = math.Round(float64(totalScore)/float64(response.SampleCount)*10) / 10
	}
	return response, nil
}

func CalculateProxyRequestHealthScore(analysis *model.ProxyLogAnalysis) int {
	if analysis == nil {
		return 0
	}
	score := 100.0
	if analysis.IsStream && analysis.FirstResponseTimeMs > 0 {
		score = math.Min(score, clampProxyTrendScore(100-40*float64(analysis.FirstResponseTimeMs)/ProxyFirstResponseTimeoutMilliseconds))
	}
	if analysis.CompletionTokens < ProxySmallOutputTokenThreshold {
		score = math.Min(score, clampProxyTrendScore(100-40*float64(analysis.UseTimeSeconds)/ProxySmallOutputTimeoutSeconds))
	} else if analysis.TokensPerSecond > 0 {
		throughputScore := 0.0
		if analysis.TokensPerSecond >= ProxyMinimumHealthyTokensPerSecond {
			throughputScore = 60 + 40*(analysis.TokensPerSecond-ProxyMinimumHealthyTokensPerSecond)/ProxyMinimumHealthyTokensPerSecond
		} else {
			throughputScore = math.Min(59, 60*analysis.TokensPerSecond/ProxyMinimumHealthyTokensPerSecond)
		}
		score = math.Min(score, clampProxyTrendScore(throughputScore))
	}
	if analysis.IsTimeout {
		score = math.Min(score, 59)
	}
	return int(math.Round(clampProxyTrendScore(score)))
}

func clampProxyTrendScore(score float64) float64 {
	return math.Max(0, math.Min(100, score))
}
