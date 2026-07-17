package service

import (
	"encoding/json"

	"github.com/01121531/HUICHUAN-AI/model"
	"github.com/01121531/HUICHUAN-AI/pkg/datasetcapture"
)

func ReconcileDatasetCaptureIndex(pathTemplate, node string) error {
	browser := datasetcapture.NewBrowser(pathTemplate, node)
	files, err := browser.ListFiles()
	if err != nil {
		return err
	}
	activeFileIDs := make([]string, 0, len(files))
	for _, file := range files {
		activeFileIDs = append(activeFileIDs, file.ID)
		var lastRow int64
		batch := make([]model.DatasetCaptureIndex, 0, 50)
		flush := func() error {
			if len(batch) == 0 {
				return nil
			}
			if err := model.BackfillDatasetCaptureIndices(batch); err != nil {
				return err
			}
			batch = batch[:0]
			return nil
		}
		err := browser.ScanRecords(file.ID, func(scanned datasetcapture.ScannedRecord) error {
			var record datasetcapture.Record
			if err := json.Unmarshal(scanned.Raw, &record); err != nil {
				return err
			}
			record.Storage.UserKey = file.UserKey
			record.Storage.TokenKey = file.TokenKey
			result := datasetcapture.WriteResult{
				CaptureID: datasetcapture.RecordID(file.ID, scanned.Row),
				FileID:    file.ID,
				Node:      node,
				Row:       scanned.Row,
				Bytes:     scanned.Size,
				Record:    record,
			}
			lastRow = scanned.Row
			batch = append(batch, model.NewDatasetCaptureIndex(result))
			if len(batch) == cap(batch) {
				return flush()
			}
			return nil
		})
		if err != nil {
			return err
		}
		if err := flush(); err != nil {
			return err
		}
		if err := model.DeleteDatasetCaptureIndicesAfterRow(file.ID, lastRow); err != nil {
			return err
		}
	}
	return model.DeleteStaleDatasetCaptureIndices(node, activeFileIDs)
}
