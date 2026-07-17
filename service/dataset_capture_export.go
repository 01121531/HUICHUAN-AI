package service

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"time"

	"github.com/01121531/HUICHUAN-AI/model"
	"github.com/01121531/HUICHUAN-AI/pkg/datasetcapture"
	"github.com/01121531/HUICHUAN-AI/setting/dataset_capture_setting"
)

var (
	ErrDatasetCaptureExportEmpty = errors.New("no dataset capture records match the selection")
	ErrDatasetCaptureExportBusy  = errors.New("dataset capture export concurrency limit reached")
	datasetCaptureExportsActive  atomic.Int64
)

type datasetCaptureExportLimiter struct {
	bytesPerSecond int64
	startedAt      time.Time
	written        int64
	now            func() time.Time
	sleep          func(time.Duration)
}

func newDatasetCaptureExportLimiter(megabytesPerSecond int) *datasetCaptureExportLimiter {
	return &datasetCaptureExportLimiter{
		bytesPerSecond: int64(megabytesPerSecond) << 20,
		startedAt:      time.Now(), now: time.Now, sleep: time.Sleep,
	}
}

func (limiter *datasetCaptureExportLimiter) wait(written int) {
	if limiter == nil || limiter.bytesPerSecond <= 0 || written <= 0 {
		return
	}
	limiter.written += int64(written)
	expected := time.Duration(float64(limiter.written) / float64(limiter.bytesPerSecond) * float64(time.Second))
	if delay := limiter.startedAt.Add(expected).Sub(limiter.now()); delay > 0 {
		limiter.sleep(delay)
	}
}

type DatasetCaptureExport struct {
	File        *os.File
	Filename    string
	RecordCount int
	UserCount   int
	Bytes       int64
	path        string
}

func BuildDatasetCaptureExport(pathTemplate, node string, indices []model.DatasetCaptureIndex) (*DatasetCaptureExport, error) {
	if len(indices) == 0 {
		return nil, ErrDatasetCaptureExportEmpty
	}
	if !tryAcquireDatasetCaptureExport(dataset_capture_setting.Get().Performance.ExportConcurrency) {
		return nil, ErrDatasetCaptureExportBusy
	}
	defer datasetCaptureExportsActive.Add(-1)
	file, err := os.CreateTemp("", "huichuan-dataset-export-*.jsonl")
	if err != nil {
		return nil, err
	}
	path := file.Name()
	keep := false
	defer func() {
		if !keep {
			_ = file.Close()
			_ = os.Remove(path)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return nil, err
	}

	policy := dataset_capture_setting.Get()
	limiter := newDatasetCaptureExportLimiter(policy.Performance.ExportReadMBps)
	browser := datasetcapture.NewBrowser(pathTemplate, node)
	for start := 0; start < len(indices); {
		end := start + 1
		for end < len(indices) && indices[end].FileID == indices[start].FileID {
			end++
		}
		locators := make([]datasetcapture.RecordLocator, 0, end-start)
		for _, index := range indices[start:end] {
			locators = append(locators, datasetcapture.RecordLocator{
				Key: index.CaptureID, FileID: index.FileID, Row: index.Row,
			})
		}
		records, err := browser.ReadRecords(locators)
		if err != nil {
			return nil, err
		}
		for _, index := range indices[start:end] {
			line := records[index.CaptureID]
			if err := datasetcapture.ValidateJSONLine(line); err != nil {
				return nil, fmt.Errorf("capture %s: %w", index.CaptureID, err)
			}
			written, err := file.Write(line)
			if err != nil {
				return nil, err
			}
			newlineWritten, err := file.Write([]byte{'\n'})
			if err != nil {
				return nil, err
			}
			limiter.wait(written + newlineWritten)
		}
		start = end
	}
	if err := file.Sync(); err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	users := make(map[int]struct{})
	for _, index := range indices {
		users[index.UserID] = struct{}{}
	}
	keep = true
	return &DatasetCaptureExport{
		File: file, path: path,
		Filename:    fmt.Sprintf("dataset-capture-%s-%d-records.jsonl", time.Now().UTC().Format("20060102-150405"), len(indices)),
		RecordCount: len(indices), UserCount: len(users), Bytes: info.Size(),
	}, nil
}

func tryAcquireDatasetCaptureExport(limit int) bool {
	if limit < 1 {
		limit = 1
	}
	for {
		current := datasetCaptureExportsActive.Load()
		if current >= int64(limit) {
			return false
		}
		if datasetCaptureExportsActive.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

func (export *DatasetCaptureExport) Close() error {
	if export == nil {
		return nil
	}
	var closeErr error
	if export.File != nil {
		closeErr = export.File.Close()
		export.File = nil
	}
	removeErr := os.Remove(export.path)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	if closeErr != nil {
		return closeErr
	}
	return removeErr
}
