package datasetcapture

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAlertManagerSendsImmediateAggregatedAndRecoveryNotifications(t *testing.T) {
	sent := make(chan AlertNotification, 4)
	manager := NewAlertManager(func(notification AlertNotification) error {
		sent <- notification
		return nil
	})
	defer manager.Close()
	manager.Update(AlertConfig{
		Enabled: true, Recipients: []string{"admin@example.com"}, Types: []string{EventQueueFull},
		Silence: time.Hour, AlertAfterDrops: 1, SendRecovery: true, Node: "node-a", Version: "v1",
	})
	manager.Notify(Event{Type: EventQueueFull, Dropped: 1})
	first := waitAlert(t, sent)
	if !strings.Contains(first.Subject, EventQueueFull) || strings.Contains(first.HTML, "secret prompt") {
		t.Fatalf("unexpected first alert: %#v", first)
	}
	manager.Notify(Event{Type: EventQueueFull, Dropped: 1, Err: assertSensitiveError("secret prompt")})
	select {
	case duplicate := <-sent:
		t.Fatalf("duplicate alert inside silence window: %#v", duplicate)
	case <-time.After(30 * time.Millisecond):
	}
	manager.Resolve(EventQueueFull)
	recovery := waitAlert(t, sent)
	if !strings.Contains(recovery.Subject, "恢复") {
		t.Fatalf("unexpected recovery: %#v", recovery)
	}
}

func TestAlertManagerCountsMailFailureWithoutStopping(t *testing.T) {
	attempted := make(chan struct{}, 2)
	manager := NewAlertManager(func(AlertNotification) error {
		attempted <- struct{}{}
		return errors.New("smtp unavailable")
	})
	defer manager.Close()
	manager.Update(AlertConfig{
		Enabled: true, Recipients: []string{"admin@example.com"}, Types: []string{EventJSONLWriteFailed},
		Silence: time.Hour, AlertAfterDrops: 1, Node: "node-a", Version: "v1",
	})
	manager.Notify(Event{Type: EventJSONLWriteFailed, Dropped: 1})
	select {
	case <-attempted:
	case <-time.After(time.Second):
		t.Fatal("mail worker did not attempt delivery")
	}
	deadline := time.Now().Add(time.Second)
	for manager.Status().SendFailed != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if status := manager.Status(); status.SendFailed != 1 {
		t.Fatalf("send failure was not counted: %#v", status)
	}
	if !manager.SendTest() {
		t.Fatal("mail worker stopped accepting notifications after a send failure")
	}
	select {
	case <-attempted:
	case <-time.After(time.Second):
		t.Fatal("mail worker stopped after a send failure")
	}
}

func TestAccessAlertManagerSupportsAllOperatorsAndOwners(t *testing.T) {
	sent := make(chan AlertNotification, 1)
	manager := NewAlertManager(func(notification AlertNotification) error {
		sent <- notification
		return nil
	})
	defer manager.Close()
	manager.Update(AlertConfig{
		Recipients: []string{"audit@example.com"}, Node: "node-a", Version: "v1",
		Access: AccessAlertConfig{
			Enabled: true, Actions: []string{"view", "download"},
			OperatorMode: "all", OwnerMode: "all",
		},
	})
	manager.NotifyAccess(AccessAlertEvent{
		EventID: "event-1", Action: "view", OperatorUserID: 9, OperatorUsername: "<root>",
		SelectionMode: "single_record", RecordCount: 1, UserCount: 1,
		Records: []AccessAlertRecord{{
			CaptureID: "capture-1", UserID: 20, Username: "<owner>",
			Model: "gpt-test", SessionID: "session-1",
		}},
	})
	notification := waitAlert(t, sent)
	if !strings.Contains(notification.Subject, "查看") ||
		!strings.Contains(notification.HTML, "&lt;root&gt;") ||
		!strings.Contains(notification.HTML, "&lt;owner&gt;") {
		t.Fatalf("unexpected access alert: %#v", notification)
	}
}

