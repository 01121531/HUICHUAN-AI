package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUsageLogAlertNotifierReceivesBoundedMetadata(t *testing.T) {
	events := make(chan UsageLogAlertEvent, 1)
	SetUsageLogAlertNotifier(func(event UsageLogAlertEvent) { events <- event })
	t.Cleanup(func() { SetUsageLogAlertNotifier(nil) })

	want := UsageLogAlertEvent{Type: UsageLogAlertQueueFull, Dropped: 1}
	NotifyUsageLogAlert(want)
	assert.Equal(t, want, <-events)
}
