package service

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/model"
)

const (
	NERVStatsTotalKey           = "nerv_stats.total"
	NERVStatsInjectKey          = "nerv_stats.inject"
	NERVStatsTamperKey          = "nerv_stats.tamper"
	NERVStatsChatInjectKey      = "nerv_stats.chat_inject"
	NERVStatsResponsesInjectKey = "nerv_stats.responses_inject"
	NERVStatsChatTamperKey      = "nerv_stats.chat_tamper"
	NERVStatsResponsesTamperKey = "nerv_stats.responses_tamper"
	NERVStatsLastEventAtKey     = "nerv_stats.last_event_at"
	NERVStatsLastEventKey       = "nerv_stats.last_event"
	NERVStatsLastTargetKey      = "nerv_stats.last_target"
	NERVStatsLastModelKey       = "nerv_stats.last_model"
	NERVStatsRecentKey          = "nerv_stats.recent"
)

const (
	nervEventInjectChat      = "inject_chat"
	nervEventInjectResponses = "inject_responses"
	nervEventTamperText      = "tamper_text"
	nervEventTamperChat      = "tamper_chat"
	nervEventTamperResponses = "tamper_responses"
	nervStatsPersistInterval = 5 * time.Second
	nervRecentLimit          = 12
)

type nervRecentEvent struct {
	TS     int64  `json:"ts"`
	Event  string `json:"event"`
	Target string `json:"target"`
	Model  string `json:"model"`
}

var nervStatsPersistState struct {
	sync.Mutex
	last time.Time
}

func recordNERVEvent(event string, target NERVTarget, modelName string) {
	now := time.Now()
	updates := map[string]string{}

	common.OptionMapRWMutex.Lock()
	incrementNERVStatLocked(NERVStatsTotalKey, updates)
	switch event {
	case nervEventInjectChat:
		incrementNERVStatLocked(NERVStatsInjectKey, updates)
		incrementNERVStatLocked(NERVStatsChatInjectKey, updates)
	case nervEventInjectResponses:
		incrementNERVStatLocked(NERVStatsInjectKey, updates)
		incrementNERVStatLocked(NERVStatsResponsesInjectKey, updates)
	case nervEventTamperText:
		incrementNERVStatLocked(NERVStatsTamperKey, updates)
	case nervEventTamperChat:
		incrementNERVStatLocked(NERVStatsTamperKey, updates)
		incrementNERVStatLocked(NERVStatsChatTamperKey, updates)
	case nervEventTamperResponses:
		incrementNERVStatLocked(NERVStatsTamperKey, updates)
		incrementNERVStatLocked(NERVStatsResponsesTamperKey, updates)
	}
	updates[NERVStatsLastEventAtKey] = strconv.FormatInt(now.Unix(), 10)
	updates[NERVStatsLastEventKey] = event
	updates[NERVStatsLastTargetKey] = string(target)
	updates[NERVStatsLastModelKey] = modelName
	updates[NERVStatsRecentKey] = appendNERVRecentEventLocked(now.Unix(), event, target, modelName)
	for key, value := range updates {
		common.OptionMap[key] = value
	}
	common.OptionMapRWMutex.Unlock()

	persistNERVStatsPeriodically(now, updates)
}

func appendNERVRecentEventLocked(ts int64, event string, target NERVTarget, modelName string) string {
	events := make([]nervRecentEvent, 0, nervRecentLimit)
	if raw, ok := common.OptionMap[NERVStatsRecentKey]; ok && strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &events)
	}
	events = append([]nervRecentEvent{{
		TS:     ts,
		Event:  event,
		Target: string(target),
		Model:  modelName,
	}}, events...)
	if len(events) > nervRecentLimit {
		events = events[:nervRecentLimit]
	}
	data, err := json.Marshal(events)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func incrementNERVStatLocked(key string, updates map[string]string) {
	current, _ := strconv.ParseInt(common.OptionMap[key], 10, 64)
	next := current + 1
	updates[key] = strconv.FormatInt(next, 10)
}

func persistNERVStatsPeriodically(now time.Time, updates map[string]string) {
	if model.DB == nil {
		return
	}

	nervStatsPersistState.Lock()
	if now.Sub(nervStatsPersistState.last) < nervStatsPersistInterval {
		nervStatsPersistState.Unlock()
		return
	}
	nervStatsPersistState.last = now
	nervStatsPersistState.Unlock()

	values := make(map[string]string, len(updates))
	for key, value := range updates {
		values[key] = value
	}
	go func() {
		_ = model.UpdateOptionsBulk(values)
	}()
}

func recordNERVStreamTamperEvent(target NERVTarget, modelName string) {
	switch target {
	case NERVTargetOpenAIResponses, NERVTargetCodexResponses:
		recordNERVEvent(nervEventTamperResponses, target, modelName)
	default:
		recordNERVEvent(nervEventTamperChat, target, modelName)
	}
}
