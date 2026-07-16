package controller

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/middleware"
	"github.com/01121531/HUICHUAN-AI/service"
	"github.com/gin-gonic/gin"
)

type datasetCaptureDeleteRequest struct {
	CaptureIDs []string `json:"capture_ids"`
}

func DeleteDatasetCaptureRecords(c *gin.Context) {
	request, err := parseDatasetCaptureDeleteRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	results, err := service.DeleteDatasetCaptureConversations(
		middleware.DatasetCapturePathTemplate(), middleware.DatasetCaptureNode(), request.CaptureIDs,
	)
	if err != nil {
		common.SysError("dataset capture batch delete failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to delete dataset captures"})
		return
	}
	deletedConversations := 0
	var deletedRecords int64
	for _, result := range results {
		if !result.Success {
			if result.Cause != nil {
				common.SysError("dataset capture conversation delete failed: " + result.Cause.Error())
			}
			continue
		}
		deletedConversations++
		deletedRecords += result.DeletedRecords
		recordDatasetCaptureAudit(c, "dataset_capture.delete", map[string]interface{}{
			"scope": "conversation", "session_id": result.SessionID,
			"selected_records": len(result.CaptureIDs), "deleted_records": result.DeletedRecords,
			"node": middleware.DatasetCaptureNode(),
		})
	}
	c.JSON(http.StatusOK, gin.H{"success": deletedConversations == len(results), "data": gin.H{
		"items": results, "deleted_conversations": deletedConversations, "deleted_records": deletedRecords,
	}})
}

func parseDatasetCaptureDeleteRequest(c *gin.Context) (datasetCaptureDeleteRequest, error) {
	var request datasetCaptureDeleteRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, errors.New("invalid dataset capture delete request")
	}
	if err := ensureJSONBodyEnd(decoder); err != nil {
		return request, errors.New("invalid dataset capture delete request")
	}
	if len(request.CaptureIDs) == 0 {
		return request, errors.New("select at least one capture record")
	}
	if len(request.CaptureIDs) > maxDatasetCaptureExportSelection {
		return request, errors.New("dataset capture delete selection is too large")
	}
	captureIDs, err := uniqueCaptureIDs(request.CaptureIDs)
	if err != nil {
		return request, err
	}
	request.CaptureIDs = captureIDs
	return request, nil
}
