package service

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/datasetcapture"
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
			return model.BackfillDatasetCaptureIndex(model.NewDatasetCaptureIndex(result))
		})
		if err != nil {
			return err
		}
		if err := model.DeleteDatasetCaptureIndicesAfterRow(file.ID, lastRow); err != nil {
			return err
		}
	}
	return model.DeleteStaleDatasetCaptureIndices(node, activeFileIDs)
}
