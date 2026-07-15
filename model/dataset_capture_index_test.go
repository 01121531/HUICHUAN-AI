package model

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/datasetcapture"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDatasetCaptureIndexFromWriteResult(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&DatasetCaptureIndex{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&DatasetCaptureIndex{}).Error)
	t.Cleanup(func() {
		_ = DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&DatasetCaptureIndex{}).Error
	})
	createdAt := "2026-07-15T12:30:45Z"
	result := datasetcapture.WriteResult{
		CaptureID: "0123456789abcdef01234567",
		FileID:    "abcdef0123456789abcdef01",
		Node:      "node-a",
		Row:       3,
		Bytes:     512,
		Record: datasetcapture.Record{
			SessionID: "0123456789abcdef",
			Model:     "upstream-model",
			CreatedAt: &createdAt,
			Storage: datasetcapture.StorageScope{
				UserKey:        "12",
				TokenKey:       "34",
				UserGroup:      "vip",
				RequestedModel: "site-model",
				ChannelID:      56,
			},
		},
	}

	index := NewDatasetCaptureIndex(result)
	require.NoError(t, UpsertDatasetCaptureIndex(index))
	var stored DatasetCaptureIndex
	require.NoError(t, DB.Where("capture_id = ?", result.CaptureID).Take(&stored).Error)
	assert.Equal(t, 12, stored.UserID)
	assert.Equal(t, 34, stored.TokenID)
	assert.Equal(t, "vip", stored.UserGroup)
	assert.Equal(t, "site-model", stored.RequestedModel)
	assert.Equal(t, "upstream-model", stored.EffectiveModel)
	assert.Equal(t, 56, stored.ChannelID)
	assert.Equal(t, int64(512), stored.RecordSize)
}
