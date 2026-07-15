package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting/dataset_capture_setting"
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
