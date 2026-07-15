package service

import (
	"sort"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/datasetcapture"
)

type DatasetCaptureDeleteResult struct {
	CaptureIDs     []string `json:"capture_ids"`
	FileID         string   `json:"file_id,omitempty"`
	SessionID      string   `json:"session_id,omitempty"`
	DeletedRecords int64    `json:"deleted_records"`
	Success        bool     `json:"success"`
	Error          string   `json:"error,omitempty"`
	Cause          error    `json:"-"`
}

func DeleteDatasetCaptureConversations(pathTemplate, node string, captureIDs []string) ([]DatasetCaptureDeleteResult, error) {
	indices, err := model.ListDatasetCaptureIndicesByCaptureIDs(node, captureIDs)
	if err != nil {
		return nil, err
	}
	selected := make(map[string]struct{}, len(captureIDs))
	for _, captureID := range captureIDs {
		selected[captureID] = struct{}{}
	}
	type conversation struct {
		fileID     string
		sessionID  string
		captureIDs []string
	}
	byFile := make(map[string]*conversation)
	for _, index := range indices {
		delete(selected, index.CaptureID)
		item := byFile[index.FileID]
		if item == nil {
			item = &conversation{fileID: index.FileID, sessionID: index.SessionID}
			byFile[index.FileID] = item
		}
		item.captureIDs = append(item.captureIDs, index.CaptureID)
	}
	files := make([]string, 0, len(byFile))
	for fileID := range byFile {
		files = append(files, fileID)
	}
	sort.Strings(files)
	results := make([]DatasetCaptureDeleteResult, 0, len(files)+len(selected))
	browser := datasetcapture.NewBrowser(pathTemplate, node)
	for _, fileID := range files {
		item := byFile[fileID]
		var deletedRecords int64
		_, deleteErr := browser.DeleteWithCallback(fileID, func(_ datasetcapture.CaptureFile) error {
			var err error
			deletedRecords, err = model.DeleteDatasetCaptureIndicesByFile(node, fileID)
			return err
		})
		result := DatasetCaptureDeleteResult{
			CaptureIDs: item.captureIDs, FileID: fileID, SessionID: item.sessionID,
			DeletedRecords: deletedRecords, Success: deleteErr == nil,
		}
		if deleteErr != nil {
			result.Error = "failed to delete dataset capture conversation"
			result.Cause = deleteErr
		}
		results = append(results, result)
	}
	missing := make([]string, 0, len(selected))
	for captureID := range selected {
		missing = append(missing, captureID)
	}
	sort.Strings(missing)
	for _, captureID := range missing {
		results = append(results, DatasetCaptureDeleteResult{
			CaptureIDs: []string{captureID}, Success: false, Error: "dataset capture record not found",
		})
	}
	return results, nil
}
