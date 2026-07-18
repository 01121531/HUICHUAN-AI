package datasetcapture

import (
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const (
	EventQueueFull             = "queue_full"
	EventInFlightBytesExceeded = "inflight_bytes_exceeded"
	EventSampleTooLarge        = "sample_too_large"
	EventDiskLimitReached      = "disk_limit_reached"
	EventDiskLow               = "disk_low"
	EventJSONLWriteFailed      = "jsonl_write_failed"
	EventIndexWriteFailed      = "index_write_failed"
	EventSpoolWriteFailed      = "spool_write_failed"
	EventWorkerPanic           = "worker_panic"
	EventCaptureIncomplete     = "incomplete_capture"
)

var ErrDiskLimitReached = errors.New("dataset capture disk limit reached")
var ErrWorkerPanic = errors.New("dataset capture worker panic")
var ErrSpoolWriteFailed = errors.New("dataset capture spool write failed")

type Event struct {
	Type     string
	At       time.Time
	Dropped  int64
	Bytes    int64
	Err      error
	Resolved bool
}

type WriterStatus struct {
	QueueDepth            int            `json:"queue_depth"`
	QueueCapacity         int            `json:"queue_capacity"`
	NormalizedDepth       int            `json:"normalized_depth"`
	IndexQueueDepth       int            `json:"index_queue_depth"`
	IndexQueueCapacity    int            `json:"index_queue_capacity"`
	InFlightBytes         int64          `json:"inflight_bytes"`
	DiskBytes             int64          `json:"disk_bytes"`
	FreeDiskBytes         int64          `json:"free_disk_bytes"`
	Submitted             int64          `json:"submitted"`
	Written               int64          `json:"written"`
	DroppedQueueFull      int64          `json:"dropped_queue_full"`
	DroppedSampleTooLarge int64          `json:"dropped_sample_too_large"`
	DroppedInFlightLimit  int64          `json:"dropped_inflight_limit"`
	BuildFailed           int64          `json:"build_failed"`
	IncompleteDropped     int64          `json:"incomplete_dropped"`
	JSONLWriteFailed      int64          `json:"jsonl_write_failed"`
	IndexWriteFailed      int64          `json:"index_write_failed"`
	DiskLimitDropped      int64          `json:"disk_limit_dropped"`
	DiskLowDropped        int64          `json:"disk_low_dropped"`
	LastMinute            ActivityWindow `json:"last_minute"`
	LastFiveMinutes       ActivityWindow `json:"last_five_minutes"`
	JSONLWriteP50MS       int64          `json:"jsonl_write_p50_ms"`
	JSONLWriteP95MS       int64          `json:"jsonl_write_p95_ms"`
	IndexWriteP50MS       int64          `json:"index_write_p50_ms"`
	IndexWriteP95MS       int64          `json:"index_write_p95_ms"`
	LastErrorType         string         `json:"last_error_type"`
	LastErrorAt           int64          `json:"last_error_at"`
}

type ActivityWindow struct {
	Submitted             int64 `json:"submitted"`
	Written               int64 `json:"written"`
	DroppedQueueFull      int64 `json:"dropped_queue_full"`
	DroppedSampleTooLarge int64 `json:"dropped_sample_too_large"`
	DroppedInFlightLimit  int64 `json:"dropped_inflight_limit"`
	BuildFailed           int64 `json:"build_failed"`
	IncompleteDropped     int64 `json:"incomplete_dropped"`
	JSONLWriteFailed      int64 `json:"jsonl_write_failed"`
	DiskLimitDropped      int64 `json:"disk_limit_dropped"`
	DiskLowDropped        int64 `json:"disk_low_dropped"`
}

func (w ActivityWindow) subtract(previous ActivityWindow) ActivityWindow {
	return ActivityWindow{
		Submitted: w.Submitted - previous.Submitted, Written: w.Written - previous.Written,
		DroppedQueueFull:      w.DroppedQueueFull - previous.DroppedQueueFull,
		DroppedSampleTooLarge: w.DroppedSampleTooLarge - previous.DroppedSampleTooLarge,
		DroppedInFlightLimit:  w.DroppedInFlightLimit - previous.DroppedInFlightLimit,
		BuildFailed:           w.BuildFailed - previous.BuildFailed,
		IncompleteDropped:     w.IncompleteDropped - previous.IncompleteDropped,
		JSONLWriteFailed:      w.JSONLWriteFailed - previous.JSONLWriteFailed,
		DiskLimitDropped:      w.DiskLimitDropped - previous.DiskLimitDropped,
		DiskLowDropped:        w.DiskLowDropped - previous.DiskLowDropped,
	}
}

type activitySample struct {
	at     time.Time
	totals ActivityWindow
}

type activityHistory struct {
	mu      sync.Mutex
	samples []activitySample
}

func (h *activityHistory) observe(at time.Time, totals ActivityWindow) {
	h.mu.Lock()
	h.samples = append(h.samples, activitySample{at: at, totals: totals})
	cutoff := at.Add(-6 * time.Minute)
	first := 0
	for first < len(h.samples)-1 && h.samples[first+1].at.Before(cutoff) {
		first++
	}
	if first > 0 {
		copy(h.samples, h.samples[first:])
		h.samples = h.samples[:len(h.samples)-first]
	}
	h.mu.Unlock()
}

func (h *activityHistory) since(now time.Time, window time.Duration, current ActivityWindow) ActivityWindow {
	cutoff := now.Add(-window)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.samples) == 0 {
		return ActivityWindow{}
	}
	baseline := h.samples[0].totals
	for _, sample := range h.samples {
		if sample.at.After(cutoff) {
			break
		}
		baseline = sample.totals
	}
	return current.subtract(baseline)
}

type writerMetrics struct {
	submitted         atomic.Int64
	written           atomic.Int64
	droppedQueueFull  atomic.Int64
	buildFailed       atomic.Int64
	incompleteDropped atomic.Int64
	jsonlWriteFailed  atomic.Int64
	indexWriteFailed  atomic.Int64
	diskLimitDropped  atomic.Int64
	diskLowDropped    atomic.Int64
	activity          activityHistory
	jsonlLatency      latencyWindow
	indexLatency      latencyWindow
	lastErrorMu       sync.RWMutex
	lastErrorType     string
	lastErrorAt       int64
}

type latencyWindow struct {
	mu     sync.Mutex
	values []int64
	next   int
}

func (w *latencyWindow) observe(duration time.Duration) {
	value := duration.Milliseconds()
	w.mu.Lock()
	if len(w.values) < 256 {
		w.values = append(w.values, value)
	} else {
		w.values[w.next] = value
		w.next = (w.next + 1) % 256
	}
	w.mu.Unlock()
}

func (w *latencyWindow) percentiles() (int64, int64) {
	w.mu.Lock()
	values := append([]int64(nil), w.values...)
	w.mu.Unlock()
	if len(values) == 0 {
		return 0, 0
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return percentile(values, 50), percentile(values, 95)
}

func percentile(values []int64, percent int) int64 {
	index := (len(values)*percent + 99) / 100
	if index < 1 {
		index = 1
	}
	return values[index-1]
}

func (m *writerMetrics) setLastError(eventType string) {
	m.lastErrorMu.Lock()
	m.lastErrorType = eventType
	m.lastErrorAt = time.Now().Unix()
	m.lastErrorMu.Unlock()
}

func (m *writerMetrics) lastError() (string, int64) {
	m.lastErrorMu.RLock()
	defer m.lastErrorMu.RUnlock()
	return m.lastErrorType, m.lastErrorAt
}
