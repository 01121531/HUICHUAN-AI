package controller

import (
	"net/http"
	"strings"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/middleware"
	"github.com/01121531/HUICHUAN-AI/model"
	"github.com/gin-gonic/gin"
)

func beginDatasetCaptureAccessAudit(c *gin.Context, input model.DatasetCaptureAccessAuditInput) (string, error) {
	input.OperatorUserID = c.GetInt("id")
	input.OperatorUsername = c.GetString("username")
	input.OperatorRole = c.GetInt("role")
	input.AuthMethod = auditAuthMethod(c)
	input.IP = c.ClientIP()
	input.Node = middleware.DatasetCaptureNode()
	return model.BeginDatasetCaptureAccessAudit(input)
}

func completeDatasetCaptureAccessAudit(eventID, outcome string) {
	if err := model.CompleteDatasetCaptureAccessAudit(eventID, outcome); err != nil {
		common.SysError("dataset capture access audit completion failed: " + err.Error())
	}
}

func ListDatasetCaptureAccessAudits(c *gin.Context) {
	page, pageSize := datasetCapturePagination(c)
	action := strings.TrimSpace(c.Query("action"))
	if action != "" && action != model.DatasetCaptureAuditActionView && action != model.DatasetCaptureAuditActionDownload {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid dataset capture audit action"})
		return
	}
	outcome := strings.TrimSpace(c.Query("outcome"))
	if outcome != "" && outcome != model.DatasetCaptureAuditOutcomePrepared && outcome != model.DatasetCaptureAuditOutcomeDelivered && outcome != model.DatasetCaptureAuditOutcomeFailed {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid dataset capture audit outcome"})
		return
	}
	startTime, err := optionalInt64Query(c, "start_time")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	endTime, err := optionalInt64Query(c, "end_time")
	if err != nil || (startTime > 0 && endTime > 0 && startTime > endTime) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid dataset capture audit time range"})
		return
	}
	admin := strings.TrimSpace(c.Query("admin"))
	if len(admin) > maxDatasetCaptureSearchLength {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "admin search is too long"})
		return
	}
	entries, total, err := model.ListDatasetCaptureAccessAudits(model.DatasetCaptureAccessAuditFilter{
		Action: action, Admin: admin, Outcome: outcome, StartTime: startTime, EndTime: endTime,
	}, page, pageSize)
	if err != nil {
		common.SysError("dataset capture access audit query failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to query dataset capture access audits"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"items": entries, "total": total, "page": page, "page_size": pageSize,
	}})
}

func datasetCaptureAuditSelectionMode(request datasetCaptureExportRequest) string {
	if request.AllFiltered {
		return "all_filtered"
	}
	if len(request.UserIDs) > 0 && len(request.CaptureIDs) > 0 {
		return "users_and_records"
	}
	if len(request.UserIDs) > 0 {
		return "users"
	}
	if len(request.CaptureIDs) == 1 {
		return "single_record"
	}
	return "records"
}

func datasetCaptureAuditMetadata(summary model.DatasetCaptureRecordSummary) map[string]interface{} {
	return map[string]interface{}{
		"scope": "record", "capture_id": summary.CaptureID,
		"user_id": summary.UserID, "username": summary.Username,
		"token_id": summary.TokenID, "token_name": summary.TokenName,
		"group": summary.UserGroup, "model": summary.EffectiveModel,
		"channel_id": summary.ChannelID, "session_id": summary.SessionID,
		"captured_at": summary.CapturedAt, "node": middleware.DatasetCaptureNode(),
	}
}
