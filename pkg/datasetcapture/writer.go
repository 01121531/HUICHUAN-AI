package datasetcapture

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrWriterClosed = errors.New("dataset capture writer is closed")
	ErrQueueFull    = errors.New("dataset capture queue is full")
	captureFileMu   sync.Mutex
)

type WriterConfig struct {
	PathTemplate        string
	Node                string
	QueueSize           int
	Workers             int
	IndexQueueSize      int
	IndexBatchSize      int
	IndexFlushInterval  time.Duration
	SegmentSize         int
	MaxSampleBytes      int64
	MaxInFlightBytes    int64
	SpoolThresholdBytes int64
	MaxDiskBytes        int64
	MinFreeDiskBytes    int64
	Partitioned         bool
	OnError             func(error)
	OnEvent             func(Event)
	OnWritten           func(WriteResult) error
	OnWrittenBatch      func([]WriteResult) error
}

type WriteResult struct {
	CaptureID string
	FileID    string
	Node      string
	Row       int64
	Bytes     int64
	Record    Record
}

type captureTask struct {
	sequence uint64
	record   Record
	session  *Session
}

type normalizedTask struct {
	sequence uint64
	record   Record
	err      error
}

func (t captureTask) buildRecord() (Record, error) {
	if t.session != nil {
		return t.session.BuildRecord()
	}
	if err := Validate(t.record); err != nil {
		return Record{}, err
	}
	return t.record, nil
}

func (t captureTask) release() {
	if t.session != nil {
		t.session.Release()
	}
}

type Writer struct {
	config          WriterConfig
	buffers         *BufferPool
	queue           chan captureTask
	normalizedQueue chan normalizedTask
	indexQueue      chan WriteResult
	diskBytes       atomic.Int64
	freeDiskBytes   atomic.Int64
	metrics         writerMetrics
	done            chan struct{}
	mu              sync.RWMutex
	nextSequence    uint64
	closed          bool
	once            sync.Once
}

func NewWriter(config WriterConfig) (*Writer, error) {
	if strings.TrimSpace(config.PathTemplate) == "" {
		return nil, errors.New("dataset capture path is empty")
	}
	if config.QueueSize <= 0 {
		config.QueueSize = 128
	}
	if config.Workers <= 0 {
		config.Workers = 2
	}
	if config.Node == "" {
		config.Node = "node"
	}
	if config.IndexQueueSize <= 0 {
		config.IndexQueueSize = config.QueueSize * 2
	}
	if config.IndexBatchSize <= 0 {
		config.IndexBatchSize = 50
	}
	if config.IndexFlushInterval <= 0 {
		config.IndexFlushInterval = time.Second
	}
	writer := &Writer{
		config:          config,
		buffers:         NewBufferPool(config.SegmentSize, config.MaxInFlightBytes),
		queue:           make(chan captureTask, config.QueueSize),
		normalizedQueue: make(chan normalizedTask, config.QueueSize),
		done:            make(chan struct{}),
	}
	initialPath := writer.resolvePath(time.Now(), Record{})
	if err := os.MkdirAll(writer.storageRoot(time.Now()), 0o700); err != nil {
		return nil, err
	}
	if !config.Partitioned {
		if err := recoverJSONLTail(initialPath); err != nil {
			return nil, err
		}
	}
	initialDiskBytes, err := directorySize(writer.storageRoot(time.Now()))
	if err != nil {
		return nil, err
	}
	writer.diskBytes.Store(initialDiskBytes)
	if freeBytes, freeErr := availableDiskBytes(writer.storageRoot(time.Now())); freeErr == nil {
		writer.freeDiskBytes.Store(freeBytes)
	} else if config.MinFreeDiskBytes > 0 {
		return nil, freeErr
	}
	if config.OnWritten != nil || config.OnWrittenBatch != nil {
		writer.indexQueue = make(chan WriteResult, config.IndexQueueSize)
	}
	go writer.run()
	return writer, nil
}

func (w *Writer) NewResponseBuffer() *SegmentedBuffer {
	if w == nil {
		return nil
	}
	return w.buffers.NewBuffer(w.config.MaxSampleBytes)
}

func (w *Writer) PrepareSession(session *Session) {
	if w == nil || session == nil {
		return
	}
	session.SetRequestBufferFactory(w.NewResponseBuffer)
}

func (w *Writer) Submit(record Record) error {
	return w.submit(captureTask{record: record})
}

