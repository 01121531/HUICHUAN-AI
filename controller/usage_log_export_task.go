package controller

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/model"
	"github.com/shirou/gopsutil/disk"
	"github.com/xuri/excelize/v2"
)

const (
	usageLogExportPhasePending    = "pending"
	usageLogExportPhaseCounting   = "counting"
	usageLogExportPhaseExporting  = "exporting"
	usageLogExportPhaseFinalizing = "finalizing"
	usageLogExportPhaseSucceeded  = "succeeded"
	usageLogExportPhaseFailed     = "failed"
	usageLogExportPhaseExpired    = "expired"
)

type usageLogExportTaskPayload struct {
	CreatorID   int                        `json:"creator_id"`
	CreatorRole int                        `json:"creator_role"`
	Scope       string                     `json:"scope"`
	Request     usageLogExportRequest      `json:"request"`
	Filters     model.UsageLogExportFilter `json:"filters"`
	CreatedAt   int64                      `json:"created_at"`
}

type usageLogExportTaskState struct {
	Phase     string `json:"phase"`
	Total     int64  `json:"total"`
	Processed int64  `json:"processed"`
	Progress  int    `json:"progress"`
	Format    string `json:"format"`
	FileID    string `json:"file_id,omitempty"`
	FileSize  int64  `json:"file_size,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	ExpiresAt int64  `json:"expires_at,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

type usageLogExportTaskResult struct {
	FileID    string `json:"file_id"`
	FileSize  int64  `json:"file_size"`
	SHA256    string `json:"sha256"`
	ExpiresAt int64  `json:"expires_at"`
	Format    string `json:"format"`
}

type UsageLogExportRuntimeStatus struct {
	Active         int64 `json:"active"`
	RowsExported   int64 `json:"rows_exported"`
	Failures       int64 `json:"failures"`
	BytesWritten   int64 `json:"bytes_written"`
	DirectLimit    int   `json:"direct_limit"`
	MaxRows        int64 `json:"max_rows"`
	BatchSize      int   `json:"batch_size"`
	RetentionHours int   `json:"retention_hours"`
}

func GetUsageLogExportRuntimeStatus() UsageLogExportRuntimeStatus {
	return UsageLogExportRuntimeStatus{
		Active: usageLogExportActive.Load(), RowsExported: usageLogExportRows.Load(),
		Failures: usageLogExportFailures.Load(), BytesWritten: usageLogExportBytes.Load(),
		DirectLimit: common.UsageLogExportDirectLimit(), MaxRows: common.UsageLogExportMaxRows(),
		BatchSize: common.UsageLogExportBatchSize(), RetentionHours: common.UsageLogExportRetentionHours(),
	}
}

type usageLogExportTaskHandler struct{}

func (usageLogExportTaskHandler) Type() string {
	return model.SystemTaskTypeUsageLogExport
}

var (
	usageLogExportActive      atomic.Int64
	usageLogExportRows        atomic.Int64
	usageLogExportFailures    atomic.Int64
	usageLogExportBytes       atomic.Int64
	usageLogExportCleanerOnce sync.Once
	usageLogExportDiskFree    = func(path string) (uint64, error) {
		usage, err := disk.Usage(path)
		if err != nil {
			return 0, err
		}
		return usage.Free, nil
	}
)

const usageLogExportMinFreeDiskBytes = uint64(256 * 1024 * 1024)

func (usageLogExportTaskHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := usageLogExportTaskPayload{}
	if err := task.DecodePayload(&payload); err != nil {
		failUsageLogExportTask(task, runnerID, payload, "invalid_payload", err)
		return
	}
	payload.Request.IPVisibility = "full"
	usageLogExportActive.Add(1)
	defer usageLogExportActive.Add(-1)

	state := usageLogExportTaskState{
		Phase:  usageLogExportPhaseCounting,
		Format: payload.Request.Format,
	}
	if err := model.UpdateSystemTaskState(task.TaskID, runnerID, state); err != nil {
		return
	}

	total, err := model.CountUsageLogsForExport(payload.Filters)
	if err != nil {
		failUsageLogExportTask(task, runnerID, payload, "count_failed", err)
		return
	}
	if maxRows := common.UsageLogExportMaxRows(); maxRows > 0 && total > maxRows {
		failUsageLogExportTask(task, runnerID, payload, "row_limit_exceeded", errors.New("row limit exceeded"))
		return
	}

	state.Total = total
	state.Phase = usageLogExportPhaseExporting
	if err = model.UpdateSystemTaskState(task.TaskID, runnerID, state); err != nil {
		return
	}

	fileID, err := common.GenerateRandomCharsKey(24)
	if err != nil {
		failUsageLogExportTask(task, runnerID, payload, "file_id_failed", err)
		return
	}
	fileID = "usage_" + fileID
	taskDir := usageLogExportTaskDir(task.TaskID)
	if err = os.MkdirAll(taskDir, 0700); err != nil {
		failUsageLogExportTask(task, runnerID, payload, "directory_create_failed", err)
		return
	}
	if err = ensureUsageLogExportDiskSpace(taskDir); err != nil {
		_ = os.RemoveAll(taskDir)
		failUsageLogExportTask(task, runnerID, payload, "insufficient_disk", err)
		return
	}
	finalPath := filepath.Join(taskDir, fileID+"."+payload.Request.Format)
	tempPath := finalPath + ".tmp"
	writer, err := newBackgroundUsageLogExportWriter(tempPath, payload.Request, total)
	if err != nil {
		_ = os.RemoveAll(taskDir)
		failUsageLogExportTask(task, runnerID, payload, "file_create_failed", err)
		return
	}
	completed := false
	defer func() {
		if !completed {
			writer.Abort()
			_ = os.RemoveAll(taskDir)
		}
	}()

	cursor := model.UsageLogExportCursor{}
	batchSize := common.UsageLogExportBatchSize()
	for {
		if err = ctx.Err(); err != nil {
			failUsageLogExportTask(task, runnerID, payload, "task_cancelled", err)
			return
		}
		var logs []*model.Log
		logs, cursor, err = model.ListUsageLogsForExportCursor(payload.Filters, cursor, batchSize)
		if err != nil {
			failUsageLogExportTask(task, runnerID, payload, "query_failed", err)
			return
		}
		if len(logs) == 0 {
			break
		}
		remaining := state.Total - state.Processed
		if remaining <= 0 {
			break
		}
		if int64(len(logs)) > remaining {
			logs = logs[:remaining]
		}
		if err = writer.Append(logs); err != nil {
			failUsageLogExportTask(task, runnerID, payload, "write_failed", err)
			return
		}
		state.Processed += int64(len(logs))
		state.Progress = usageLogExportProgress(state.Processed, state.Total)
		if err = model.UpdateSystemTaskState(task.TaskID, runnerID, state); err != nil {
			return
		}
		if state.Processed%int64(batchSize*10) == 0 {
			if err = ensureUsageLogExportDiskSpace(taskDir); err != nil {
				failUsageLogExportTask(task, runnerID, payload, "insufficient_disk", err)
				return
			}
		}
	}

	state.Phase = usageLogExportPhaseFinalizing
	state.Progress = 99
	if err = model.UpdateSystemTaskState(task.TaskID, runnerID, state); err != nil {
		return
	}
	if err = writer.Finish(); err != nil {
		failUsageLogExportTask(task, runnerID, payload, "finalize_failed", err)
		return
	}
	hash, size, err := hashUsageLogExportFile(tempPath)
	if err != nil {
		failUsageLogExportTask(task, runnerID, payload, "hash_failed", err)
		return
	}
	if err = os.Rename(tempPath, finalPath); err != nil {
		failUsageLogExportTask(task, runnerID, payload, "rename_failed", err)
		return
	}
	if err = os.Chmod(finalPath, 0600); err != nil {
		failUsageLogExportTask(task, runnerID, payload, "permission_failed", err)
		return
	}

	state.Phase = usageLogExportPhaseSucceeded
	state.Progress = 100
	state.FileID = fileID
	state.FileSize = size
	state.SHA256 = hash
	state.ExpiresAt = common.GetTimestamp() + int64(common.UsageLogExportRetentionHours()*3600)
	if err = model.UpdateSystemTaskState(task.TaskID, runnerID, state); err != nil {
		return
	}
	result := usageLogExportTaskResult{
		FileID: fileID, FileSize: size, SHA256: hash,
		ExpiresAt: state.ExpiresAt, Format: payload.Request.Format,
	}
	if err = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, result, ""); err != nil {
		return
	}

	completed = true
	usageLogExportRows.Add(state.Processed)
	usageLogExportBytes.Add(size)
}

