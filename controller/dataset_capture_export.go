package controller

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/middleware"
	"github.com/01121531/HUICHUAN-AI/model"
	"github.com/01121531/HUICHUAN-AI/service"
	"github.com/01121531/HUICHUAN-AI/service/authz"
	"github.com/gin-gonic/gin"
)

const maxDatasetCaptureExportSelection = 10000

var datasetCaptureIDPattern = regexp.MustCompile(`^[0-9a-f]{24}$`)

type datasetCaptureExportRequest struct {
	UserIDs     []int                       `json:"user_ids"`
	CaptureIDs  []string                    `json:"capture_ids"`
	AllFiltered bool                        `json:"all_filtered"`
	Filter      datasetCaptureFilterRequest `json:"filter"`
}

type datasetCaptureFilterRequest struct {
	StartTime  int64    `json:"start_time"`
	EndTime    int64    `json:"end_time"`
	Models     []string `json:"models"`
	TokenIDs   []int    `json:"token_ids"`
	Groups     []string `json:"groups"`
	ChannelIDs []int    `json:"channel_ids"`
	Username   string   `json:"username"`
	Content    string   `json:"content"`
}

func ExportDatasetCaptures(c *gin.Context) {
	request, filter, selection, err := parseDatasetCaptureExportRequest(c)
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
	export, err := service.BuildDatasetCaptureExport(
		middleware.DatasetCapturePathTemplate(), middleware.DatasetCaptureNode(), indices,
	)
	if errors.Is(err, service.ErrDatasetCaptureExportEmpty) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": err.Error()})
		return
	}
	if errors.Is(err, service.ErrDatasetCaptureExportBusy) {
		c.JSON(http.StatusTooManyRequests, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err != nil {
		common.SysError("dataset capture export failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to export dataset captures"})
		return
	}
	defer export.Close()

	if !authz.Can(c.GetInt("id"), c.GetInt("role"), authz.DatasetCaptureView) ||
		!authz.Can(c.GetInt("id"), c.GetInt("role"), authz.DatasetCaptureDownload) {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "dataset capture download permission was revoked"})
		return
	}
	summaries, err := model.GetDatasetCaptureSummaries(indices)
	if err != nil {
		datasetCaptureQueryError(c, err)
		return
	}
	auditInput := model.DatasetCaptureAccessAuditInput{
		Action:        model.DatasetCaptureAuditActionDownload,
		SelectionMode: datasetCaptureAuditSelectionMode(request), Bytes: export.Bytes,
		StartTime: filter.StartTime, EndTime: filter.EndTime, Models: filter.Models,
		TokenIDs: filter.TokenIDs, Groups: filter.Groups, ChannelIDs: filter.ChannelIDs,
		UsernameFilter: filter.Username, Records: summaries,
	}
	auditEventID, err := beginDatasetCaptureAccessAudit(c, auditInput)
	if err != nil {
		common.SysError("dataset capture download audit failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to create dataset capture access audit"})
		return
	}

	c.Header("Content-Type", "application/x-ndjson")
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": export.Filename}))
	c.Header("Content-Length", strconv.FormatInt(export.Bytes, 10))
	c.Status(http.StatusOK)
	written, err := io.Copy(c.Writer, export.File)
	if err != nil || written != export.Bytes {
		completeDatasetCaptureAccessAudit(c, auditEventID, model.DatasetCaptureAuditOutcomeFailed, auditInput)
		if err == nil {
			err = io.ErrShortWrite
		}
		common.SysError("dataset capture export delivery failed: " + err.Error())
		return
	}
	completeDatasetCaptureAccessAudit(c, auditEventID, model.DatasetCaptureAuditOutcomeDelivered, auditInput)
	recordDatasetCaptureAudit(c, "dataset_capture.download", map[string]interface{}{
		"scope": "selection", "user_count": export.UserCount,
		"record_count": export.RecordCount, "bytes": export.Bytes,
		"start_time": filter.StartTime, "end_time": filter.EndTime,
		"models": filter.Models, "selection_mode": datasetCaptureAuditSelectionMode(request),
		"audit_event_id": auditEventID, "node": middleware.DatasetCaptureNode(),
	})
}