func TestAccessAlertManagerRequiresBothSelectedScopes(t *testing.T) {
	sent := make(chan AlertNotification, 2)
	manager := NewAlertManager(func(notification AlertNotification) error {
		sent <- notification
		return nil
	})
	defer manager.Close()
	manager.Update(AlertConfig{
		Recipients: []string{"audit@example.com"},
		Access: AccessAlertConfig{
			Enabled: true, Actions: []string{"download"},
			OperatorMode: "selected", OperatorUserIDs: []int{9},
			OwnerMode: "selected", OwnerUserIDs: []int{20},
		},
	})

	manager.NotifyAccess(accessAlertTestEvent("download", 8, 20))
	manager.NotifyAccess(accessAlertTestEvent("download", 9, 21))
	manager.NotifyAccess(accessAlertTestEvent("view", 9, 20))
	select {
	case unexpected := <-sent:
		t.Fatalf("access alert should require action, operator and owner to match: %#v", unexpected)
	case <-time.After(50 * time.Millisecond):
	}

	manager.NotifyAccess(accessAlertTestEvent("download", 9, 20))
	notification := waitAlert(t, sent)
	if !strings.Contains(notification.Subject, "下载") {
		t.Fatalf("unexpected access alert: %#v", notification)
	}
}

func TestAccessAlertEmailOnlyListsMatchingSelectedOwners(t *testing.T) {
	sent := make(chan AlertNotification, 1)
	manager := NewAlertManager(func(notification AlertNotification) error {
		sent <- notification
		return nil
	})
	defer manager.Close()
	manager.Update(AlertConfig{
		Recipients: []string{"audit@example.com"},
		Access: AccessAlertConfig{
			Enabled: true, Actions: []string{"download"}, OperatorMode: "all",
			OwnerMode: "selected", OwnerUserIDs: []int{20},
		},
	})
	event := accessAlertTestEvent("download", 9, 20)
	event.Records = append(event.Records, AccessAlertRecord{
		CaptureID: "unrelated-capture", UserID: 21, Username: "unrelated-owner",
	})
	event.RecordCount = 2
	event.UserCount = 2
	manager.NotifyAccess(event)

	notification := waitAlert(t, sent)
	if strings.Contains(notification.HTML, "unrelated-owner") ||
		!strings.Contains(notification.HTML, "owner") {
		t.Fatalf("selected-owner email leaked unrelated location metadata: %#v", notification)
	}
}

func TestAccessAlertManagerTestMailWorksWithoutOperationalAlerts(t *testing.T) {
	sent := make(chan AlertNotification, 1)
	manager := NewAlertManager(func(notification AlertNotification) error {
		sent <- notification
		return nil
	})
	defer manager.Close()
	manager.Update(AlertConfig{
		Recipients: []string{"audit@example.com"},
		Access:     AccessAlertConfig{Enabled: true, Actions: []string{"view"}},
	})
	if !manager.SendTest() {
		t.Fatal("access-only alert configuration should allow a test email")
	}
	if notification := waitAlert(t, sent); !strings.Contains(notification.Subject, "测试") {
		t.Fatalf("unexpected test notification: %#v", notification)
	}
}

func TestAccessAlertNotificationDoesNotBlockOnSMTP(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	manager := NewAlertManager(func(AlertNotification) error {
		started <- struct{}{}
		<-release
		return nil
	})
	manager.Update(AlertConfig{
		Recipients: []string{"audit@example.com"},
		Access: AccessAlertConfig{
			Enabled: true, Actions: []string{"view"}, OperatorMode: "all", OwnerMode: "all",
		},
	})
	manager.NotifyAccess(accessAlertTestEvent("view", 9, 20))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("SMTP worker did not start")
	}

	returned := make(chan struct{})
	go func() {
		manager.NotifyAccess(accessAlertTestEvent("view", 9, 20))
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("access notification waited for SMTP")
	}
	close(release)
	manager.Close()
}

type assertSensitiveError string

func (e assertSensitiveError) Error() string { return string(e) }

func waitAlert(t *testing.T, alerts <-chan AlertNotification) AlertNotification {
	t.Helper()
	select {
	case notification := <-alerts:
		return notification
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for alert")
		return AlertNotification{}
	}
}

func accessAlertTestEvent(action string, operatorID, ownerID int) AccessAlertEvent {
	return AccessAlertEvent{
		EventID: "event-test", Action: action, OperatorUserID: operatorID,
		OperatorUsername: "admin", RecordCount: 1, UserCount: 1,
		Records: []AccessAlertRecord{{CaptureID: "capture", UserID: ownerID, Username: "owner"}},
	}
}