func failUsageLogExportTask(task *model.SystemTask, runnerID string, payload usageLogExportTaskPayload, code string, cause error) {
	usageLogExportFailures.Add(1)
	state := usageLogExportTaskState{
		Phase:     usageLogExportPhaseFailed,
		Format:    payload.Request.Format,
		ErrorCode: code,
	}
	_ = model.UpdateSystemTaskState(task.TaskID, runnerID, state)
	_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, code)
	if payload.CreatorID > 0 {
		model.RecordOperationAuditLog(
			payload.CreatorID,
			"Usage log export failed",
			"",
			"usage_log.export_failed",
			map[string]interface{}{"task_id": task.TaskID, "error_code": code},
			nil,
			nil,
		)
	}
	if cause != nil {
		common.SysError(fmt.Sprintf("usage log export task failed: task=%s code=%s err=%v", task.TaskID, code, cause))
	}
}

func usageLogExportProgress(processed, total int64) int {
	if total <= 0 {
		return 99
	}
	progress := int(processed * 100 / total)
	if progress > 99 {
		return 99
	}
	if progress < 0 {
		return 0
	}
	return progress
}

func usageLogExportTaskDir(taskID string) string {
	return filepath.Join(*common.LogDir, "exports", "usage-logs", taskID)
}

func hashUsageLogExportFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), size, nil
}