func (w *Writer) SubmitSession(session *Session) error {
	if session == nil {
		return ErrIncompleteCapture
	}
	session.SetSpoolThreshold(w.config.SpoolThresholdBytes)
	if err := session.reserveRetainedMemory(w.buffers, w.config.MaxSampleBytes); err != nil {
		session.Release()
		w.ReportCaptureDrop(err)
		return err
	}
	return w.submit(captureTask{session: session})
}

func (w *Writer) submit(task captureTask) error {
	if w == nil {
		task.release()
		return ErrWriterClosed
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		task.release()
		return ErrWriterClosed
	}
	task.sequence = w.nextSequence
	select {
	case w.queue <- task:
		w.nextSequence++
		w.metrics.submitted.Add(1)
		w.emit(Event{Type: EventQueueFull, Resolved: true})
		return nil
	default:
		task.release()
		w.metrics.droppedQueueFull.Add(1)
		w.emit(Event{Type: EventQueueFull, Dropped: 1})
		return ErrQueueFull
	}
}

func (w *Writer) Close() error {
	return w.CloseContext(context.Background())
}

func (w *Writer) CloseContext(ctx context.Context) error {
	if w == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	w.once.Do(func() {
		w.mu.Lock()
		w.closed = true
		close(w.queue)
		w.mu.Unlock()
	})
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Writer) run() {
	defer close(w.done)
	metricsDone := make(chan struct{})
	go w.runMetricsSampler(metricsDone)
	defer close(metricsDone)
	var indexDone chan struct{}
	if w.indexQueue != nil {
		indexDone = make(chan struct{})
		go w.runIndexWriter(w.indexQueue, indexDone)
	}
	var normalizers sync.WaitGroup
	normalizers.Add(w.config.Workers)
	for range w.config.Workers {
		go func() {
			defer normalizers.Done()
			w.runNormalizer()
		}()
	}
	go func() {
		normalizers.Wait()
		close(w.normalizedQueue)
	}()
	w.runFileWriter()
	if w.indexQueue != nil {
		close(w.indexQueue)
		<-indexDone
	}
}

func (w *Writer) runMetricsSampler(done <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	w.metrics.activity.observe(time.Now(), w.activityTotals())
	for {
		select {
		case now := <-ticker.C:
			w.metrics.activity.observe(now, w.activityTotals())
		case <-done:
			return
		}
	}
}

func (w *Writer) runNormalizer() {
	for task := range w.queue {
		record, err := safelyBuildRecord(task.buildRecord)
		task.release()
		if errors.Is(err, ErrWorkerPanic) {
			w.reportEvent(EventWorkerPanic, err, 1, 0)
		} else if errors.Is(err, ErrSpoolWriteFailed) {
			w.reportEvent(EventSpoolWriteFailed, err, 1, 0)
		} else if errors.Is(err, ErrIncompleteCapture) {
			w.metrics.incompleteDropped.Add(1)
			w.metrics.setLastError(EventCaptureIncomplete)
			w.emit(Event{Type: EventCaptureIncomplete, Err: err, Dropped: 1})
		} else if errors.Is(err, ErrSampleTooLarge) || errors.Is(err, ErrInFlightLimitReached) {
			w.ReportCaptureDrop(err)
		} else if err == nil {
			w.emit(Event{Type: EventWorkerPanic, Resolved: true})
			w.emit(Event{Type: EventSpoolWriteFailed, Resolved: true})
		}
		w.normalizedQueue <- normalizedTask{sequence: task.sequence, record: record, err: err}
	}
}

func safelyBuildRecord(build func() (Record, error)) (record Record, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%w: %v", ErrWorkerPanic, recovered)
		}
	}()
	return build()
}

func (w *Writer) runFileWriter() {
	rows := map[string]int64{}
	pending := map[uint64]normalizedTask{}
	var nextSequence uint64
	lastDiskCalibration := time.Now()
	lastFreeDiskCheck := time.Now()
	for result := range w.normalizedQueue {
		pending[result.sequence] = result
		for {
			current, ok := pending[nextSequence]
			if !ok {
				break
			}
			delete(pending, nextSequence)
			nextSequence++
			if current.err != nil {
				w.metrics.buildFailed.Add(1)
				w.report(current.err)
				continue
			}
			w.writeRecord(current.record, rows, &lastDiskCalibration, &lastFreeDiskCheck)
		}
	}
}

