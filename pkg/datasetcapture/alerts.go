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
	Access          AccessAlertConfig
}

type AccessAlertConfig struct {
	Enabled         bool
	Actions         []string
	OperatorMode    string
	OperatorUserIDs []int
	OwnerMode       string
	OwnerUserIDs    []int
}

type AccessAlertRecord struct {
	CaptureID string
	UserID    int
	Username  string
	Model     string
	SessionID string
}

type AccessAlertEvent struct {
	EventID          string
	Action           string
	OperatorUserID   int
	OperatorUsername string
	OperatorRole     int
	SelectionMode    string
	RecordCount      int
	UserCount        int
	Bytes            int64
	At               time.Time
	Records          []AccessAlertRecord
}

type AlertNotification struct {
	Subject    string
	Recipients []string
	HTML       string
}

type AlertStatus struct {
	EventQueueDepth  int   `json:"event_queue_depth"`
	AccessQueueDepth int   `json:"access_queue_depth"`
	MailQueueDepth   int   `json:"mail_queue_depth"`
	EventsDropped    int64 `json:"events_dropped"`
	AccessDropped    int64 `json:"access_dropped"`
	AccessQueued     int64 `json:"access_queued"`
	MailDropped      int64 `json:"mail_dropped"`
	SendFailed       int64 `json:"send_failed"`
	LastAlertAt      int64 `json:"last_alert_at"`
	LastAccessAt     int64 `json:"last_access_at"`
	LastRecoveryAt   int64 `json:"last_recovery_at"`
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
	accessEvents   chan AccessAlertEvent
	mail           chan AlertNotification
	done           chan struct{}
	stop           chan struct{}
	once           sync.Once
	mu             sync.RWMutex
	config         AlertConfig
	states         map[string]*alertState
	eventsDropped  atomic.Int64
	accessDropped  atomic.Int64
	accessQueued   atomic.Int64
	mailDropped    atomic.Int64
	sendFailed     atomic.Int64
	lastAlertAt    atomic.Int64
	lastAccessAt   atomic.Int64
	lastRecoveryAt atomic.Int64
}