func ensureUsageLogExportDiskSpace(path string) error {
	free, err := usageLogExportDiskFree(path)
	if err != nil {
		return err
	}
	if free < usageLogExportMinFreeDiskBytes {
		return errors.New("insufficient free disk space")
	}
	return nil
}

type backgroundUsageLogExportWriter interface {
	Append(logs []*model.Log) error
	Finish() error
	Abort()
}

func newBackgroundUsageLogExportWriter(path string, request usageLogExportRequest, total int64) (backgroundUsageLogExportWriter, error) {
	target, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	if request.Format == "csv" {
		writer := &backgroundUsageLogCSVWriter{target: target, writer: csv.NewWriter(target)}
		if _, err = target.Write([]byte{0xEF, 0xBB, 0xBF}); err == nil {
			err = writer.writer.Write(usageLogExportHeaders)
		}
		if err != nil {
			writer.Abort()
			return nil, err
		}
		return writer, nil
	}

	writer, err := newBackgroundUsageLogXLSXWriter(target, request, total)
	if err != nil {
		_ = target.Close()
		_ = os.Remove(path)
		return nil, err
	}
	return writer, nil
}

type backgroundUsageLogCSVWriter struct {
	target *os.File
	writer *csv.Writer
}

func (writer *backgroundUsageLogCSVWriter) Append(logs []*model.Log) error {
	for _, log := range logs {
		row, _, _ := usageLogExportRow(log)
		for index := range row {
			row[index] = sanitizeSpreadsheetText(row[index])
		}
		if err := writer.writer.Write(row); err != nil {
			return err
		}
	}
	return nil
}

func (writer *backgroundUsageLogCSVWriter) Finish() error {
	writer.writer.Flush()
	if err := writer.writer.Error(); err != nil {
		_ = writer.target.Close()
		return err
	}
	if err := writer.target.Sync(); err != nil {
		_ = writer.target.Close()
		return err
	}
	return writer.target.Close()
}

func (writer *backgroundUsageLogCSVWriter) Abort() {
	_ = writer.target.Close()
}

type backgroundUsageLogXLSXWriter struct {
	target          *os.File
	workbook        *excelize.File
	detailsWriter   *excelize.StreamWriter
	componentWriter *excelize.StreamWriter
	request         usageLogExportRequest
	total           int64
	detailsRow      int
	componentRow    int
}

func newBackgroundUsageLogXLSXWriter(target *os.File, request usageLogExportRequest, total int64) (*backgroundUsageLogXLSXWriter, error) {
	workbook := excelize.NewFile(excelize.Options{TmpDir: filepath.Dir(target.Name())})
	detailsSheet := "日志明细"
	if err := workbook.SetSheetName(workbook.GetSheetName(0), detailsSheet); err != nil {
		workbook.Close()
		return nil, err
	}
	if _, err := workbook.NewSheet("计价组成"); err != nil {
		workbook.Close()
		return nil, err
	}
	if _, err := workbook.NewSheet("导出说明"); err != nil {
		workbook.Close()
		return nil, err
	}
	detailsWriter, err := workbook.NewStreamWriter(detailsSheet)
	if err != nil {
		workbook.Close()
		return nil, err
	}
	componentWriter, err := workbook.NewStreamWriter("计价组成")
	if err != nil {
		workbook.Close()
		return nil, err
	}
	if err = detailsWriter.SetRow("A1", stringInterfaces(usageLogExportHeaders)); err != nil {
		workbook.Close()
		return nil, err
	}
	componentHeaders := []string{"Request ID", "组成类型", "数量", "单位", "请求时单价(USD)", "价格单位", "倍率", "小计额度", "备注"}
	if err = componentWriter.SetRow("A1", stringInterfaces(componentHeaders)); err != nil {
		workbook.Close()
		return nil, err
	}
	return &backgroundUsageLogXLSXWriter{
		target: target, workbook: workbook, detailsWriter: detailsWriter,
		componentWriter: componentWriter, request: request, total: total,
		detailsRow: 2, componentRow: 2,
	}, nil
}

