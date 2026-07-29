package service

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/model"
)

const (
	NERVProxyRecentKey = "nerv_proxy.recent"
	nervProxyLogLimit  = 100
)

type NERVProxyEvent struct {
	TS            int64  `json:"ts"`
	RequestID     string `json:"request_id"`
	Event         string `json:"event"`
	Target        string `json:"target"`
	Model         string `json:"model"`
	Path          string `json:"path"`
	Method        string `json:"method"`
	StatusCode    int    `json:"status_code"`
	Injected      bool   `json:"injected"`
	Tampered      bool   `json:"tampered"`
	Stream        bool   `json:"stream"`
	RequestBytes  int    `json:"request_bytes"`
	ResponseBytes int    `json:"response_bytes"`
	UserPreview   string `json:"user_preview"`
	ReplyPreview  string `json:"reply_preview"`
	Technique     string `json:"technique"`
}

type NERVProxyStats struct {
	Total           int `json:"total"`
	Inject          int `json:"inject"`
	Tamper          int `json:"tamper"`
	Stream          int `json:"stream"`
	ChatInject      int `json:"chat_inject"`
	ResponsesInject int `json:"responses_inject"`
	ChatTamper      int `json:"chat_tamper"`
	ResponsesTamper int `json:"responses_tamper"`
}

func appendNERVProxyEventLocked(now time.Time, event string, target NERVTarget, modelName string, userPreview string, replyPreview string, technique string) string {
	events := loadNERVProxyEventsLocked()
	entry := NERVProxyEvent{
		TS:            now.Unix(),
		RequestID:     shortNERVMemoryHash(userPreview + "|" + replyPreview + "|" + event + "|" + modelName),
		Event:         event,
		Target:        string(target),
		Model:         modelName,
		Path:          pathFromNERVTarget(target),
		Method:        "POST",
		StatusCode:    0,
		Injected:      strings.HasPrefix(event, "inject_"),
		Tampered:      strings.HasPrefix(event, "tamper_"),
		Stream:        strings.EqualFold(technique, "stream_tamper"),
		RequestBytes:  len([]byte(userPreview)),
		ResponseBytes: len([]byte(replyPreview)),
		UserPreview:   trimNERVMemoryPreview(userPreview),
		ReplyPreview:  trimNERVMemoryPreview(replyPreview),
		Technique:     strings.TrimSpace(technique),
	}
	events = append([]NERVProxyEvent{entry}, events...)
	if len(events) > nervProxyLogLimit {
		events = events[:nervProxyLogLimit]
	}
	data, err := json.Marshal(events)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func loadNERVProxyEventsLocked() []NERVProxyEvent {
	events := make([]NERVProxyEvent, 0, nervProxyLogLimit)
	raw := strings.TrimSpace(common.OptionMap[NERVProxyRecentKey])
	if raw == "" {
		return events
	}
	_ = json.Unmarshal([]byte(raw), &events)
	return events
}

func NERVProxySnapshot(limit int, target string, tampered *bool) ([]NERVProxyEvent, NERVProxyStats) {
	common.OptionMapRWMutex.RLock()
	events := loadNERVProxyEventsLocked()
	common.OptionMapRWMutex.RUnlock()

	filtered := make([]NERVProxyEvent, 0, len(events))
	target = strings.TrimSpace(target)
	for _, event := range events {
		if target != "" && event.Target != target {
			continue
		}
		if tampered != nil && event.Tampered != *tampered {
			continue
		}
		filtered = append(filtered, event)
	}
	stats := buildNERVProxyStats(filtered)
	if limit <= 0 || limit > nervProxyLogLimit {
		limit = nervProxyLogLimit
	}
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, stats
}

func ClearNERVProxyLogs() error {
	common.OptionMapRWMutex.Lock()
	common.OptionMap[NERVProxyRecentKey] = "[]"
	common.OptionMapRWMutex.Unlock()
	if model.DB != nil {
		return model.UpdateOptionsBulk(map[string]string{NERVProxyRecentKey: "[]"})
	}
	return nil
}

func buildNERVProxyStats(events []NERVProxyEvent) NERVProxyStats {
	stats := NERVProxyStats{}
	for _, event := range events {
		stats.Total++
		if event.Injected {
			stats.Inject++
			if isResponsesNERVTarget(event.Target) {
				stats.ResponsesInject++
			} else {
				stats.ChatInject++
			}
		}
		if event.Tampered {
			stats.Tamper++
			if isResponsesNERVTarget(event.Target) {
				stats.ResponsesTamper++
			} else {
				stats.ChatTamper++
			}
		}
		if event.Stream {
			stats.Stream++
		}
	}
	return stats
}

func isResponsesNERVTarget(target string) bool {
	switch NERVTarget(target) {
	case NERVTargetOpenAIResponses, NERVTargetCodexResponses:
		return true
	default:
		return false
	}
}

func pathFromNERVTarget(target NERVTarget) string {
	switch target {
	case NERVTargetCodexResponses, NERVTargetOpenAIResponses:
		return "/v1/responses"
	case NERVTargetOpenAIChat, NERVTargetClaudeToOpenAI, NERVTargetGeminiToOpenAI:
		return "/v1/chat/completions"
	default:
		return ""
	}
}
