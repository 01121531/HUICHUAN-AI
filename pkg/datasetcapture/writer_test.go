package datasetcapture

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWriterConcurrentJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.jsonl")
	writer, err := NewWriter(WriterConfig{PathTemplate: path, Node: "test", QueueSize: 64})
	if err != nil {
		t.Fatal(err)
	}
	request := []byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}]}`)
	response := []byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	record, err := Normalize(testCapture("/v1/chat/completions", request, response))
	if err != nil {
		t.Fatal(err)
	}
	const count = 40
	var wg sync.WaitGroup
	errCh := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- writer.Submit(record)
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	rows := 0
	for scanner.Scan() {
		var decoded Record
		if err := json.Unmarshal(scanner.Bytes(), &decoded); err != nil {
			t.Fatal(err)
		}
		rows++
		if decoded.Meta.SourceRow != int64(rows) || decoded.Meta.SourceFile != "sample.jsonl" {
			t.Fatalf("unexpected source metadata at row %d: %#v", rows, decoded.Meta)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if rows != count {
		t.Fatalf("rows=%d want=%d", rows, count)
	}
}

func TestWriterReportsCommittedRecordMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.jsonl")
	written := make(chan WriteResult, 1)
	writer, err := NewWriter(WriterConfig{
		PathTemplate: path,
		Node:         "test-node",
		OnWritten: func(result WriteResult) error {
			written <- result
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := Normalize(testCapture(
		"/v1/chat/completions",
		[]byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}]}`),
		[]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`),
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Submit(record); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	result := <-written
	if len(result.CaptureID) != 24 || len(result.FileID) != 24 {
		t.Fatalf("unexpected opaque ids: %#v", result)
	}
	if result.Node != "test-node" || result.Row != 1 || result.Bytes <= 0 {
		t.Fatalf("unexpected write metadata: %#v", result)
	}
	if result.Record.Meta.SourceRow != 1 || result.Record.Meta.SourceFile != "sample.jsonl" {
		t.Fatalf("callback did not receive committed source metadata: %#v", result.Record.Meta)
	}
}

func TestWriterIndexCallbackDoesNotHoldConversationFileLock(t *testing.T) {
	directory := t.TempDir()
	template := filepath.Join(directory, "sample-{date}-{node}.jsonl")
	callbackStarted := make(chan WriteResult, 1)
	releaseCallback := make(chan struct{})
	writer, err := NewWriter(WriterConfig{
		PathTemplate: template, Node: "lock-node", Partitioned: true,
		IndexBatchSize: 1,
		OnWritten: func(result WriteResult) error {
			callbackStarted <- result
			<-releaseCallback
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := Normalize(testCapture(
		"/v1/chat/completions",
		[]byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}]}`),
		[]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`),
	))
	if err != nil {
		t.Fatal(err)
	}
	record.Storage = StorageScope{UserKey: "1", TokenKey: "2"}
	if err := writer.Submit(record); err != nil {
		t.Fatal(err)
	}
	result := <-callbackStarted
	deleteDone := make(chan error, 1)
	go func() {
		_, err := NewBrowser(template, "lock-node").Delete(result.FileID)
		deleteDone <- err
	}()
	select {
	case err := <-deleteDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("delete remained blocked by the index callback")
	}
	close(releaseCallback)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWriterRecoversCorruptTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.jsonl")
	if err := os.WriteFile(path, []byte("{\"valid\":true}\n{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	writer, err := NewWriter(WriterConfig{PathTemplate: path})
	if err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{\"valid\":true}\n" {
		t.Fatalf("unexpected recovered file: %q", data)
	}
	matches, err := filepath.Glob(path + ".corrupt-*")
	if err != nil || len(matches) != 1 {
		t.Fatalf("corrupt backups=%v err=%v", matches, err)
	}
}

func TestWriterConcurrentSubmitAndClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.jsonl")
	writer, err := NewWriter(WriterConfig{PathTemplate: path, QueueSize: 8})
	if err != nil {
		t.Fatal(err)
	}
	request := []byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}]}`)
	response := []byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	record, err := Normalize(testCapture("/v1/chat/completions", request, response))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				err := writer.Submit(record)
				if err == ErrWriterClosed {
					return
				}
			}
		}()
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
}

func TestWriterPartitionsByUserTokenAndConversation(t *testing.T) {
	dir := t.TempDir()
	writer, err := NewWriter(WriterConfig{
		PathTemplate: filepath.Join(dir, "sample-{date}-{node}.jsonl"),
		Node:         "test", QueueSize: 8, Partitioned: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := []byte(`{"model":"gpt-test","conversation_id":"conversation-a","messages":[{"role":"user","content":"hello"}]}`)
	response := []byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	capture := testCapture("/v1/chat/completions", request, response)
	capture.TokenID = "token-7"
	record, err := Normalize(capture)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Submit(record); err != nil {
		t.Fatal(err)
	}
	if err := writer.Submit(record); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "node-test", "user-"+record.Storage.UserKey, "token-"+record.Storage.TokenKey, "session-"+record.SessionID+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		rows++
		var decoded map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &decoded); err != nil {
			t.Fatal(err)
		}
		if len(decoded) != 11 {
			t.Fatalf("partition metadata leaked into sample: %#v", decoded)
		}
	}
	if rows != 2 {
		t.Fatalf("rows=%d want=2", rows)
	}
}

