package service

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/datasetcapture"
)

var ErrDatasetCaptureExportEmpty = errors.New("no dataset capture records match the selection")

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
	file, err := os.CreateTemp("", "new-api-dataset-export-*.jsonl")
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
			if _, err := file.Write(line); err != nil {
				return nil, err
			}
			if _, err := file.Write([]byte{'\n'}); err != nil {
				return nil, err
			}
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
