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