func (w *Writer) writeRecord(record Record, rows map[string]int64, lastDiskCalibration, lastFreeDiskCheck *time.Time) {
	now := time.Now()
	path := w.resolvePath(now, record)
	if w.config.MinFreeDiskBytes > 0 {
		if now.Sub(*lastFreeDiskCheck) >= 10*time.Second {
			freeBytes, err := availableDiskBytes(w.storageRoot(now))
			if err != nil {
				w.reportEvent(EventDiskLow, err, 1, 0)
				return
			}
			w.freeDiskBytes.Store(freeBytes)
			*lastFreeDiskCheck = now
		}
		if freeBytes := w.freeDiskBytes.Load(); freeBytes < w.config.MinFreeDiskBytes {
			w.metrics.diskLowDropped.Add(1)
			err := fmt.Errorf("dataset capture free disk space below limit: %d bytes", freeBytes)
			w.reportEvent(EventDiskLow, err, 1, 0)
			return
		}
	}
	if w.config.MaxDiskBytes > 0 {
		if now.Sub(*lastDiskCalibration) >= time.Minute {
			size, err := directorySize(w.storageRoot(now))
			if err != nil {
				w.report(err)
				return
			}
			w.diskBytes.Store(size)
			*lastDiskCalibration = now
		}
		if size := w.diskBytes.Load(); size >= w.config.MaxDiskBytes {
			w.metrics.diskLimitDropped.Add(1)
			err := fmt.Errorf("%w: %d bytes", ErrDiskLimitReached, size)
			w.reportEvent(EventDiskLimitReached, err, 1, 0)
			return
		}
	}
	captureFileMu.Lock()
	startedAt := time.Now()
	writtenRecord, bytesWritten, err := appendQueuedRecord(path, record, rows)
	captureFileMu.Unlock()
	w.metrics.jsonlLatency.observe(time.Since(startedAt))
	if err == nil {
		w.diskBytes.Add(bytesWritten)
		w.metrics.written.Add(1)
		w.emit(Event{Type: EventDiskLow, Resolved: true})
		w.emit(Event{Type: EventDiskLimitReached, Resolved: true})
		w.emit(Event{Type: EventJSONLWriteFailed, Resolved: true})
	} else {
		w.metrics.jsonlWriteFailed.Add(1)
		w.reportEvent(EventJSONLWriteFailed, err, 1, 0)
	}
	if err == nil && w.indexQueue != nil {
		fileID := captureFileID(path)
		w.indexQueue <- WriteResult{
			CaptureID: RecordID(fileID, writtenRecord.Meta.SourceRow),
			FileID:    fileID,
			Node:      safeNodeName(w.config.Node),
			Row:       writtenRecord.Meta.SourceRow,
			Bytes:     bytesWritten,
			Record:    writtenRecord,
		}
	}
	if err == nil {
		w.report(nil)
	}
}

func (w *Writer) DiskBytes() int64 {
	if w == nil {
		return 0
	}
	return w.diskBytes.Load()
}

