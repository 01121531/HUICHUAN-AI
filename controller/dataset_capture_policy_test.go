package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/01121531/HUICHUAN-AI/setting/dataset_capture_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestUpdateDatasetCapturePolicyRejectsEmptySelectedModels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	original := dataset_capture_setting.Get()
	t.Cleanup(func() { _ = dataset_capture_setting.Apply(original) })

	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/dataset-capture-policy",
		bytes.NewBufferString(`{"enabled":true,"model_mode":"selected","models":[]}`),
	)
	UpdateDatasetCapturePolicy(context)

	assert.Equal(t, http.StatusBadRequest, response.Code)
	assert.Equal(t, original, dataset_capture_setting.Get())
}

func TestGetDatasetCapturePolicyReturnsEmptyModelsArray(t *testing.T) {
	gin.SetMode(gin.TestMode)
	original := dataset_capture_setting.Get()
	t.Cleanup(func() { _ = dataset_capture_setting.Apply(original) })
	assert.NoError(t, dataset_capture_setting.Apply(dataset_capture_setting.Policy{
		ModelMode: dataset_capture_setting.ModelModeAll,
	}))

	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/dataset-capture-policy", nil)
	GetDatasetCapturePolicy(context)

	assert.Equal(t, http.StatusOK, response.Code)
	var payload struct {
		Success bool                           `json:"success"`
		Data    dataset_capture_setting.Policy `json:"data"`
	}
	assert.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
	assert.True(t, payload.Success)
	assert.Equal(t, dataset_capture_setting.CurrentVersion, payload.Data.Version)
	assert.NotNil(t, payload.Data.Models)
	assert.NotNil(t, payload.Data.UserIDs)
	assert.NotNil(t, payload.Data.TokenIDs)
	assert.Equal(t, 1024, payload.Data.Performance.QueueSize)
}
