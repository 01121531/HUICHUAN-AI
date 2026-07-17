package datasetcapture

import (
	"fmt"
	"html"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type AlertConfig struct {
	Enabled         bool
	Recipients      []string
	Types           []string
	Silence         time.Duration
	AlertAfterDrops int64
	SendRecovery    bool
	Node            string
	Version         string
}

type AlertNotification struct {
	Subject    string
	Recipients []string
	HTML       string
}

type AlertStatus struct {
	EventQueueDepth int   `json:"event_queue_depth"`
	MailQueueDepth  int   `json:"mail_queue_depth"`
	EventsDropped   int64 `json:"events_dropped"`
	MailDropped     int64 `json:"mail_dropped"`
	SendFailed      int64 `json:"send_failed"`
	LastAlertAt     int64 `json:"last_alert_at"`
	LastRecoveryAt  int64 `json:"last_recovery_at"`
}

type alertState struct {
	active   bool
	firstAt  time.Time
	lastAt   time.Time
	lastSent time.Time
	count    int64
	dropped  int64
	bytes    int64
}

type AlertManager struct {
	sender         func(AlertNotification) error
	events         chan Event
	mail           chan AlertNotification
	done           chan struct{}
	stop           chan struct{}
	once           sync.Once
	mu             sync.RWMutex
	config         AlertConfig
	states         map[string]*alertState
	eventsDropped  atomic.Int64
	mailDropped    atomic.Int64
	sendFailed     atomic.Int64
	lastAlertAt    atomic.Int64
	lastRecoveryAt atomic.Int64
}

func NewAlertManager(sender func(AlertNotification) error) *AlertManager {
	manager := &AlertManager{
		sender: sender, events: make(chan Event, 256), mail: make(chan AlertNotification, 32),
		done: make(chan struct{}), stop: make(chan struct{}), states: make(map[string]*alertState),
	}
	go manager.runEvents()
	go manager.runMail()
	return manager
}

func (m *AlertManager) Update(config AlertConfig) {
	if m == nil {
		return
	}
	if config.Silence <= 0 {
		config.Silence = 10 * time.Minute
	}
	if config.AlertAfterDrops <= 0 {
		config.AlertAfterDrops = 1
	}
	config.Recipients = append([]string(nil), config.Recipients...)
	config.Types = append([]string(nil), config.Types...)
	m.mu.Lock()
	m.config = config
	m.mu.Unlock()
}

func (m *AlertManager) Notify(event Event) {
	if m == nil {
		return
	}
	if event.At.IsZero() {
		event.At = time.Now()
	}
	select {
	case m.events <- event:
	default:
		m.eventsDropped.Add(1)
	}
}

func (m *AlertManager) Resolve(eventType string) {
	m.Notify(Event{Type: eventType, At: time.Now(), Resolved: true})
}

func (m *AlertManager) SendTest() bool {
	if m == nil {
		return false
	}
	config := m.getConfig()
	if !config.Enabled || len(config.Recipients) == 0 {
		return false
	}
	notification := AlertNotification{
		Subject:    "[HUICHUAN] 数据快照告警测试",
		Recipients: append([]string(nil), config.Recipients...),
		HTML:       fmt.Sprintf("<h2>数据快照告警测试</h2><p>节点：%s</p><p>版本：%s</p>", html.EscapeString(config.Node), html.EscapeString(config.Version)),
	}
	return m.enqueueMail(notification)
}

func (m *AlertManager) Status() AlertStatus {
	if m == nil {
		return AlertStatus{}
	}
	return AlertStatus{
		EventQueueDepth: len(m.events), MailQueueDepth: len(m.mail),
		EventsDropped: m.eventsDropped.Load(), MailDropped: m.mailDropped.Load(),
		SendFailed: m.sendFailed.Load(), LastAlertAt: m.lastAlertAt.Load(), LastRecoveryAt: m.lastRecoveryAt.Load(),
	}
}

func (m *AlertManager) Close() {
	if m == nil {
		return
	}
	m.once.Do(func() { close(m.stop) })
	<-m.done
}

func (m *AlertManager) runEvents() {
	defer close(m.mail)
	for {
		select {
		case event := <-m.events:
			m.handleEvent(event)
		case <-m.stop:
			return
		}
	}
}

func (m *AlertManager) runMail() {
	defer close(m.done)
	for notification := range m.mail {
		if m.sender != nil {
			if err := m.sender(notification); err != nil {
				m.sendFailed.Add(1)
			}
		}
	}
}

func (m *AlertManager) handleEvent(event Event) {
	config := m.getConfig()
	if event.Resolved {
		m.handleRecovery(config, event)
		return
	}
	if !config.Enabled || len(config.Recipients) == 0 || !containsAlertType(config.Types, event.Type) {
		return
	}
	m.mu.Lock()
	state := m.states[event.Type]
	if state == nil {
		state = &alertState{}
		m.states[event.Type] = state
	}
	if !state.active {
		state.active = true
		state.firstAt = event.At
		state.count = 0
		state.dropped = 0
		state.bytes = 0
	}
	state.lastAt = event.At
	state.count++
	state.dropped += event.Dropped
	state.bytes += event.Bytes
	shouldSend := state.count >= config.AlertAfterDrops && (state.lastSent.IsZero() || event.At.Sub(state.lastSent) >= config.Silence)
	if shouldSend {
		state.lastSent = event.At
	}
	snapshot := *state
	m.mu.Unlock()
	if shouldSend && m.enqueueMail(buildAlertNotification(config, event.Type, snapshot, false)) {
		m.lastAlertAt.Store(event.At.Unix())
	}
}

func (m *AlertManager) handleRecovery(config AlertConfig, event Event) {
	m.mu.Lock()
	state := m.states[event.Type]
	if state == nil || !state.active {
		m.mu.Unlock()
		return
	}
	snapshot := *state
	state.active = false
	m.mu.Unlock()
	if config.Enabled && config.SendRecovery && !snapshot.lastSent.IsZero() && len(config.Recipients) > 0 {
		if m.enqueueMail(buildAlertNotification(config, event.Type, snapshot, true)) {
			m.lastRecoveryAt.Store(event.At.Unix())
		}
	}
}

func (m *AlertManager) enqueueMail(notification AlertNotification) bool {
	select {
	case m.mail <- notification:
		return true
	default:
		m.mailDropped.Add(1)
		return false
	}
}

func (m *AlertManager) getConfig() AlertConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	config := m.config
	config.Recipients = append([]string(nil), config.Recipients...)
	config.Types = append([]string(nil), config.Types...)
	return config
}

