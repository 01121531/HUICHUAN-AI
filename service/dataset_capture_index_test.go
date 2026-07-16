package service

import (
	"path/filepath"
	"testing"

	"github.com/01121531/HUICHUAN-AI/model"
	"github.com/01121531/HUICHUAN-AI/pkg/datasetcapture"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestReconcileDatasetCaptureIndexAndContentSearch(t *testing.T) {
	require.NoError(t, model.DB.AutoMigrate(&model.DatasetCaptureIndex{}))
	require.NoError(t, model.DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.DatasetCaptureIndex{}).Error)
	t.Cleanup(func() {
		_ = model.DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.DatasetCaptureIndex{}).Error
	})
	directory := t.TempDir()
	template := filepath.Join(directory, "sample-{date}-{node}.jsonl")
	writer, err := datasetcapture.NewWriter(datasetcapture.WriterConfig{
		PathTemplate: template, Node: "reconcile-node", Partitioned: true,
	})
	require.NoError(t, err)
	capture := datasetcapture.Capture{
		Path:         "/v1/chat/completions",
		RequestBody:  []byte(`{"model":"model-a","messages":[{"role":"user","content":"find-the-needle"}]}`),
		ResponseBody: []byte(`{"choices":[{"message":{"content":"answer"},"finish_reason":"stop"}]}`),
		UserID:       "77", TokenID: "88", HMACKey: "test-key",
	}
	record, err := datasetcapture.Normalize(capture)
	require.NoError(t, err)
	require.NoError(t, writer.Submit(record))
	require.NoError(t, writer.Close())

	require.NoError(t, model.DB.Create(&model.DatasetCaptureIndex{
		CaptureID: "stale-capture-index-001", Node: "reconcile-node", FileID: "stale-file-index-00001",
		Row: 1, TokenScope: "1", EffectiveModel: "stale", SessionID: "0000000000000000",
	}).Error)
	require.NoError(t, ReconcileDatasetCaptureIndex(template, "reconcile-node"))

	var indices []model.DatasetCaptureIndex
	require.NoError(t, model.DB.Where("node = ?", "reconcile-node").Find(&indices).Error)
	require.Len(t, indices, 1)
	assert.Equal(t, 77, indices[0].UserID)
	assert.Equal(t, 88, indices[0].TokenID)
	assert.Equal(t, "model-a", indices[0].EffectiveModel)

	require.NoError(t, model.DB.Model(&model.DatasetCaptureIndex{}).
		Where("capture_id = ?", indices[0].CaptureID).
		Updates(map[string]any{"user_group": "vip", "requested_model": "site-model", "channel_id": 9}).Error)
	require.NoError(t, ReconcileDatasetCaptureIndex(template, "reconcile-node"))
	var preserved model.DatasetCaptureIndex
	require.NoError(t, model.DB.Where("capture_id = ?", indices[0].CaptureID).Take(&preserved).Error)
	assert.Equal(t, "vip", preserved.UserGroup)
	assert.Equal(t, "site-model", preserved.RequestedModel)
	assert.Equal(t, 9, preserved.ChannelID)

	matches, err := MatchDatasetCaptureContent(template, "reconcile-node", model.DatasetCaptureFilter{Node: "reconcile-node"}, "NEEDLE")
	require.NoError(t, err)
	assert.Equal(t, []string{preserved.CaptureID}, matches)
}
