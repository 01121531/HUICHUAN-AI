package service

import (
	"strings"
	"sync"
	"time"
)

// 渠道可变代理轮换参数（轻量版默认值，后续可改为系统设置项）。
var (
	// ChannelProxyRotateRequests 单个代理在自动轮换前可承载的请求数。
	ChannelProxyRotateRequests = 500
	// ChannelProxyRotateSeconds 单个代理在自动轮换前可连续使用的秒数。
	ChannelProxyRotateSeconds = 1800
	// ChannelProxyFailCooldownSeconds 代理请求失败后进入冷却的秒数。
	ChannelProxyFailCooldownSeconds = 60
)

// channelProxyState 单个渠道的代理轮换运行状态。
type channelProxyState struct {
	mu           sync.Mutex
	raw          string
	proxies      []string
	currentIndex int
	roundCount   int
	roundStart   time.Time
	failedUntil  map[int]time.Time
}

// channelProxyRotator 按渠道维护多代理自动轮换状态。
type channelProxyRotator struct {
	mu     sync.Mutex
	states map[int]*channelProxyState
}

// ChannelProxyRotator 是全局渠道代理轮换器。
var ChannelProxyRotator = &channelProxyRotator{states: make(map[int]*channelProxyState)}

// parseProxyList 解析渠道代理设置：支持每行一个代理，也兼容逗号分隔，自动去重去空。
func parseProxyList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == ','
	})
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func (r *channelProxyRotator) state(channelID int) *channelProxyState {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.states[channelID]
	if st == nil {
		st = &channelProxyState{
			roundStart:  time.Now(),
			failedUntil: make(map[int]time.Time),
		}
		r.states[channelID] = st
	}
	return st
}

// SelectProxy 为渠道从代理设置中选择当前要使用的代理。
// 返回代理地址、1 基代理序号（用于日志关联）与错误；仅有一个代理时始终使用该代理。
func (r *channelProxyRotator) SelectProxy(channelID int, raw string) (string, int, error) {
	return r.SelectProxyWithPolicy(channelID, raw, ChannelProxyRotateRequests, ChannelProxyRotateSeconds)
}

func (r *channelProxyRotator) SelectProxyWithPolicy(stateKey int, raw string, maxRequests int, maxDurationSeconds int) (string, int, error) {
	return r.SelectProxyWithPolicyFromIndex(stateKey, raw, maxRequests, maxDurationSeconds, 0)
}

func (r *channelProxyRotator) SelectProxyWithPolicyFromIndex(stateKey int, raw string, maxRequests int, maxDurationSeconds int, startIndex int) (string, int, error) {
	proxies := parseProxyList(raw)
	if len(proxies) == 0 {
		return "", 0, nil
	}

	st := r.state(stateKey)
	st.mu.Lock()
	defer st.mu.Unlock()

	now := time.Now()
	// 代理列表发生变化时重置轮换状态。
	if st.raw != raw {
		st.raw = raw
		st.proxies = proxies
		if startIndex < 0 || startIndex >= len(proxies) {
			startIndex = 0
		}
		st.currentIndex = startIndex
		st.roundCount = 0
		st.roundStart = now
		st.failedUntil = make(map[int]time.Time)
	}

	// 达到请求次数或使用时长阈值时轮换到下一个代理。
	if (maxRequests > 0 && st.roundCount >= maxRequests) ||
		(maxDurationSeconds > 0 && now.Sub(st.roundStart) >= time.Duration(maxDurationSeconds)*time.Second) {
		st.currentIndex = (st.currentIndex + 1) % len(st.proxies)
		st.roundCount = 0
		st.roundStart = now
	}

	// 当前代理处于失败冷却时，跳到下一个可用代理。
	for i := 0; i < len(st.proxies); i++ {
		idx := (st.currentIndex + i) % len(st.proxies)
		if until, ok := st.failedUntil[idx]; !ok || now.After(until) {
			st.currentIndex = idx
			break
		}
	}

	// 所有代理都在冷却中：清空冷却，从当前索引继续，避免请求无限失败堆积。
	if until, ok := st.failedUntil[st.currentIndex]; ok && !now.After(until) {
		st.failedUntil = make(map[int]time.Time)
	}

	st.roundCount++
	return st.proxies[st.currentIndex], st.currentIndex + 1, nil
}

// MarkProxyFailed 标记代理请求失败并进入冷却，同时让下一次请求切换到下一个代理。
func (r *channelProxyRotator) MarkProxyFailed(channelID int, proxyIndex int) {
	r.MarkProxyFailedWithCooldown(channelID, proxyIndex, ChannelProxyFailCooldownSeconds)
}

func (r *channelProxyRotator) MarkProxyFailedWithCooldown(stateKey int, proxyIndex int, cooldownSeconds int) {
	if proxyIndex <= 0 {
		return
	}
	st := r.state(stateKey)
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.proxies) == 0 {
		return
	}
	idx := (proxyIndex - 1) % len(st.proxies)
	if cooldownSeconds <= 0 {
		cooldownSeconds = ChannelProxyFailCooldownSeconds
	}
	st.failedUntil[idx] = time.Now().Add(time.Duration(cooldownSeconds) * time.Second)
	// 当前代理失败时立即切换到下一个，重置轮换计数。
	if idx == st.currentIndex && len(st.proxies) > 1 {
		st.currentIndex = (st.currentIndex + 1) % len(st.proxies)
		st.roundCount = 0
		st.roundStart = time.Now()
	}
}
