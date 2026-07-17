package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDatasetCaptureExportRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("NODE_NAME", "export-controller-node")
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/dataset-captures/export", bytes.NewBufferString(`{
		"user_ids":[2,2],
		"capture_ids":["ABCDEFABCDEFABCDEFABCDEF","abcdefabcdefabcdefabcdef"],
		"filter":{"models":["model-a","model-a"],"token_ids":[3],"groups":["vip"],"channel_ids":[4]}
	}`))

	_, filter, selection, err := parseDatasetCaptureExportRequest(context)
	require.NoError(t, err)
	assert.Equal(t, []int{2}, selection.UserIDs)
	assert.Equal(t, []string{"abcdefabcdefabcdefabcdef"}, selection.CaptureIDs)
	assert.Equal(t, []string{"model-a"}, filter.Models)
	assert.Equal(t, "export-controller-node", filter.Node)
}

func TestParseDatasetCaptureExportRequestRejectsAmbiguousSelection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/dataset-captures/export", bytes.NewBufferString(`{
		"all_filtered":true,
		"user_ids":[2],
		"filter":{}
	}`))

	_, _, _, err := parseDatasetCaptureExportRequest(context)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "all_filtered")
}

func TestParseDatasetCaptureDeleteRequestDeduplicatesCaptureIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("NODE_NAME", "delete-controller-node")
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodDelete, "/api/dataset-captures/records/batch", bytes.NewBufferString(`{
		"capture_ids":["ABCDEFABCDEFABCDEFABCDEF","abcdefabcdefabcdefabcdef"]
	}`))

	request, filter, selection, err := parseDatasetCaptureDeleteRequest(context)
	require.NoError(t, err)
	assert.Equal(t, []string{"abcdefabcdefabcdefabcdef"}, request.CaptureIDs)
	assert.Equal(t, []string{"abcdefabcdefabcdefabcdef"}, selection.CaptureIDs)
	assert.Equal(t, "delete-controller-node", filter.Node)
}

func TestParseDatasetCaptureDeleteRequestSupportsUserSelection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("NODE_NAME", "delete-controller-node")
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodDelete, "/api/dataset-captures/records/batch", bytes.NewBufferString(`{
		"user_ids":[7,7],
		"filter":{"models":["gpt-5.2"]}
	}`))

	_, filter, selection, err := parseDatasetCaptureDeleteRequest(context)
	require.NoError(t, err)
	assert.Equal(t, []int{7}, selection.UserIDs)
	assert.Equal(t, []string{"gpt-5.2"}, filter.Models)
	assert.Equal(t, "delete-controller-node", filter.Node)
}
