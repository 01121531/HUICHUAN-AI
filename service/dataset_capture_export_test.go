package service

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/01121531/HUICHUAN-AI/model"
	"github.com/01121531/HUICHUAN-AI/pkg/datasetcapture"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildDatasetCaptureExportValidatesAndMergesRecords(t *testing.T) {
	directory := t.TempDir()
	template := filepath.Join(directory, "sample-{date}-{node}.jsonl")
	results := make([]datasetcapture.WriteResult, 0, 3)
	writer, err := datasetcapture.NewWriter(datasetcapture.WriterConfig{
		PathTemplate: template, Node: "export-node", Partitioned: true,
		OnWritten: func(result datasetcapture.WriteResult) error {
			results = append(results, result)
			return nil
		},
	})
	require.NoError(t, err)

	first := exportTestRecord(t, "request-user-2", "model-user-2", time.Unix(300, 0))
	first.Storage = datasetcapture.StorageScope{UserKey: "2", TokenKey: "20"}
	second := exportTestRecord(t, "request-user-1-a", "model-user-1-a", time.Unix(100, 0))
	second.Storage = datasetcapture.StorageScope{UserKey: "1", TokenKey: "10"}
	third := exportTestRecord(t, "request-user-1-b", "model-user-1-b", time.Unix(200, 0))
	third.SessionID = second.SessionID
	third.Storage = datasetcapture.StorageScope{UserKey: "1", TokenKey: "10"}
	for _, record := range []datasetcapture.Record{first, second, third} {
		require.NoError(t, writer.Submit(record))
	}
	require.NoError(t, writer.Close())
	require.Len(t, results, 3)

	indices := []model.DatasetCaptureIndex{
		model.NewDatasetCaptureIndex(results[1]),
		model.NewDatasetCaptureIndex(results[2]),
		model.NewDatasetCaptureIndex(results[0]),
	}
	export, err := BuildDatasetCaptureExport(template, "export-node", indices)
	require.NoError(t, err)
	exportPath := export.path
	t.Cleanup(func() { _ = export.Close() })

	info, err := export.File.Stat()
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	assert.Equal(t, 3, export.RecordCount)
	assert.Equal(t, 2, export.UserCount)
	assert.Positive(t, export.Bytes)

	models := make([]string, 0, 3)
	scanner := bufio.NewScanner(export.File)
	for scanner.Scan() {
		require.NoError(t, datasetcapture.ValidateJSONLine(scanner.Bytes()))
		var record datasetcapture.Record
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &record))
		models = append(models, record.Model)
	}
	require.NoError(t, scanner.Err())
	assert.Equal(t, []string{"model-user-1-a", "model-user-1-b", "model-user-2"}, models)

	require.NoError(t, export.Close())
	_, err = os.Stat(exportPath)
	assert.True(t, errors.Is(err, os.ErrNotExist))
}

func TestBuildDatasetCaptureExportRejectsEmptySelection(t *testing.T) {
	export, err := BuildDatasetCaptureExport("unused", "node", nil)
	assert.Nil(t, export)
	assert.ErrorIs(t, err, ErrDatasetCaptureExportEmpty)
}

func TestDeleteDatasetCaptureConversationsRemovesWholeSelectedFilesAndIndices(t *testing.T) {
	require.NoError(t, model.DB.AutoMigrate(&model.DatasetCaptureIndex{}))
	node := "delete-service-node"
	require.NoError(t, model.DB.Where("node = ?", node).Delete(&model.DatasetCaptureIndex{}).Error)
	t.Cleanup(func() {
		_ = model.DB.Where("node = ?", node).Delete(&model.DatasetCaptureIndex{}).Error
	})
	directory := t.TempDir()
	template := filepath.Join(directory, "sample-{date}-{node}.jsonl")
	results := make([]datasetcapture.WriteResult, 0, 3)
	writer, err := datasetcapture.NewWriter(datasetcapture.WriterConfig{
		PathTemplate: template, Node: node, Partitioned: true,
		OnWritten: func(result datasetcapture.WriteResult) error {
			results = append(results, result)
			return model.UpsertDatasetCaptureIndex(model.NewDatasetCaptureIndex(result))
		},
	})
	require.NoError(t, err)
	first := exportTestRecord(t, "delete-conversation-a", "model-a", time.Unix(100, 0))
	first.Storage = datasetcapture.StorageScope{UserKey: "11", TokenKey: "21"}
	second := exportTestRecord(t, "delete-conversation-a-next", "model-a", time.Unix(200, 0))
	second.SessionID = first.SessionID
	second.Storage = first.Storage
	remaining := exportTestRecord(t, "delete-conversation-b", "model-b", time.Unix(300, 0))
	remaining.Storage = datasetcapture.StorageScope{UserKey: "11", TokenKey: "22"}
	for _, record := range []datasetcapture.Record{first, second, remaining} {
		require.NoError(t, writer.Submit(record))
	}
	require.NoError(t, writer.Close())
	require.Len(t, results, 3)

	deleteResults, err := DeleteDatasetCaptureConversations(template, node, []string{
		results[0].CaptureID, results[1].CaptureID, "ffffffffffffffffffffffff",
	})
	require.NoError(t, err)
	require.Len(t, deleteResults, 2)
	assert.True(t, deleteResults[0].Success)
	assert.Equal(t, int64(2), deleteResults[0].DeletedRecords)
	assert.Len(t, deleteResults[0].CaptureIDs, 2)
	assert.False(t, deleteResults[1].Success)
	assert.Equal(t, "dataset capture record not found", deleteResults[1].Error)

	var indices []model.DatasetCaptureIndex
	require.NoError(t, model.DB.Where("node = ?", node).Find(&indices).Error)
	require.Len(t, indices, 1)
	assert.Equal(t, results[2].CaptureID, indices[0].CaptureID)
	files, err := datasetcapture.NewBrowser(template, node).ListFiles()
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, results[2].FileID, files[0].ID)
}

func exportTestRecord(t *testing.T, requestID, modelName string, createdAt time.Time) datasetcapture.Record {
	t.Helper()
	record, err := datasetcapture.Normalize(datasetcapture.Capture{
		Path:         "/v1/chat/completions",
		RequestBody:  []byte(`{"model":"` + modelName + `","messages":[{"role":"user","content":"hello"}]}`),
		ResponseBody: []byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`),
		RequestID:    requestID,
		HMACKey:      "export-test-key",
		CreatedAt:    createdAt,
	})
	require.NoError(t, err)
	return record
}