func (writer *backgroundUsageLogXLSXWriter) Append(logs []*model.Log) error {
	for _, log := range logs {
		row, snapshot, _ := usageLogExportRow(log)
		if err := writer.detailsWriter.SetRow("A"+strconv.Itoa(writer.detailsRow), stringInterfaces(row)); err != nil {
			return err
		}
		writer.detailsRow++
		if snapshot == nil {
			continue
		}
		for _, component := range snapshot.Components {
			values := []interface{}{
				sanitizeSpreadsheetText(log.RequestId), component.Kind, component.Quantity,
				component.Unit, component.UnitPriceUSD, component.PriceUnit, component.Ratio,
				component.SubtotalQuota, sanitizeSpreadsheetText(component.Note),
			}
			if err := writer.componentWriter.SetRow("A"+strconv.Itoa(writer.componentRow), values); err != nil {
				return err
			}
			writer.componentRow++
		}
	}
	return nil
}

func (writer *backgroundUsageLogXLSXWriter) Finish() error {
	if err := writer.detailsWriter.Flush(); err != nil {
		writer.Abort()
		return err
	}
	if err := writer.componentWriter.Flush(); err != nil {
		writer.Abort()
		return err
	}
	noteRows := [][]interface{}{
		{"字段", "值"},
		{"导出时间", time.Now().Format(time.RFC3339)},
		{"导出范围", writer.request.SelectionMode},
		{"IP显示方式", writer.request.IPVisibility},
		{"总记录数", writer.total},
		{"计价快照版本", model.BillingSnapshotVersion},
		{"HUICHUAN-AI版本", common.Version},
	}
	for index, row := range noteRows {
		if err := writer.workbook.SetSheetRow("导出说明", fmt.Sprintf("A%d", index+1), &row); err != nil {
			writer.Abort()
			return err
		}
	}
	if _, err := writer.workbook.WriteTo(writer.target); err != nil {
		writer.Abort()
		return err
	}
	if err := writer.target.Sync(); err != nil {
		writer.Abort()
		return err
	}
	if err := writer.target.Close(); err != nil {
		writer.workbook.Close()
		return err
	}
	return writer.workbook.Close()
}

func (writer *backgroundUsageLogXLSXWriter) Abort() {
	_ = writer.workbook.Close()
	_ = writer.target.Close()
}

func startUsageLogExportCleaner() {
	if !common.IsMasterNode {
		return
	}
	usageLogExportCleanerOnce.Do(func() {
		go func() {
			cleanupExpiredUsageLogExports(common.GetTimestamp())
			ticker := time.NewTicker(time.Hour)
			defer ticker.Stop()
			for range ticker.C {
				cleanupExpiredUsageLogExports(common.GetTimestamp())
			}
		}()
	})
}

func cleanupExpiredUsageLogExports(now int64) {
	_ = model.ExpireStaleSystemTaskLocks(now)
	cleanupOrphanedUsageLogExportDirectories()
	retentionSeconds := int64(common.UsageLogExportRetentionHours() * 3600)
	tasks, err := model.ListCompletedSystemTasksByTypeBefore(
		model.SystemTaskTypeUsageLogExport,
		now-retentionSeconds,
		100,
	)
	if err != nil {
		return
	}
	for _, task := range tasks {
		state := usageLogExportTaskState{}
		if task.DecodeState(&state) != nil || state.Phase == usageLogExportPhaseExpired {
			continue
		}
		if state.ExpiresAt == 0 || state.ExpiresAt > now {
			continue
		}
		_ = os.RemoveAll(usageLogExportTaskDir(task.TaskID))
		state.Phase = usageLogExportPhaseExpired
		state.ErrorCode = ""
		_ = model.UpdateCompletedSystemTaskState(task.TaskID, state)
	}
}

func cleanupOrphanedUsageLogExportDirectories() {
	root := filepath.Join(*common.LogDir, "exports", "usage-logs")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		task, err := model.GetSystemTaskByTaskID(entry.Name())
		if err != nil {
			continue
		}
		if task == nil || task.Status == model.SystemTaskStatusFailed {
			_ = os.RemoveAll(filepath.Join(root, entry.Name()))
			continue
		}
		state := usageLogExportTaskState{}
		if task.DecodeState(&state) == nil && state.Phase == usageLogExportPhaseExpired {
			_ = os.RemoveAll(filepath.Join(root, entry.Name()))
		}
	}
}
