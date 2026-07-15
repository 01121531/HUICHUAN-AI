package datasetcapture

import (
	"bufio"
	"bytes"
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
	"time"
)

var (
	ErrWriterClosed = errors.New("dataset capture writer is closed")
	ErrQueueFull    = errors.New("dataset capture queue is full")
	captureFileMu   sync.Mutex
)

type WriterConfig struct {
	PathTemplate string
	Node         string
	QueueSize    int
	MaxDiskBytes int64
	Partitioned  bool
	OnError      func(error)
	OnWritten    func(WriteResult) error
}

type WriteResult struct {
	CaptureID string
	FileID    string
	Node      string
	Row       int64
	Bytes     int64
	Record    Record
}

type Writer struct {
	config WriterConfig
	queue  chan Record
	done   chan struct{}
	mu     sync.RWMutex
	closed bool
	once   sync.Once
}

func NewWriter(config WriterConfig) (*Writer, error) {
	if strings.TrimSpace(config.PathTemplate) == "" {
		return nil, errors.New("dataset capture path is empty")
	}
	if config.QueueSize <= 0 {
		config.QueueSize = 128
	}
	if config.Node == "" {
		config.Node = "node"
	}
	writer := &Writer{
		config: config,
		queue:  make(chan Record, config.QueueSize),
		done:   make(chan struct{}),
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
	go writer.run()
	return writer, nil
}

func (w *Writer) Submit(record Record) error {
	if w == nil {
		return ErrWriterClosed
	}
	if err := Validate(record); err != nil {
		return err
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		return ErrWriterClosed
	}
	select {
	case w.queue <- record:
		return nil
	default:
		return ErrQueueFull
	}
}

func (w *Writer) Close() error {
	if w == nil {
		return nil
	}
	w.once.Do(func() {
		w.mu.Lock()
		w.closed = true
		close(w.queue)
		w.mu.Unlock()
	})
	<-w.done
	return nil
}

func (w *Writer) run() {
	defer close(w.done)
	rows := map[string]int64{}
	for record := range w.queue {
		now := time.Now()
		path := w.resolvePath(now, record)
		if w.config.MaxDiskBytes > 0 {
			size, err := directorySize(w.storageRoot(now))
			if err != nil {
				w.report(err)
				continue
			}
			if size >= w.config.MaxDiskBytes {
				w.report(fmt.Errorf("dataset capture disk limit reached: %d bytes", size))
				continue
			}
		}
		captureFileMu.Lock()
		writtenRecord, bytesWritten, err := appendQueuedRecord(path, record, rows)
		if err == nil && w.config.OnWritten != nil {
			fileID := captureFileID(path)
			err = w.config.OnWritten(WriteResult{
				CaptureID: RecordID(fileID, writtenRecord.Meta.SourceRow),
				FileID:    fileID,
				Node:      safeNodeName(w.config.Node),
				Row:       writtenRecord.Meta.SourceRow,
				Bytes:     bytesWritten,
				Record:    writtenRecord,
			})
		}
		captureFileMu.Unlock()
		w.report(err)
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
