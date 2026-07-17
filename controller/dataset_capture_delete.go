package controller

import (
	"errors"
	"net/http"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/middleware"
	"github.com/01121531/HUICHUAN-AI/model"
	"github.com/01121531/HUICHUAN-AI/service"
	"github.com/gin-gonic/gin"
)

func DeleteDatasetCaptureRecords(c *gin.Context) {
	request, filter, selection, err := parseDatasetCaptureDeleteRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := applyDatasetCaptureContentFilter(&filter, request.Filter.Content); err != nil {
		datasetCaptureQueryError(c, err)
		return
	}
	indices, err := model.ListDatasetCaptureExportIndices(filter, selection)
	if err != nil {
		datasetCaptureQueryError(c, err)
		return
	}
	if len(indices) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "no dataset capture records match the selection"})
		return
	}
	captureIDs := make([]string, 0, len(indices))
	for _, index := range indices {
		captureIDs = append(captureIDs, index.CaptureID)
	}
	results, err := service.DeleteDatasetCaptureConversations(
		middleware.DatasetCapturePathTemplate(), middleware.DatasetCaptureNode(), captureIDs,
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

func parseDatasetCaptureDeleteRequest(c *gin.Context) (
	datasetCaptureExportRequest,
	model.DatasetCaptureFilter,
	model.DatasetCaptureSelection,
	error,
) {
	request, filter, selection, err := parseDatasetCaptureExportRequest(c)
	if err != nil {
		return request, filter, selection, errors.New("invalid dataset capture delete request: " + err.Error())
	}
	return request, filter, selection, nil
}