func (w *Writer) runIndexWriter(queue <-chan WriteResult, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(w.config.IndexFlushInterval)
	defer ticker.Stop()
	batch := make([]WriteResult, 0, w.config.IndexBatchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		var err error
		startedAt := time.Now()
		for attempt := 0; attempt < 3; attempt++ {
			if w.config.OnWrittenBatch != nil {
				err = w.config.OnWrittenBatch(batch)
			} else {
				for _, result := range batch {
					if callbackErr := w.config.OnWritten(result); callbackErr != nil && err == nil {
						err = callbackErr
					}
				}
			}
			if err == nil {
				break
			}
			if attempt < 2 {
				time.Sleep(time.Duration(100*(1<<attempt)) * time.Millisecond)
				err = nil
			}
		}
		w.metrics.indexLatency.observe(time.Since(startedAt))
		if err != nil {
			w.metrics.indexWriteFailed.Add(1)
			w.reportEvent(EventIndexWriteFailed, err, 0, 0)
		} else {
			w.emit(Event{Type: EventIndexWriteFailed, Resolved: true})
		}
		batch = batch[:0]
	}
	for {
		select {
		case result, ok := <-queue:
			if !ok {
				flush()
				return
			}
			batch = append(batch, result)
			if len(batch) >= w.config.IndexBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func appendQueuedRecord(path string, record Record, rows map[string]int64) (Record, int64, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		delete(rows, path)
	} else if err != nil {
		return Record{}, 0, err
	}
	if _, ok := rows[path]; !ok {
		if err := recoverJSONLTail(path); err != nil {
			return Record{}, 0, err
		}
		count, err := countValidLines(path)
		if err != nil {
			return Record{}, 0, err
		}
		rows[path] = count
	}
	rows[path]++
	record.Meta.SourceFile = sourceFile(path)
	record.Meta.SourceRow = rows[path]
	bytesWritten, err := appendRecord(path, record)
	if err != nil {
		rows[path]--
		return Record{}, 0, err
	}
	return record, bytesWritten, nil
}

func (w *Writer) resolvePath(now time.Time, record Record) string {
	path := strings.ReplaceAll(w.config.PathTemplate, "{date}", now.Format("20060102"))
	path = strings.ReplaceAll(path, "{node}", safeNodeName(w.config.Node))
	if w.config.Partitioned {
		path = filepath.Join(
			filepath.Dir(path),
			"node-"+safeNodeName(w.config.Node),
			"user-"+safeScopeName(record.Storage.UserKey),
			"token-"+safeScopeName(record.Storage.TokenKey),
			"session-"+safeSessionName(record.SessionID)+".jsonl",
		)
	}
	return filepath.Clean(path)
}

func (w *Writer) storageRoot(now time.Time) string {
	path := strings.ReplaceAll(w.config.PathTemplate, "{date}", now.Format("20060102"))
	path = strings.ReplaceAll(path, "{node}", safeNodeName(w.config.Node))
	return filepath.Clean(filepath.Dir(path))
}

func (w *Writer) report(err error) {
	if w.config.OnError != nil && err != nil {
		w.config.OnError(err)
	}
}

func (w *Writer) emit(event Event) {
	if w == nil || w.config.OnEvent == nil {
		return
	}
	if event.At.IsZero() {
		event.At = time.Now()
	}
	w.config.OnEvent(event)
}

func (w *Writer) reportEvent(eventType string, err error, dropped, bytes int64) {
	w.metrics.setLastError(eventType)
	w.report(err)
	w.emit(Event{Type: eventType, Err: err, Dropped: dropped, Bytes: bytes})
}

func (w *Writer) ReportCaptureDrop(err error) {
	if w == nil || err == nil {
		return
	}
	switch {
	case errors.Is(err, ErrSampleTooLarge):
		w.metrics.setLastError(EventSampleTooLarge)
		w.emit(Event{Type: EventSampleTooLarge, Err: err, Dropped: 1})
	case errors.Is(err, ErrInFlightLimitReached):
		w.metrics.setLastError(EventInFlightBytesExceeded)
		w.emit(Event{Type: EventInFlightBytesExceeded, Err: err, Dropped: 1})
	}
}

func (w *Writer) ReportCaptureHealthy() {
	if w == nil {
		return
	}
	w.emit(Event{Type: EventSampleTooLarge, Resolved: true})
	w.emit(Event{Type: EventInFlightBytesExceeded, Resolved: true})
	w.emit(Event{Type: EventCaptureIncomplete, Resolved: true})
}

func (w *Writer) Status() WriterStatus {
	if w == nil {
		return WriterStatus{}
	}
	jsonP50, jsonP95 := w.metrics.jsonlLatency.percentiles()
	indexP50, indexP95 := w.metrics.indexLatency.percentiles()
	lastType, lastAt := w.metrics.lastError()
	totals := w.activityTotals()
	now := time.Now()
	status := WriterStatus{
		QueueDepth: len(w.queue), QueueCapacity: cap(w.queue), NormalizedDepth: len(w.normalizedQueue),
		InFlightBytes: w.buffers.InFlightBytes(), DiskBytes: w.diskBytes.Load(),
		FreeDiskBytes: w.freeDiskBytes.Load(),
		Submitted:     w.metrics.submitted.Load(), Written: w.metrics.written.Load(),
		DroppedQueueFull:      w.metrics.droppedQueueFull.Load(),
		DroppedSampleTooLarge: w.buffers.droppedSampleTooLarge.Load(),
		DroppedInFlightLimit:  w.buffers.droppedInFlightLimit.Load(),
		BuildFailed:           w.metrics.buildFailed.Load(), JSONLWriteFailed: w.metrics.jsonlWriteFailed.Load(),
		IncompleteDropped: w.metrics.incompleteDropped.Load(),
		IndexWriteFailed:  w.metrics.indexWriteFailed.Load(), DiskLimitDropped: w.metrics.diskLimitDropped.Load(),
		DiskLowDropped:  w.metrics.diskLowDropped.Load(),
		LastMinute:      w.metrics.activity.since(now, time.Minute, totals),
		LastFiveMinutes: w.metrics.activity.since(now, 5*time.Minute, totals),
		JSONLWriteP50MS: jsonP50, JSONLWriteP95MS: jsonP95,
		IndexWriteP50MS: indexP50, IndexWriteP95MS: indexP95,
		LastErrorType: lastType, LastErrorAt: lastAt,
	}
	if w.indexQueue != nil {
		status.IndexQueueDepth = len(w.indexQueue)
		status.IndexQueueCapacity = cap(w.indexQueue)
	}
	return status
}

func (w *Writer) activityTotals() ActivityWindow {
	if w == nil {
		return ActivityWindow{}
	}
	return ActivityWindow{
		Submitted: w.metrics.submitted.Load(), Written: w.metrics.written.Load(),
		DroppedQueueFull:      w.metrics.droppedQueueFull.Load(),
		DroppedSampleTooLarge: w.buffers.droppedSampleTooLarge.Load(),
		DroppedInFlightLimit:  w.buffers.droppedInFlightLimit.Load(),
		BuildFailed:           w.metrics.buildFailed.Load(), JSONLWriteFailed: w.metrics.jsonlWriteFailed.Load(),
		IncompleteDropped: w.metrics.incompleteDropped.Load(),
		DiskLimitDropped:  w.metrics.diskLimitDropped.Load(), DiskLowDropped: w.metrics.diskLowDropped.Load(),
	}
}

func appendRecord(path string, record Record) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return 0, err
	}
	line, err := json.Marshal(record)
	if err != nil {
		return 0, err
	}
	line = append(line, '\n')
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	written, err := file.Write(line)
	if err != nil {
		return 0, err
	}
	if written != len(line) {
		return 0, io.ErrShortWrite
	}
	return int64(written), nil
}

