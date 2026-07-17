package controller

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/model"
	"github.com/01121531/HUICHUAN-AI/service"
	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

var usageLogDirectExportSlot = make(chan struct{}, 1)

type usageLogExportRequest struct {
	SelectionMode string                     `json:"selection_mode"`
	Format        string                     `json:"format"`
	IPVisibility  string                     `json:"ip_visibility"`
	IDs           []int                      `json:"ids"`
	RequestIDs    []string                   `json:"request_ids"`
	Filters       model.UsageLogExportFilter `json:"filters"`
}

func ExportAllUsageLogs(c *gin.Context) {
	exportUsageLogs(c, false)
}

func ExportSelfUsageLogs(c *gin.Context) {
	exportUsageLogs(c, true)
}

func exportUsageLogs(c *gin.Context, selfOnly bool) {
	var request usageLogExportRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid export request"})
		return
	}
	if err := normalizeUsageLogExportRequest(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if selfOnly {
		request.Filters.UserID = c.GetInt("id")
		request.Filters.Username = ""
	} else {
		request.Filters.UserID = 0
	}
	frozenFilters, err := model.FreezeUsageLogExportFilter(request.Filters)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	request.Filters = frozenFilters

	total, err := model.CountUsageLogsForExport(request.Filters)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if maxRows := common.UsageLogExportMaxRows(); maxRows > 0 && total > maxRows {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "export exceeds the configured maximum row count",
			"total":   total,
		})
		return
	}

	directLimit := common.UsageLogExportDirectLimit()
	if total > int64(directLimit) {
		createUsageLogExportTask(c, request, selfOnly, total, "row_threshold")
		return
	}

	select {
	case usageLogDirectExportSlot <- struct{}{}:
		defer func() { <-usageLogDirectExportSlot }()
	default:
		createUsageLogExportTask(c, request, selfOnly, total, "direct_export_busy")
		return
	}

	logs, err := model.ListUsageLogsForExport(request.Filters, directLimit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	filename := fmt.Sprintf("usage-logs-%s.%s", time.Now().Format("20060102-150405"), request.Format)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	if request.Format == "csv" {
		c.Header("Content-Type", "text/csv; charset=utf-8")
		err = writeUsageLogCSV(c.Writer, logs)
	} else {
		c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		err = writeUsageLogXLSX(c.Writer, logs, request)
	}
	if err != nil {
		common.SysError("usage log direct export failed: " + err.Error())
		return
	}

	auditParams := map[string]interface{}{
		"format":         request.Format,
		"record_count":   len(logs),
		"selection_mode": request.SelectionMode,
		"ip_visibility":  request.IPVisibility,
		"delivery":       "direct",
	}
	if selfOnly {
		recordUserSecurityAudit(c, c.GetInt("id"), "usage_log.export_download", auditParams)
	} else {
		recordManageAudit(c, "usage_log.export_download", auditParams)
	}
}

func createUsageLogExportTask(c *gin.Context, request usageLogExportRequest, selfOnly bool, total int64, reason string) {
	scope := "admin"
	if selfOnly {
		scope = "self"
	}
	payload := usageLogExportTaskPayload{
		CreatorID:   c.GetInt("id"),
		CreatorRole: c.GetInt("role"),
		Scope:       scope,
		Request:     request,
		Filters:     request.Filters,
		CreatedAt:   common.GetTimestamp(),
	}
	state := usageLogExportTaskState{
		Phase:  usageLogExportPhasePending,
		Total:  total,
		Format: request.Format,
	}
	task, err := service.EnqueueQueuedSystemTask(model.SystemTaskTypeUsageLogExport, payload, state)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	auditParams := map[string]interface{}{
		"task_id":        task.TaskID,
		"format":         request.Format,
		"record_count":   total,
		"selection_mode": request.SelectionMode,
		"ip_visibility":  request.IPVisibility,
		"reason":         reason,
	}
	if selfOnly {
		recordUserSecurityAudit(c, c.GetInt("id"), "usage_log.export_create", auditParams)
	} else {
		recordManageAudit(c, "usage_log.export_create", auditParams)
	}
	c.JSON(http.StatusAccepted, gin.H{
		"success":             true,
		"requires_background": true,
		"total":               total,
		"message":             "export has been queued",
		"data":                task.ToResponse(),
	})
}

func GetAllUsageLogExportTask(c *gin.Context) {
	getUsageLogExportTask(c, false)
}

