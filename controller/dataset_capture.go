package controller

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/datasetcapture"
	"github.com/QuantumNous/new-api/setting/dataset_capture_setting"
	"github.com/gin-gonic/gin"
)

func datasetCaptureBrowser() *datasetcapture.Browser {
	return datasetcapture.NewBrowser(middleware.DatasetCapturePathTemplate(), middleware.DatasetCaptureNode())
}

func ListDatasetCaptureFiles(c *gin.Context) {
	files, err := datasetCaptureBrowser().ListFiles()
	if err != nil {
		common.ApiErrorMsg(c, "failed to list dataset capture files: "+err.Error())
		return
	}
	enrichDatasetCaptureFiles(files)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"files":           files,
			"capture_enabled": dataset_capture_setting.IsEnabled(),
			"can_delete":      c.GetInt("role") == common.RoleRootUser,
			"node":            middleware.DatasetCaptureNode(),
		},
	})
}

func DeleteDatasetCaptureFile(c *gin.Context) {
	fileID := c.Param("file_id")
	var deletedRecords int64
	file, err := datasetCaptureBrowser().DeleteWithCallback(fileID, func(file datasetcapture.CaptureFile) error {
		var deleteErr error
		deletedRecords, deleteErr = model.DeleteDatasetCaptureIndicesByFile(file.Node, file.ID)
		return deleteErr
	})
	if err != nil {
		datasetCaptureError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
	recordDatasetCaptureAudit(c, "dataset_capture.delete", map[string]interface{}{
		"file_id": file.ID, "scope": "conversation",
		"deleted_records": deletedRecords, "node": file.Node,
	})
}

func enrichDatasetCaptureFiles(files []datasetcapture.CaptureFile) {
	userIDs := make([]int, 0)
	tokenIDs := make([]int, 0)
	seenUsers := map[int]struct{}{}
	seenTokens := map[int]struct{}{}
	for _, file := range files {
		if id, err := strconv.Atoi(file.UserKey); err == nil && id > 0 {
			if _, exists := seenUsers[id]; !exists {
				seenUsers[id] = struct{}{}
				userIDs = append(userIDs, id)
			}
		}
		if id, err := strconv.Atoi(file.TokenKey); err == nil && id > 0 {
			if _, exists := seenTokens[id]; !exists {
				seenTokens[id] = struct{}{}
				tokenIDs = append(tokenIDs, id)
			}
		}
	}

	userNames := map[int]string{}
	if len(userIDs) > 0 {
		var users []struct {
			Id       int
			Username string
		}
		if err := model.DB.Unscoped().Model(&model.User{}).Select("id", "username").Where("id IN ?", userIDs).Scan(&users).Error; err != nil {
			common.SysError("dataset capture user labels failed: " + err.Error())
		} else {
			for _, user := range users {
				userNames[user.Id] = user.Username
			}
		}
	}
	tokenNames := map[int]string{}
	if len(tokenIDs) > 0 {
		var tokens []struct {
			Id   int
			Name string
		}
		if err := model.DB.Unscoped().Model(&model.Token{}).Select("id", "name").Where("id IN ?", tokenIDs).Scan(&tokens).Error; err != nil {
			common.SysError("dataset capture token labels failed: " + err.Error())
		} else {
			for _, token := range tokens {
				tokenNames[token.Id] = token.Name
			}
		}
	}

	for index := range files {
		file := &files[index]
		switch file.UserKey {
		case "anonymous":
			file.UserName = "Anonymous"
		default:
			if id, err := strconv.Atoi(file.UserKey); err == nil {
				file.UserName = userNames[id]
			}
		}
		switch file.TokenKey {
		case "anonymous":
			file.TokenName = "Anonymous"
		case "playground":
			file.TokenName = "Playground"
		default:
			if id, err := strconv.Atoi(file.TokenKey); err == nil {
				file.TokenName = tokenNames[id]
			}
		}
	}
}

func ListDatasetCaptureRecords(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	fileID := c.Param("file_id")
	browser := datasetCaptureBrowser()
	records, err := browser.Records(fileID, page, pageSize)
	if err != nil {
		datasetCaptureError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": records})
	file, _ := browser.Resolve(fileID)
	recordDatasetCaptureAudit(c, "dataset_capture.view", map[string]interface{}{
		"file_id":   fileID,
		"file":      file.Name,
		"page":      records.Page,
		"page_size": records.PageSize,
		"rows":      len(records.Records),
		"node":      middleware.DatasetCaptureNode(),
	})
}

func DownloadDatasetCaptureFile(c *gin.Context) {
	fileID := c.Param("file_id")
	handle, file, err := datasetCaptureBrowser().Open(fileID)
	if err != nil {
		datasetCaptureError(c, err)
		return
	}
	defer handle.Close()
	c.Header("Content-Type", "application/x-ndjson")
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": file.Name}))
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, handle); err != nil {
		common.SysError("dataset capture download failed: " + err.Error())
		return
	}
	recordDatasetCaptureAudit(c, "dataset_capture.download", map[string]interface{}{
		"file_id": file.ID,
		"file":    file.Name,
		"scope":   "file",
		"node":    file.Node,
	})
}

func DownloadDatasetCaptureRecord(c *gin.Context) {
	row, err := strconv.Atoi(c.Param("row"))
	if err != nil || row < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid dataset capture row"})
		return
	}
	fileID := c.Param("file_id")
	line, file, err := datasetCaptureBrowser().Record(fileID, row)
	if err != nil {
		datasetCaptureError(c, err)
		return
	}
	name := fmt.Sprintf("%s-row-%d.jsonl", file.Name[:len(file.Name)-len(".jsonl")], row)
	c.Header("Content-Type", "application/x-ndjson")
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	c.Data(http.StatusOK, "application/x-ndjson", line)
	recordDatasetCaptureAudit(c, "dataset_capture.download", map[string]interface{}{
		"file_id": file.ID,
		"file":    file.Name,
		"scope":   "record",
		"row":     row,
		"node":    file.Node,
	})
}

func datasetCaptureError(c *gin.Context, err error) {
	if errors.Is(err, datasetcapture.ErrCaptureFileNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "dataset capture file or record not found"})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to read dataset capture data"})
	common.SysError("dataset capture read failed: " + err.Error())
}

func recordDatasetCaptureAudit(c *gin.Context, action string, params map[string]interface{}) {
	operatorID := c.GetInt("id")
	adminInfo := map[string]interface{}{
		"admin_id":       operatorID,
		"admin_username": c.GetString("username"),
		"admin_role":     c.GetInt("role"),
		"auth_method": func() string {
			if c.GetBool("use_access_token") {
				return "access_token"
			}
			return "session"
		}(),
	}
	auditInfo := map[string]interface{}{
		"method":  c.Request.Method,
		"route":   c.FullPath(),
		"status":  c.Writer.Status(),
		"success": true,
	}
	model.RecordOperationAuditLog(operatorID, action, c.ClientIP(), action, params, adminInfo, auditInfo)
}