func RecordID(fileID string, row int64) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", fileID, row)))
	return hex.EncodeToString(digest[:12])
}

func countValidLines(path string) (int64, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	var count int64
	for {
		line, readErr := reader.ReadBytes('\n')
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			if !json.Valid(trimmed) {
				return 0, fmt.Errorf("invalid JSONL record at row %d", count+1)
			}
			count++
		}
		if readErr == io.EOF {
			return count, nil
		}
		if readErr != nil {
			return 0, readErr
		}
	}
}

func recoverJSONLTail(path string) error {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	reader := bufio.NewReader(file)
	var validBytes int64
	var row int64
	for {
		line, readErr := reader.ReadBytes('\n')
		trimmed := bytes.TrimSpace(line)
		if readErr == nil {
			row++
			if len(trimmed) == 0 || !json.Valid(trimmed) {
				_ = file.Close()
				return fmt.Errorf("invalid JSONL record at row %d", row)
			}
			validBytes += int64(len(line))
			continue
		}
		if readErr != io.EOF {
			_ = file.Close()
			return readErr
		}
		if err := file.Close(); err != nil {
			return err
		}
		if len(trimmed) == 0 {
			return nil
		}
		if json.Valid(trimmed) {
			appendFile, openErr := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
			if openErr != nil {
				return openErr
			}
			_, writeErr := appendFile.Write([]byte{'\n'})
			closeErr := appendFile.Close()
			if writeErr != nil {
				return writeErr
			}
			return closeErr
		}
		corruptPath := path + ".corrupt-" + time.Now().Format("20060102-150405")
		if err := os.WriteFile(corruptPath, line, 0o600); err != nil {
			return err
		}
		return os.Truncate(path, validBytes)
	}
}

func directorySize(root string) (int64, error) {
	var size int64
	err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

var invalidNodeCharacter = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func safeNodeName(value string) string {
	value = invalidNodeCharacter.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-.")
	if value == "" {
		return "node"
	}
	return value
}

func NormalizeNode(value string) string {
	return safeNodeName(value)
}

func safeScopeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "anonymous" || value == "playground" {
		return value
	}
	if value != "" {
		for _, character := range value {
			if character < '0' || character > '9' {
				return "anonymous"
			}
		}
		return value
	}
	return "anonymous"
}

func safeSessionName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 16 {
		return "invalid-session"
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return "invalid-session"
		}
	}
	return value
}
