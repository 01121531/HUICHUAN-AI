package common

import "sync/atomic"

const (
	UsageLogAlertQueueFull   = "usage_log_queue_full"
	UsageLogAlertWriteFailed = "usage_log_write_failed"
)

type UsageLogAlertEvent struct {
	Type     string
	Dropped  int64
	Resolved bool
}

type usageLogAlertNotifier func(UsageLogAlertEvent)

var usageLogAlertCallback atomic.Value

func SetUsageLogAlertNotifier(notifier func(UsageLogAlertEvent)) {
	if notifier == nil {
		notifier = func(UsageLogAlertEvent) {}
	}
	usageLogAlertCallback.Store(usageLogAlertNotifier(notifier))
}

func NotifyUsageLogAlert(event UsageLogAlertEvent) {
	value := usageLogAlertCallback.Load()
	if value == nil {
		return
	}
	value.(usageLogAlertNotifier)(event)
}