func GetSelfUsageLogExportTask(c *gin.Context) {
	getUsageLogExportTask(c, true)
}

func getUsageLogExportTask(c *gin.Context, selfOnly bool) {
	task, payload, state, ok := loadAuthorizedUsageLogExportTask(c, selfOnly)
	if !ok {
		return
	}
	if expireUsageLogExportTaskIfNeeded(task, &state) {
		task, _ = model.GetSystemTaskByTaskID(task.TaskID)
	}
	response := task.ToResponse()
	response.Payload = map[string]interface{}{
		"scope":      payload.Scope,
		"format":     payload.Request.Format,
		"created_at": payload.CreatedAt,
		"creator_id": payload.CreatorID,
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    response,
	})
}

func DownloadAllUsageLogExport(c *gin.Context) {
	downloadUsageLogExport(c, false)
}

func DownloadSelfUsageLogExport(c *gin.Context) {
	downloadUsageLogExport(c, true)
}

func downloadUsageLogExport(c *gin.Context, selfOnly bool) {
	task, payload, state, ok := loadAuthorizedUsageLogExportTask(c, selfOnly)
	if !ok {
		return
	}
	if task.Status != model.SystemTaskStatusSucceeded || state.Phase != usageLogExportPhaseSucceeded {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "export is not ready"})
		return
	}
	if expireUsageLogExportTaskIfNeeded(task, &state) {
		c.JSON(http.StatusGone, gin.H{"success": false, "message": "export has expired"})
		return
	}
	if state.FileID == "" || (payload.Request.Format != "csv" && payload.Request.Format != "xlsx") {
		c.JSON(http.StatusGone, gin.H{"success": false, "message": "export file is unavailable"})
		return
	}
	if !isSafeUsageLogExportFileID(state.FileID) {
		c.JSON(http.StatusGone, gin.H{"success": false, "message": "export file is unavailable"})
		return
	}
	path := filepath.Join(usageLogExportTaskDir(task.TaskID), state.FileID+"."+payload.Request.Format)
	hash, size, err := hashUsageLogExportFile(path)
	if err != nil || size != state.FileSize || hash != state.SHA256 {
		c.JSON(http.StatusGone, gin.H{"success": false, "message": "export file verification failed"})
		return
	}
	filename := fmt.Sprintf("usage-logs-%s.%s", time.Now().Format("20060102-150405"), payload.Request.Format)
	c.FileAttachment(path, filename)
	params := map[string]interface{}{
		"task_id":       task.TaskID,
		"format":        payload.Request.Format,
		"record_count":  state.Processed,
		"file_size":     size,
		"ip_visibility": payload.Request.IPVisibility,
		"delivery":      "background",
		"status":        c.Writer.Status(),
	}
	if selfOnly {
		recordUserSecurityAudit(c, c.GetInt("id"), "usage_log.export_download", params)
	} else {
		recordManageAudit(c, "usage_log.export_download", params)
	}
}

func loadAuthorizedUsageLogExportTask(c *gin.Context, selfOnly bool) (*model.SystemTask, usageLogExportTaskPayload, usageLogExportTaskState, bool) {
	taskID := strings.TrimSpace(c.Param("task_id"))
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "task id is required"})
		return nil, usageLogExportTaskPayload{}, usageLogExportTaskState{}, false
	}
	task, err := model.GetSystemTaskByTaskID(taskID)
	if err != nil {
		common.ApiError(c, err)
		return nil, usageLogExportTaskPayload{}, usageLogExportTaskState{}, false
	}
	if task == nil || task.Type != model.SystemTaskTypeUsageLogExport {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "export task not found"})
		return nil, usageLogExportTaskPayload{}, usageLogExportTaskState{}, false
	}
	payload := usageLogExportTaskPayload{}
	state := usageLogExportTaskState{}
	if task.DecodePayload(&payload) != nil || task.DecodeState(&state) != nil {
		c.JSON(http.StatusGone, gin.H{"success": false, "message": "export task metadata is invalid"})
		return nil, usageLogExportTaskPayload{}, usageLogExportTaskState{}, false
	}
	payload.Request.IPVisibility = "full"
	currentUserID := c.GetInt("id")
	currentUser, err := model.GetUserById(currentUserID, false)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "export task access denied"})
		return nil, usageLogExportTaskPayload{}, usageLogExportTaskState{}, false
	}
	currentRole := currentUser.Role
	if payload.CreatorID != currentUserID {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "export task access denied"})
		return nil, usageLogExportTaskPayload{}, usageLogExportTaskState{}, false
	}
	if payload.Scope == "admin" {
		if selfOnly || currentRole < common.RoleAdminUser {
			c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "export task access denied"})
			return nil, usageLogExportTaskPayload{}, usageLogExportTaskState{}, false
		}
	} else if payload.Scope != "self" || !selfOnly {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "export task access denied"})
		return nil, usageLogExportTaskPayload{}, usageLogExportTaskState{}, false
	}
	return task, payload, state, true
}

