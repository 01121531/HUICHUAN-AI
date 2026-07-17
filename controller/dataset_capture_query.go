package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/middleware"
	"github.com/01121531/HUICHUAN-AI/model"
	"github.com/01121531/HUICHUAN-AI/pkg/datasetcapture"
	"github.com/01121531/HUICHUAN-AI/service"
	"github.com/01121531/HUICHUAN-AI/setting/dataset_capture_setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	defaultDatasetCapturePageSize = 20
	maxDatasetCapturePageSize     = 100
	maxDatasetCaptureSearchLength = 200
)

func ListDatasetCaptureUsers(c *gin.Context) {
	filter, content, err := datasetCaptureFilterFromRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := applyDatasetCaptureContentFilter(&filter, content); err != nil {
		datasetCaptureQueryError(c, err)
		return
	}
	page, pageSize := datasetCapturePagination(c)
	users, total, err := model.ListDatasetCaptureUsers(filter, page, pageSize)
	if err != nil {
		datasetCaptureQueryError(c, err)
		return
	}
	totals, err := model.GetDatasetCaptureTotals(filter)
	if err != nil {
		datasetCaptureQueryError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"items": users, "total": total, "page": page, "page_size": pageSize,
		"record_count": totals.RecordCount, "total_size": totals.TotalSize,
		"capture_enabled": dataset_capture_setting.IsEnabled(), "node": middleware.DatasetCaptureNode(),
	}})
}

func ListDatasetCaptureUserRecords(c *gin.Context) {
	userID, err := strconv.Atoi(c.Param("user_id"))
	if err != nil || userID < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid dataset capture user"})
		return
	}
	filter, content, err := datasetCaptureFilterFromRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := applyDatasetCaptureContentFilter(&filter, content); err != nil {
		datasetCaptureQueryError(c, err)
		return
	}
	page, pageSize := datasetCapturePagination(c)
	records, total, err := model.ListDatasetCaptureRecords(filter, userID, page, pageSize)
	if err != nil {
		datasetCaptureQueryError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"items": records, "total": total, "page": page, "page_size": pageSize,
	}})
}

func GetDatasetCaptureFacets(c *gin.Context) {
	facets, err := model.GetDatasetCaptureFacets(middleware.DatasetCaptureNode())
	if err != nil {
		datasetCaptureQueryError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": facets})
}

func GetDatasetCaptureRecord(c *gin.Context) {
	captureID := strings.TrimSpace(c.Param("capture_id"))
	index, err := model.GetDatasetCaptureIndex(middleware.DatasetCaptureNode(), captureID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "dataset capture record not found"})
		return
	}
	if err != nil {
		datasetCaptureQueryError(c, err)
		return
	}
	records, err := datasetCaptureBrowser().ReadRecords([]datasetcapture.RecordLocator{{
		Key: captureID, FileID: index.FileID, Row: index.Row,
	}})
	if err != nil {
		datasetCaptureError(c, err)
		return
	}
	var record json.RawMessage = records[captureID]
	summary, err := model.GetDatasetCaptureSummary(index)
	if err != nil {
		datasetCaptureQueryError(c, err)
		return
	}
	auditInput := model.DatasetCaptureAccessAuditInput{
		Action: model.DatasetCaptureAuditActionView, SelectionMode: "single_record",
		Records: []model.DatasetCaptureRecordSummary{summary},
	}
	auditEventID, err := beginDatasetCaptureAccessAudit(c, auditInput)
	if err != nil {
		common.SysError("dataset capture view audit failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to create dataset capture access audit"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"metadata": summary, "record": record}})
	completeDatasetCaptureAccessAudit(c, auditEventID, model.DatasetCaptureAuditOutcomeDelivered, auditInput)
	recordDatasetCaptureAudit(c, "dataset_capture.record_view", datasetCaptureAuditMetadata(summary))
}

func datasetCaptureFilterFromRequest(c *gin.Context) (model.DatasetCaptureFilter, string, error) {
	startTime, err := optionalInt64Query(c, "start_time")
	if err != nil {
		return model.DatasetCaptureFilter{}, "", err
	}
	endTime, err := optionalInt64Query(c, "end_time")
	if err != nil {
		return model.DatasetCaptureFilter{}, "", err
	}
	if startTime > 0 && endTime > 0 && startTime > endTime {
		return model.DatasetCaptureFilter{}, "", errors.New("start_time must not be after end_time")
	}
	tokenIDs, err := intListQuery(c, "token_id")
	if err != nil {
		return model.DatasetCaptureFilter{}, "", err
	}
	channelIDs, err := intListQuery(c, "channel_id")
	if err != nil {
		return model.DatasetCaptureFilter{}, "", err
	}
	content := strings.TrimSpace(c.Query("content"))
	if len(content) > maxDatasetCaptureSearchLength {
		return model.DatasetCaptureFilter{}, "", errors.New("content search is too long")
	}
	return model.DatasetCaptureFilter{
		Node: middleware.DatasetCaptureNode(), StartTime: startTime, EndTime: endTime,
		Models: stringListQuery(c, "model"), TokenIDs: tokenIDs,
		Groups: stringListQuery(c, "group"), ChannelIDs: channelIDs,
		Username: strings.TrimSpace(c.Query("username")),
	}, content, nil
}

func applyDatasetCaptureContentFilter(filter *model.DatasetCaptureFilter, content string) error {
	if content == "" {
		return nil
	}
	matches, err := service.MatchDatasetCaptureContent(
		middleware.DatasetCapturePathTemplate(), middleware.DatasetCaptureNode(), *filter, content,
	)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		filter.NoMatches = true
		return nil
	}
	filter.CaptureIDs = matches
	return nil
}

func datasetCapturePagination(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", strconv.Itoa(defaultDatasetCapturePageSize)))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = defaultDatasetCapturePageSize
	}
	if pageSize > maxDatasetCapturePageSize {
		pageSize = maxDatasetCapturePageSize
	}
	return page, pageSize
}

func optionalInt64Query(c *gin.Context, key string) (int64, error) {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return 0, errors.New("invalid " + key)
	}
	return parsed, nil
}

func intListQuery(c *gin.Context, key string) ([]int, error) {
	values := stringListQuery(c, key)
	result := make([]int, 0, len(values))
	for _, value := range values {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			return nil, errors.New("invalid " + key)
		}
		result = append(result, parsed)
	}
	return result, nil
}

func stringListQuery(c *gin.Context, key string) []string {
	values := c.QueryArray(key)
	if len(values) == 0 {
		values = []string{c.Query(key)}
	}
	result := make([]string, 0)
	seen := make(map[string]struct{})
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if _, exists := seen[part]; exists {
				continue
			}
			seen[part] = struct{}{}
			result = append(result, part)
		}
	}
	return result
}

func datasetCaptureQueryError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrDatasetCaptureContentSearchTooBroad) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"success": false, "message": err.Error()})
		return
	}
	common.SysError("dataset capture query failed: " + err.Error())
	c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to query dataset captures"})
}