func buildAlertNotification(config AlertConfig, eventType string, state alertState, recovered bool) AlertNotification {
	status := "异常"
	subjectState := "告警"
	if recovered {
		status = "已恢复"
		subjectState = "恢复"
	}
	duration := state.lastAt.Sub(state.firstAt).Round(time.Second)
	content := fmt.Sprintf(
		"<h2>数据快照%s</h2><table><tr><td>节点</td><td>%s</td></tr><tr><td>版本</td><td>%s</td></tr><tr><td>状态</td><td>%s</td></tr><tr><td>类型</td><td>%s</td></tr><tr><td>首次时间</td><td>%s</td></tr><tr><td>最近时间</td><td>%s</td></tr><tr><td>持续时间</td><td>%s</td></tr><tr><td>事件次数</td><td>%d</td></tr><tr><td>丢弃快照</td><td>%d</td></tr><tr><td>影响字节</td><td>%d</td></tr></table>",
		subjectState, html.EscapeString(config.Node), html.EscapeString(config.Version), status,
		html.EscapeString(eventType), state.firstAt.Format(time.RFC3339), state.lastAt.Format(time.RFC3339),
		duration, state.count, state.dropped, state.bytes,
	)
	return AlertNotification{
		Subject:    fmt.Sprintf("[HUICHUAN] 数据快照%s：%s", subjectState, eventType),
		Recipients: append([]string(nil), config.Recipients...), HTML: content,
	}
}

func containsAlertType(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}