func expireUsageLogExportTaskIfNeeded(task *model.SystemTask, state *usageLogExportTaskState) bool {
	if state.ExpiresAt == 0 || state.ExpiresAt > common.GetTimestamp() {
		return false
	}
	_ = os.RemoveAll(usageLogExportTaskDir(task.TaskID))
	state.Phase = usageLogExportPhaseExpired
	state.ErrorCode = ""
	_ = model.UpdateCompletedSystemTaskState(task.TaskID, *state)
	return true
}

func isSafeUsageLogExportFileID(fileID string) bool {
	if !strings.HasPrefix(fileID, "usage_") || len(fileID) > 64 {
		return false
	}
	for _, char := range fileID {
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '_' {
			continue
		}
		return false
	}
	return true
}

func normalizeUsageLogExportRequest(request *usageLogExportRequest) error {
	request.Format = strings.ToLower(strings.TrimSpace(request.Format))
	if request.Format != "csv" && request.Format != "xlsx" {
		return errors.New("format must be csv or xlsx")
	}
	if request.SelectionMode == "" {
		request.SelectionMode = "filtered"
	}
	if request.SelectionMode != "selected" && request.SelectionMode != "filtered" {
		return errors.New("selection_mode must be selected or filtered")
	}
	request.IPVisibility = "full"
	if len(request.IDs) > 100000 || len(request.RequestIDs) > 100000 {
		return errors.New("too many selected log identifiers")
	}
	if request.SelectionMode == "selected" {
		if len(request.IDs) == 0 && len(request.RequestIDs) == 0 {
			return errors.New("selected export requires log identifiers")
		}
		request.Filters.SelectedIDs = usageExportUniquePositiveInts(request.IDs)
		request.Filters.SelectedRequestIDs = usageExportUniqueNonEmptyStrings(request.RequestIDs)
	} else {
		request.Filters.SelectedIDs = nil
		request.Filters.SelectedRequestIDs = nil
	}
	return nil
}

func usageExportUniquePositiveInts(values []int) []int {
	seen := make(map[int]struct{}, len(values))
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func usageExportUniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

var usageLogExportHeaders = []string{
	"时间", "日志类型", "用户ID", "用户名", "令牌ID", "令牌名称",
	"请求模型", "实际模型", "渠道ID", "渠道名称", "分组", "请求IP",
	"Request ID", "Upstream Request ID", "请求路径", "是否流式", "响应时间(秒)",
	"输入Token", "输出Token", "计价方式", "计费来源", "最终扣费额度", "快照状态", "日志摘要",
}

func writeUsageLogCSV(writer io.Writer, logs []*model.Log) error {
	if _, err := writer.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return err
	}
	csvWriter := csv.NewWriter(writer)
	if err := csvWriter.Write(usageLogExportHeaders); err != nil {
		return err
	}
	for _, log := range logs {
		row, _, _ := usageLogExportRow(log)
		for index := range row {
			row[index] = sanitizeSpreadsheetText(row[index])
		}
		if err := csvWriter.Write(row); err != nil {
			return err
		}
	}
	csvWriter.Flush()
	return csvWriter.Error()
}

