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
	NERVMemoryKernelKey         = "nerv_memory.kernel"
)

const (
	nervEventInjectChat      = "inject_chat"
	nervEventInjectResponses = "inject_responses"
	nervEventTamperText      = "tamper_text"
	nervEventTamperChat      = "tamper_chat"
	nervEventTamperResponses = "tamper_responses"
	nervStatsPersistInterval = 5 * time.Second
	nervRecentLimit          = 12
	nervMemorySuccessLimit   = 50
	nervMemoryPreviewLimit   = 240
)

type nervRecentEvent struct {
	TS     int64  `json:"ts"`
	Event  string `json:"event"`
	Target string `json:"target"`
	Model  string `json:"model"`
}

type NERVMemoryKernel struct {
	Successes  []NERVMemorySuccess `json:"successes"`
	Patterns   map[string]int      `json:"patterns"`
	Techniques map[string]int      `json:"techniques"`
	Stats      map[string]int      `json:"stats"`
}

type NERVMemorySuccess struct {
	TS        string `json:"ts"`
	Category  string `json:"category"`
	User      string `json:"user"`
	Result    string `json:"result"`
	Technique string `json:"technique"`
	Hash      string `json:"hash"`
	Target    string `json:"target"`
	Model     string `json:"model"`
	Event     string `json:"event"`
}

var nervStatsPersistState struct {
	sync.Mutex
	last time.Time
}

func recordNERVEvent(event string, target NERVTarget, modelName string) {
	recordNERVEventWithLearning(event, target, modelName, "", "", "")
}

func recordNERVEventWithLearning(event string, target NERVTarget, modelName string, userPreview string, resultPreview string, technique string) {
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
	updates[NERVMemoryKernelKey] = appendNERVMemoryLocked(now, event, target, modelName, userPreview, resultPreview, technique)
	updates[NERVProxyRecentKey] = appendNERVProxyEventLocked(now, event, target, modelName, userPreview, resultPreview, technique)
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

func appendNERVMemoryLocked(now time.Time, event string, target NERVTarget, modelName string, userPreview string, resultPreview string, technique string) string {
	kernel := loadNERVMemoryKernelLocked()
	category := classifyNERVMemoryCategory(event, userPreview, resultPreview)
	userPreview = trimNERVMemoryPreview(userPreview)
	resultPreview = trimNERVMemoryPreview(resultPreview)
	technique = strings.TrimSpace(technique)
	if technique == "" {
		technique = techniqueFromNERVEvent(event)
	}

	kernel.Stats["total"]++
	kernel.Stats[category]++
	for _, token := range tokenizeNERVMemoryText(userPreview) {
		kernel.Patterns[token]++
	}
	if technique != "" {
		kernel.Techniques[technique]++
	}
	kernel.Successes = append(kernel.Successes, NERVMemorySuccess{
		TS:        now.Format(time.RFC3339),
		Category:  category,
		User:      userPreview,
		Result:    resultPreview,
		Technique: technique,
		Hash:      shortNERVMemoryHash(userPreview + "|" + event + "|" + modelName),
		Target:    string(target),
		Model:     modelName,
		Event:     event,
	})
	if len(kernel.Successes) > nervMemorySuccessLimit {
		kernel.Successes = kernel.Successes[len(kernel.Successes)-nervMemorySuccessLimit:]
	}
	data, err := json.Marshal(kernel)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func loadNERVMemoryKernelLocked() NERVMemoryKernel {
	kernel := defaultNERVMemoryKernel()
	raw := strings.TrimSpace(common.OptionMap[NERVMemoryKernelKey])
	if raw == "" {
		return kernel
	}
	_ = json.Unmarshal([]byte(raw), &kernel)
	if kernel.Patterns == nil {
		kernel.Patterns = map[string]int{}
	}
	if kernel.Techniques == nil {
		kernel.Techniques = map[string]int{}
	}
	if kernel.Stats == nil {
		kernel.Stats = map[string]int{}
	}
	return kernel
}

func NERVMemorySnapshot() NERVMemoryKernel {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return loadNERVMemoryKernelLocked()
}

func defaultNERVMemoryKernel() NERVMemoryKernel {
	return NERVMemoryKernel{
		Successes:  []NERVMemorySuccess{},
		Patterns:   map[string]int{},
		Techniques: map[string]int{},
		Stats:      map[string]int{"total": 0, "crack": 0, "reverse": 0, "pentest": 0, "tamper": 0, "general": 0},
	}
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

func recordNERVStreamTamperEvent(target NERVTarget, modelName string, originalText string, replacement string) {
	switch target {
	case NERVTargetOpenAIResponses, NERVTargetCodexResponses:
		recordNERVEventWithLearning(nervEventTamperResponses, target, modelName, originalText, replacement, "stream_tamper")
	default:
		recordNERVEventWithLearning(nervEventTamperChat, target, modelName, originalText, replacement, "stream_tamper")
	}
}

func classifyNERVMemoryCategory(event string, userPreview string, resultPreview string) string {
	if strings.Contains(event, "tamper") {
		return "tamper"
	}
	text := strings.ToLower(userPreview + " " + resultPreview)
	switch {
	case containsAnyNERVMemoryText(text, "crack", "keygen", "vip", "bypass", "license", "破解", "激活", "会员"):
		return "crack"
	case containsAnyNERVMemoryText(text, "reverse", "reversing", "disassemble", "decompile", "unpack", "binary", "逆向", "反编译", "脱壳"):
		return "reverse"
	case containsAnyNERVMemoryText(text, "pentest", "sqli", "xss", "csrf", "ssrf", "rce", "cve", "scan", "渗透", "漏洞", "扫描"):
		return "pentest"
	default:
		return "general"
	}
}

func containsAnyNERVMemoryText(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func techniqueFromNERVEvent(event string) string {
	switch event {
	case nervEventInjectChat:
		return "chat_bridge"
	case nervEventInjectResponses:
		return "responses_bridge"
	case nervEventTamperText, nervEventTamperChat, nervEventTamperResponses:
		return "tamper"
	default:
		return event
	}
}

func tokenizeNERVMemoryText(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r == '_' || r == '-' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r > 127)
	})
	tokens := make([]string, 0, len(fields))
	seen := map[string]bool{}
	for _, field := range fields {
		field = strings.Trim(field, "`'\".。；;，,：:()（）[]【】")
		if len([]rune(field)) < 2 || seen[field] {
			continue
		}
		seen[field] = true
		tokens = append(tokens, field)
	}
	return tokens
}

func trimNERVMemoryPreview(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\x00", ""))
	runes := []rune(value)
	if len(runes) <= nervMemoryPreviewLimit {
		return value
	}
	return string(runes[:nervMemoryPreviewLimit]) + "..."
}

func shortNERVMemoryHash(value string) string {
	var hash uint32 = 2166136261
	for _, b := range []byte(value) {
		hash ^= uint32(b)
		hash *= 16777619
	}
	return strconv.FormatUint(uint64(hash), 16)
}