func parseDatasetCaptureExportRequest(c *gin.Context) (datasetCaptureExportRequest, model.DatasetCaptureFilter, model.DatasetCaptureSelection, error) {
	var request datasetCaptureExportRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, model.DatasetCaptureFilter{}, model.DatasetCaptureSelection{}, errors.New("invalid dataset capture export request")
	}
	if err := ensureJSONBodyEnd(decoder); err != nil {
		return request, model.DatasetCaptureFilter{}, model.DatasetCaptureSelection{}, err
	}
	if request.Filter.StartTime < 0 || request.Filter.EndTime < 0 ||
		(request.Filter.StartTime > 0 && request.Filter.EndTime > 0 && request.Filter.StartTime > request.Filter.EndTime) {
		return request, model.DatasetCaptureFilter{}, model.DatasetCaptureSelection{}, errors.New("invalid export time range")
	}
	if len(request.UserIDs)+len(request.CaptureIDs) > maxDatasetCaptureExportSelection {
		return request, model.DatasetCaptureFilter{}, model.DatasetCaptureSelection{}, errors.New("dataset capture export selection is too large")
	}
	userIDs, err := uniqueNonNegativeInts(request.UserIDs, "user_ids")
	if err != nil {
		return request, model.DatasetCaptureFilter{}, model.DatasetCaptureSelection{}, err
	}
	captureIDs, err := uniqueCaptureIDs(request.CaptureIDs)
	if err != nil {
		return request, model.DatasetCaptureFilter{}, model.DatasetCaptureSelection{}, err
	}
	request.UserIDs = userIDs
	request.CaptureIDs = captureIDs
	if request.AllFiltered && (len(userIDs) > 0 || len(captureIDs) > 0) {
		return request, model.DatasetCaptureFilter{}, model.DatasetCaptureSelection{}, errors.New("all_filtered cannot be combined with explicit selections")
	}
	if !request.AllFiltered && len(userIDs) == 0 && len(captureIDs) == 0 {
		return request, model.DatasetCaptureFilter{}, model.DatasetCaptureSelection{}, errors.New("select at least one user or capture record")
	}
	tokenIDs, err := uniquePositiveInts(request.Filter.TokenIDs, "token_ids")
	if err != nil {
		return request, model.DatasetCaptureFilter{}, model.DatasetCaptureSelection{}, err
	}
	channelIDs, err := uniquePositiveInts(request.Filter.ChannelIDs, "channel_ids")
	if err != nil {
		return request, model.DatasetCaptureFilter{}, model.DatasetCaptureSelection{}, err
	}
	models, err := uniqueBoundedStrings(request.Filter.Models, 255, "models")
	if err != nil {
		return request, model.DatasetCaptureFilter{}, model.DatasetCaptureSelection{}, err
	}
	groups, err := uniqueBoundedStrings(request.Filter.Groups, 64, "groups")
	if err != nil {
		return request, model.DatasetCaptureFilter{}, model.DatasetCaptureSelection{}, err
	}
	request.Filter.Content = strings.TrimSpace(request.Filter.Content)
	request.Filter.Username = strings.TrimSpace(request.Filter.Username)
	if len(request.Filter.Content) > maxDatasetCaptureSearchLength || len(request.Filter.Username) > maxDatasetCaptureSearchLength {
		return request, model.DatasetCaptureFilter{}, model.DatasetCaptureSelection{}, errors.New("dataset capture export search is too long")
	}
	filter := model.DatasetCaptureFilter{
		Node: middleware.DatasetCaptureNode(), StartTime: request.Filter.StartTime, EndTime: request.Filter.EndTime,
		Models: models, TokenIDs: tokenIDs, Groups: groups, ChannelIDs: channelIDs,
		Username: request.Filter.Username,
	}
	selection := model.DatasetCaptureSelection{UserIDs: userIDs, CaptureIDs: captureIDs, AllFiltered: request.AllFiltered}
	return request, filter, selection, nil
}

func ensureJSONBodyEnd(decoder *json.Decoder) error {
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	}
	return errors.New("invalid dataset capture export request")
}

func uniqueNonNegativeInts(values []int, field string) ([]int, error) {
	result := make([]int, 0, len(values))
	seen := make(map[int]struct{}, len(values))
	for _, value := range values {
		if value < 0 {
			return nil, errors.New("invalid " + field)
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func uniquePositiveInts(values []int, field string) ([]int, error) {
	result, err := uniqueNonNegativeInts(values, field)
	if err != nil {
		return nil, err
	}
	for _, value := range result {
		if value == 0 {
			return nil, errors.New("invalid " + field)
		}
	}
	return result, nil
}

func uniqueCaptureIDs(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if !datasetCaptureIDPattern.MatchString(value) {
			return nil, errors.New("invalid capture_ids")
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func uniqueBoundedStrings(values []string, maxLength int, field string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > maxLength {
			return nil, errors.New("invalid " + field)
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}
