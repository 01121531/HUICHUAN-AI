package service

import (
	"strconv"
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
)

const (
	nervEventInjectChat      = "inject_chat"
	nervEventInjectResponses = "inject_responses"
	nervEventTamperText      = "tamper_text"
	nervEventTamperChat      = "tamper_chat"
	nervEventTamperResponses = "tamper_responses"
	nervStatsPersistInterval = 5 * time.Second
)

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
	for key, value := range updates {
		common.OptionMap[key] = value
	}
	common.OptionMapRWMutex.Unlock()

	persistNERVStatsPeriodically(now, updates)
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
