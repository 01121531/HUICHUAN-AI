package service

import (
	"strconv"
	"testing"
	"time"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/model"
)

func withNERVProxyTestOptions(t *testing.T) {
	t.Helper()

	common.OptionMapRWMutex.Lock()
	previous := common.OptionMap
	previousDB := model.DB
	common.OptionMap = map[string]string{
		NERVProxyRecentKey: "[]",
	}
	model.DB = nil
	common.OptionMapRWMutex.Unlock()

	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previous
		model.DB = previousDB
		common.OptionMapRWMutex.Unlock()
	})
}

func TestNERVProxySnapshotStatsAndFilters(t *testing.T) {
	withNERVProxyTestOptions(t)
	now := time.Unix(1_700_000_000, 0)

	common.OptionMapRWMutex.Lock()
	common.OptionMap[NERVProxyRecentKey] = appendNERVProxyEventLocked(now, nervEventInjectChat, NERVTargetOpenAIChat, "gpt-test", "用户请求", "", "prompt_inject")
	common.OptionMap[NERVProxyRecentKey] = appendNERVProxyEventLocked(now.Add(time.Second), nervEventTamperResponses, NERVTargetOpenAIResponses, "gpt-test", "原回复", "替换回复", "stream_tamper")
	common.OptionMapRWMutex.Unlock()

	events, stats := NERVProxySnapshot(10, "", nil)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if stats.Total != 2 || stats.Inject != 1 || stats.Tamper != 1 || stats.Stream != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if stats.ChatInject != 1 || stats.ResponsesTamper != 1 {
		t.Fatalf("unexpected target stats: %+v", stats)
	}
	if events[0].Path != "/v1/responses" || !events[0].Tampered || !events[0].Stream {
		t.Fatalf("latest responses event not recorded correctly: %+v", events[0])
	}
	if events[1].Path != "/v1/chat/completions" || !events[1].Injected {
		t.Fatalf("chat event not recorded correctly: %+v", events[1])
	}

	onlyTampered := true
	filtered, filteredStats := NERVProxySnapshot(10, "", &onlyTampered)
	if len(filtered) != 1 || filteredStats.Total != 1 || filtered[0].Target != string(NERVTargetOpenAIResponses) {
		t.Fatalf("tamper filter failed: events=%+v stats=%+v", filtered, filteredStats)
	}

	targetFiltered, targetStats := NERVProxySnapshot(10, string(NERVTargetOpenAIChat), nil)
	if len(targetFiltered) != 1 || targetStats.ChatInject != 1 || targetFiltered[0].Target != string(NERVTargetOpenAIChat) {
		t.Fatalf("target filter failed: events=%+v stats=%+v", targetFiltered, targetStats)
	}
}

func TestNERVProxySnapshotRingLimitAndClear(t *testing.T) {
	withNERVProxyTestOptions(t)
	now := time.Unix(1_700_000_000, 0)

	common.OptionMapRWMutex.Lock()
	for i := 0; i < nervProxyLogLimit+5; i++ {
		common.OptionMap[NERVProxyRecentKey] = appendNERVProxyEventLocked(
			now.Add(time.Duration(i)*time.Second),
			nervEventInjectChat,
			NERVTargetOpenAIChat,
			"gpt-"+strconv.Itoa(i),
			"request "+strconv.Itoa(i),
			"",
			"prompt_inject",
		)
	}
	common.OptionMapRWMutex.Unlock()

	events, stats := NERVProxySnapshot(nervProxyLogLimit+50, "", nil)
	if len(events) != nervProxyLogLimit {
		t.Fatalf("expected ring limit %d, got %d", nervProxyLogLimit, len(events))
	}
	if stats.Total != nervProxyLogLimit {
		t.Fatalf("expected stats to use retained events, got %+v", stats)
	}
	if events[0].Model != "gpt-104" {
		t.Fatalf("expected newest event first, got %+v", events[0])
	}

	if err := ClearNERVProxyLogs(); err != nil {
		t.Fatal(err)
	}
	events, stats = NERVProxySnapshot(10, "", nil)
	if len(events) != 0 || stats.Total != 0 {
		t.Fatalf("clear failed: events=%+v stats=%+v", events, stats)
	}
}