func TestWriterRestartsRowsAfterConversationFileDeletion(t *testing.T) {
	dir := t.TempDir()
	template := filepath.Join(dir, "sample-{date}-{node}.jsonl")
	writer, err := NewWriter(WriterConfig{PathTemplate: template, Node: "test", QueueSize: 8, Partitioned: true})
	if err != nil {
		t.Fatal(err)
	}
	request := []byte(`{"model":"gpt-test","conversation_id":"conversation-a","messages":[{"role":"user","content":"hello"}]}`)
	response := []byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	capture := testCapture("/v1/chat/completions", request, response)
	capture.TokenID = "7"
	record, err := Normalize(capture)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Submit(record); err != nil {
		t.Fatal(err)
	}
	browser := NewBrowser(template, "test")
	var file CaptureFile
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		files, listErr := browser.ListFiles()
		if listErr != nil {
			t.Fatal(listErr)
		}
		if len(files) == 1 {
			file = files[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if file.ID == "" {
		t.Fatal("first conversation file was not written")
	}
	if _, err := browser.Delete(file.ID); err != nil {
		t.Fatal(err)
	}
	if err := writer.Submit(record); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	files, err := browser.ListFiles()
	if err != nil || len(files) != 1 {
		t.Fatalf("recreated files=%#v err=%v", files, err)
	}
	page, err := browser.Records(files[0].ID, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalRows != 1 {
		t.Fatalf("rows=%d want=1", page.TotalRows)
	}
	var recreated Record
	if err := json.Unmarshal(page.Records[0], &recreated); err != nil {
		t.Fatal(err)
	}
	if recreated.Meta.SourceRow != 1 {
		t.Fatalf("source row=%d want=1", recreated.Meta.SourceRow)
	}
}

func TestWriterBuildsAndValidatesSessionAsynchronously(t *testing.T) {
	output := filepath.Join(t.TempDir(), "sample.jsonl")
	errorsReported := make(chan error, 1)
	writer, err := NewWriter(WriterConfig{
		PathTemplate: output,
		QueueSize:    1,
		OnError: func(err error) {
			errorsReported <- err
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	session := NewSession(testCapture(
		"/v1/chat/completions",
		[]byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}]}`),
		nil,
	))
	session.BeginAttempt("gpt-test", "route")
	session.SucceedAttempt()

	if err := writer.SubmitSession(session); err != nil {
		t.Fatalf("SubmitSession performed synchronous validation: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case reported := <-errorsReported:
		if !errors.Is(reported, ErrIncompleteCapture) {
			t.Fatalf("reported error = %v", reported)
		}
	default:
		t.Fatal("worker did not report the incomplete session")
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid session created output: %v", err)
	}
}

func TestWriterTracksDiskBytesWithoutPerRecordDirectoryScan(t *testing.T) {
	output := filepath.Join(t.TempDir(), "sample.jsonl")
	writer, err := NewWriter(WriterConfig{PathTemplate: output, Workers: 2, QueueSize: 8})
	if err != nil {
		t.Fatal(err)
	}
	record, err := Normalize(testCapture(
		"/v1/chat/completions",
		[]byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}]}`),
		[]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`),
	))
	if err != nil {
		t.Fatal(err)
	}
	for range 4 {
		if err := writer.Submit(record); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if writer.DiskBytes() != info.Size() {
		t.Fatalf("tracked disk bytes=%d file size=%d", writer.DiskBytes(), info.Size())
	}
}

func TestWriterDiskLimitDropsWholeLaterRecord(t *testing.T) {
	output := filepath.Join(t.TempDir(), "sample.jsonl")
	errorsReported := make(chan error, 2)
	writer, err := NewWriter(WriterConfig{
		PathTemplate: output,
		MaxDiskBytes: 1,
		OnError: func(err error) {
			errorsReported <- err
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := Normalize(testCapture(
		"/v1/chat/completions",
		[]byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}]}`),
		[]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`),
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Submit(record); err != nil {
		t.Fatal(err)
	}
	if err := writer.Submit(record); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if lines := bytes.Count(data, []byte{'\n'}); lines != 1 {
		t.Fatalf("JSONL rows=%d, want 1", lines)
	}
	select {
	case reported := <-errorsReported:
		if !strings.Contains(reported.Error(), "disk limit reached") {
			t.Fatalf("reported error=%v", reported)
		}
	default:
		t.Fatal("disk limit was not reported")
	}
}

func TestWriterStatusCountsSubmittedWrittenAndDropped(t *testing.T) {
	output := filepath.Join(t.TempDir(), "sample.jsonl")
	writer, err := NewWriter(WriterConfig{PathTemplate: output, QueueSize: 16})
	if err != nil {
		t.Fatal(err)
	}
	record, err := Normalize(testCapture(
		"/v1/chat/completions",
		[]byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}]}`),
		[]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`),
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Submit(record); err != nil {
		t.Fatal(err)
	}
	buffer := writer.NewResponseBuffer()
	writer.config.MaxSampleBytes = 1
	buffer.maxBytes = 1
	dropErr := buffer.TryAppendString("too large")
	writer.ReportCaptureDrop(dropErr)
	buffer.Release()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	status := writer.Status()
	if status.Submitted != 1 || status.Written != 1 || status.DroppedSampleTooLarge != 1 {
		t.Fatalf("unexpected status: %#v", status)
	}
	if status.LastErrorType != EventSampleTooLarge {
		t.Fatalf("last error type=%q", status.LastErrorType)
	}
}

func TestWriterMinimumFreeDiskDropsWholeRecord(t *testing.T) {
	output := filepath.Join(t.TempDir(), "sample.jsonl")
	events := make(chan Event, 1)
	writer, err := NewWriter(WriterConfig{
		PathTemplate: output, MinFreeDiskBytes: int64(^uint64(0) >> 1),
		OnEvent: func(event Event) {
			if !event.Resolved {
				events <- event
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := Normalize(testCapture(
		"/v1/chat/completions",
		[]byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}]}`),
		[]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`),
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Submit(record); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("low disk record created output: %v", err)
	}
	select {
	case event := <-events:
		if event.Type != EventDiskLow {
			t.Fatalf("event type=%q", event.Type)
		}
	default:
		t.Fatal("low disk event was not emitted")
	}
}

func TestWriterJSONLFailureDropsWholeRecordAndEmitsEvent(t *testing.T) {
	output := filepath.Join(t.TempDir(), "sample.jsonl")
	events := make(chan Event, 1)
	writer, err := NewWriter(WriterConfig{
		PathTemplate: output,
		OnEvent: func(event Event) {
			if !event.Resolved && event.Type == EventJSONLWriteFailed {
				events <- event
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(output, 0o700); err != nil {
		t.Fatal(err)
	}
	record, err := Normalize(testCapture(
		"/v1/chat/completions",
		[]byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}]}`),
		[]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`),
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Submit(record); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if status := writer.Status(); status.JSONLWriteFailed != 1 || status.Written != 0 {
		t.Fatalf("unexpected status after JSONL failure: %#v", status)
	}
	select {
	case event := <-events:
		if event.Err == nil {
			t.Fatal("JSONL failure event did not include a sanitized error source")
		}
	default:
		t.Fatal("JSONL failure event was not emitted")
	}
	entries, err := os.ReadDir(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("JSONL failure left partial files: %v", entries)
	}
}

func TestSafelyBuildRecordRecoversWorkerPanic(t *testing.T) {
	_, err := safelyBuildRecord(func() (Record, error) {
		panic("broken normalizer")
	})
	if !errors.Is(err, ErrWorkerPanic) {
		t.Fatalf("error=%v, want ErrWorkerPanic", err)
	}
}

func TestWriterRetriesIndexAndKeepsJSONL(t *testing.T) {
	output := filepath.Join(t.TempDir(), "sample.jsonl")
	var attempts atomic.Int64
	writer, err := NewWriter(WriterConfig{
		PathTemplate: output, IndexBatchSize: 1,
		OnWrittenBatch: func(_ []WriteResult) error {
			if attempts.Add(1) < 3 {
				return errors.New("temporary database failure")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := Normalize(testCapture(
		"/v1/chat/completions",
		[]byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}]}`),
		[]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`),
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Submit(record); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 3 {
		t.Fatalf("index attempts=%d, want 3", attempts.Load())
	}
	if writer.Status().IndexWriteFailed != 0 {
		t.Fatalf("recovered index write counted as failure: %#v", writer.Status())
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("JSONL was not retained: %v", err)
	}
}

func TestWriterCloseContextTimesOutAndCanFinish(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.jsonl")
	indexStarted := make(chan struct{})
	releaseIndex := make(chan struct{})
	writer, err := NewWriter(WriterConfig{
		PathTemplate: path,
		QueueSize:    4,
		OnWrittenBatch: func([]WriteResult) error {
			close(indexStarted)
			<-releaseIndex
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := Normalize(testCapture(
		"/v1/chat/completions",
		[]byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}]}`),
		[]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`),
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Submit(record); err != nil {
		t.Fatal(err)
	}
	select {
	case <-indexStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("index callback did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := writer.CloseContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CloseContext error=%v", err)
	}
	close(releaseIndex)
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer finishCancel()
	if err := writer.CloseContext(finishCtx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("JSONL was not retained after delayed shutdown: %v", err)
	}
}
