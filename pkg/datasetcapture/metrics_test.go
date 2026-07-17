package datasetcapture

import (
	"testing"
	"time"
)

func TestActivityHistoryReturnsWindowDeltas(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	history := activityHistory{}
	history.observe(start, ActivityWindow{})
	history.observe(start.Add(4*time.Minute), ActivityWindow{
		Submitted: 10, Written: 8, DroppedQueueFull: 1,
	})
	history.observe(start.Add(5*time.Minute), ActivityWindow{
		Submitted: 20, Written: 17, DroppedQueueFull: 2, DiskLowDropped: 1,
	})
	current := ActivityWindow{
		Submitted: 25, Written: 21, DroppedQueueFull: 3,
		DroppedSampleTooLarge: 2, DiskLowDropped: 1,
	}
	now := start.Add(5*time.Minute + 30*time.Second)

	lastMinute := history.since(now, time.Minute, current)
	if lastMinute.Submitted != 15 || lastMinute.Written != 13 || lastMinute.DroppedQueueFull != 2 {
		t.Fatalf("unexpected last-minute window: %#v", lastMinute)
	}
	if lastMinute.DroppedSampleTooLarge != 2 || lastMinute.DiskLowDropped != 1 {
		t.Fatalf("last-minute drop reasons were not preserved: %#v", lastMinute)
	}

	lastFiveMinutes := history.since(now, 5*time.Minute, current)
	if lastFiveMinutes.Submitted != 25 || lastFiveMinutes.Written != 21 || lastFiveMinutes.DroppedQueueFull != 3 {
		t.Fatalf("unexpected five-minute window: %#v", lastFiveMinutes)
	}
}