func NewAlertManager(sender func(AlertNotification) error) *AlertManager {
	manager := &AlertManager{
		sender: sender, events: make(chan Event, 256), accessEvents: make(chan AccessAlertEvent, 256),
		mail: make(chan AlertNotification, 32), done: make(chan struct{}), stop: make(chan struct{}),
		states: make(map[string]*alertState),
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
	cloneAlertConfig(&config)
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

// NotifyAccess only transfers bounded metadata to a background queue. Email
// formatting and SMTP delivery never run in the view/download request path.
func (m *AlertManager) NotifyAccess(event AccessAlertEvent) {
	if m == nil {
		return
	}
	if event.At.IsZero() {
		event.At = time.Now()
	}
	event.Records = append([]AccessAlertRecord(nil), event.Records...)
	select {
	case m.accessEvents <- event:
	default:
		m.accessDropped.Add(1)
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
	if (!config.Enabled && !config.Access.Enabled) || len(config.Recipients) == 0 {
		return false
	}
	return m.enqueueMail(AlertNotification{
		Subject:    "[HUICHUAN] 数据快照告警测试",
		Recipients: append([]string(nil), config.Recipients...),
		HTML: fmt.Sprintf(
			"<h2>数据快照告警测试</h2><p>节点：%s</p><p>版本：%s</p>",
			html.EscapeString(config.Node), html.EscapeString(config.Version),
		),
	})
}

func (m *AlertManager) Status() AlertStatus {
	if m == nil {
		return AlertStatus{}
	}
	return AlertStatus{
		EventQueueDepth: len(m.events), AccessQueueDepth: len(m.accessEvents), MailQueueDepth: len(m.mail),
		EventsDropped: m.eventsDropped.Load(), AccessDropped: m.accessDropped.Load(),
		AccessQueued: m.accessQueued.Load(), MailDropped: m.mailDropped.Load(),
		SendFailed: m.sendFailed.Load(), LastAlertAt: m.lastAlertAt.Load(),
		LastAccessAt: m.lastAccessAt.Load(), LastRecoveryAt: m.lastRecoveryAt.Load(),
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
		case event := <-m.accessEvents:
			m.handleAccessEvent(event)
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
	shouldSend := state.count >= config.AlertAfterDrops &&
		(state.lastSent.IsZero() || event.At.Sub(state.lastSent) >= config.Silence)
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

func (m *AlertManager) handleAccessEvent(event AccessAlertEvent) {
	config := m.getConfig()
	if !matchesAccessAlert(config.Access, event) || len(config.Recipients) == 0 {
		return
	}
	if m.enqueueMail(buildAccessAlertNotification(config, event)) {
		m.accessQueued.Add(1)
		m.lastAccessAt.Store(event.At.Unix())
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
	cloneAlertConfig(&config)
	return config
}

func cloneAlertConfig(config *AlertConfig) {
	config.Recipients = append([]string(nil), config.Recipients...)
	config.Types = append([]string(nil), config.Types...)
	config.Access.Actions = append([]string(nil), config.Access.Actions...)
	config.Access.OperatorUserIDs = append([]int(nil), config.Access.OperatorUserIDs...)
	config.Access.OwnerUserIDs = append([]int(nil), config.Access.OwnerUserIDs...)
}

func buildAlertNotification(config AlertConfig, eventType string, state alertState, recovered bool) AlertNotification {
	category := "数据快照"
	if strings.HasPrefix(eventType, "usage_log_") {
		category = "使用日志"
	}
	status := "异常"
	subjectState := "告警"
	if recovered {
		status = "已恢复"
		subjectState = "恢复"
	}
	duration := state.lastAt.Sub(state.firstAt).Round(time.Second)
	content := fmt.Sprintf(
		"<h2>%s%s</h2><table><tr><td>节点</td><td>%s</td></tr><tr><td>版本</td><td>%s</td></tr><tr><td>状态</td><td>%s</td></tr><tr><td>类型</td><td>%s</td></tr><tr><td>首次时间</td><td>%s</td></tr><tr><td>最近时间</td><td>%s</td></tr><tr><td>持续时间</td><td>%s</td></tr><tr><td>事件次数</td><td>%d</td></tr><tr><td>丢弃记录</td><td>%d</td></tr><tr><td>影响字节</td><td>%d</td></tr></table>",
		category, subjectState, html.EscapeString(config.Node), html.EscapeString(config.Version), status,
		html.EscapeString(eventType), state.firstAt.Format(time.RFC3339), state.lastAt.Format(time.RFC3339),
		duration, state.count, state.dropped, state.bytes,
	)
	return AlertNotification{
		Subject:    fmt.Sprintf("[HUICHUAN] %s%s：%s", category, subjectState, eventType),
		Recipients: append([]string(nil), config.Recipients...), HTML: content,
	}
}

func matchesAccessAlert(config AccessAlertConfig, event AccessAlertEvent) bool {
	if !config.Enabled || !containsAlertType(config.Actions, event.Action) {
		return false
	}
	if config.OperatorMode == "selected" && !containsInt(config.OperatorUserIDs, event.OperatorUserID) {
		return false
	}
	if config.OwnerMode != "selected" {
		return true
	}
	for _, record := range event.Records {
		if containsInt(config.OwnerUserIDs, record.UserID) {
			return true
		}
	}
	return false
}

func buildAccessAlertNotification(config AlertConfig, event AccessAlertEvent) AlertNotification {
	actionLabel := event.Action
	switch event.Action {
	case "view":
		actionLabel = "查看"
	case "download":
		actionLabel = "下载"
	}
	records := event.Records
	if config.Access.OwnerMode == "selected" {
		records = make([]AccessAlertRecord, 0, len(event.Records))
		for _, record := range event.Records {
			if containsInt(config.Access.OwnerUserIDs, record.UserID) {
				records = append(records, record)
			}
		}
	}
	var rows strings.Builder
	const maxDetails = 20
	for index, record := range records {
		if index >= maxDetails {
			break
		}
		fmt.Fprintf(
			&rows,
			"<tr><td>%s</td><td>%d / %s</td><td>%s</td><td>%s</td></tr>",
			html.EscapeString(record.CaptureID), record.UserID, html.EscapeString(record.Username),
			html.EscapeString(record.Model), html.EscapeString(record.SessionID),
		)
	}
	detailNote := ""
	if len(records) > maxDetails {
		detailNote = fmt.Sprintf("<p>仅展示前 %d 条定位信息，其余请通过访问审计查看。</p>", maxDetails)
	}
	content := fmt.Sprintf(
		"<h2>数据快照访问提醒</h2><table><tr><td>节点</td><td>%s</td></tr><tr><td>版本</td><td>%s</td></tr><tr><td>时间</td><td>%s</td></tr><tr><td>审计事件</td><td>%s</td></tr><tr><td>操作人</td><td>%d / %s</td></tr><tr><td>操作人角色</td><td>%d</td></tr><tr><td>操作</td><td>%s</td></tr><tr><td>选择方式</td><td>%s</td></tr><tr><td>记录数</td><td>%d</td></tr><tr><td>用户数</td><td>%d</td></tr><tr><td>导出字节数</td><td>%d</td></tr></table><h3>匹配的数据定位（不含对话正文和令牌密钥）</h3><table><tr><th>快照 ID</th><th>所属用户</th><th>模型</th><th>会话 ID</th></tr>%s</table>%s",
		html.EscapeString(config.Node), html.EscapeString(config.Version), event.At.Format(time.RFC3339),
		html.EscapeString(event.EventID), event.OperatorUserID, html.EscapeString(event.OperatorUsername),
		event.OperatorRole, actionLabel, html.EscapeString(event.SelectionMode), event.RecordCount, event.UserCount, event.Bytes,
		rows.String(), detailNote,
	)
	return AlertNotification{
		Subject:    fmt.Sprintf("[HUICHUAN] 数据快照%s提醒：%s", actionLabel, safeSubjectValue(event.OperatorUsername)),
		Recipients: append([]string(nil), config.Recipients...),
		HTML:       content,
	}
}

func safeSubjectValue(value string) string {
	return strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(value))
}

func containsAlertType(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}

func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