func writeUsageLogXLSX(writer io.Writer, logs []*model.Log, request usageLogExportRequest) error {
	file := excelize.NewFile()
	defer file.Close()
	detailsSheet := "日志明细"
	defaultSheet := file.GetSheetName(0)
	if err := file.SetSheetName(defaultSheet, detailsSheet); err != nil {
		return err
	}
	if _, err := file.NewSheet("计价组成"); err != nil {
		return err
	}
	if _, err := file.NewSheet("导出说明"); err != nil {
		return err
	}

	detailsWriter, err := file.NewStreamWriter(detailsSheet)
	if err != nil {
		return err
	}
	if err = detailsWriter.SetRow("A1", stringInterfaces(usageLogExportHeaders)); err != nil {
		return err
	}
	componentHeaders := []string{"Request ID", "组成类型", "数量", "单位", "请求时单价(USD)", "价格单位", "倍率", "小计额度", "备注"}
	componentWriter, err := file.NewStreamWriter("计价组成")
	if err != nil {
		return err
	}
	if err = componentWriter.SetRow("A1", stringInterfaces(componentHeaders)); err != nil {
		return err
	}

	componentRow := 2
	for index, log := range logs {
		row, snapshot, _ := usageLogExportRow(log)
		if err = detailsWriter.SetRow(fmt.Sprintf("A%d", index+2), stringInterfaces(row)); err != nil {
			return err
		}
		if snapshot == nil {
			continue
		}
		for _, component := range snapshot.Components {
			values := []interface{}{
				sanitizeSpreadsheetText(log.RequestId),
				component.Kind,
				component.Quantity,
				component.Unit,
				component.UnitPriceUSD,
				component.PriceUnit,
				component.Ratio,
				component.SubtotalQuota,
				sanitizeSpreadsheetText(component.Note),
			}
			if err = componentWriter.SetRow(fmt.Sprintf("A%d", componentRow), values); err != nil {
				return err
			}
			componentRow++
		}
	}
	if err = detailsWriter.Flush(); err != nil {
		return err
	}
	if err = componentWriter.Flush(); err != nil {
		return err
	}

	noteRows := [][]interface{}{
		{"字段", "值"},
		{"导出时间", time.Now().Format(time.RFC3339)},
		{"导出范围", request.SelectionMode},
		{"IP显示方式", request.IPVisibility},
		{"总记录数", len(logs)},
		{"计价快照版本", model.BillingSnapshotVersion},
		{"HUICHUAN-AI版本", common.Version},
	}
	for index, row := range noteRows {
		if err = file.SetSheetRow("导出说明", fmt.Sprintf("A%d", index+1), &row); err != nil {
			return err
		}
	}
	_, err = file.WriteTo(writer)
	return err
}

func stringInterfaces(values []string) []interface{} {
	result := make([]interface{}, len(values))
	for index, value := range values {
		result[index] = sanitizeSpreadsheetText(value)
	}
	return result
}

func usageLogExportRow(log *model.Log) ([]string, *model.BillingSnapshotV1, map[string]interface{}) {
	if log == nil {
		return make([]string, len(usageLogExportHeaders)), nil, nil
	}
	other, _ := common.StrToMap(log.Other)
	var snapshot *model.BillingSnapshotV1
	if raw, exists := other["billing_snapshot_v1"]; exists {
		if encoded, err := json.Marshal(raw); err == nil {
			var parsed model.BillingSnapshotV1
			if json.Unmarshal(encoded, &parsed) == nil {
				snapshot = &parsed
			}
		}
	}
	effectiveModel := log.ModelName
	requestPath := ""
	mode := "legacy"
	source := stringFromMap(other, "billing_source")
	status := "legacy"
	if value := stringFromMap(other, "upstream_model_name"); value != "" {
		effectiveModel = value
	}
	requestPath = stringFromMap(other, "request_path")
	if snapshot != nil {
		mode = snapshot.Mode
		source = snapshot.Source
		status = snapshot.Status
	}
	row := []string{
		time.Unix(log.CreatedAt, 0).Format(time.RFC3339),
		strconv.Itoa(log.Type),
		strconv.Itoa(log.UserId),
		log.Username,
		strconv.Itoa(log.TokenId),
		log.TokenName,
		log.ModelName,
		effectiveModel,
		strconv.Itoa(log.ChannelId),
		log.ChannelName,
		log.Group,
		log.Ip,
		log.RequestId,
		log.UpstreamRequestId,
		requestPath,
		strconv.FormatBool(log.IsStream),
		strconv.Itoa(log.UseTime),
		strconv.Itoa(log.PromptTokens),
		strconv.Itoa(log.CompletionTokens),
		mode,
		source,
		strconv.Itoa(log.Quota),
		status,
		log.Content,
	}
	return row, snapshot, other
}

func stringFromMap(values map[string]interface{}, key string) string {
	if value, ok := values[key].(string); ok {
		return value
	}
	return ""
}

func sanitizeSpreadsheetText(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + value
	}
	return value
}
