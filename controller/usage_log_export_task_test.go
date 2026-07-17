package controller

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func setupUsageLogExportTaskTest(t *testing.T) {
	t.Helper()
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalLogDir := *common.LogDir
	originalDirectLimit := common.UsageLogExportDirectLimit()
	originalMaxRows := common.UsageLogExportMaxRows()
	originalBatchSize := common.UsageLogExportBatchSize()
	originalRetention := common.UsageLogExportRetentionHours()
	originalDiskFree := usageLogExportDiskFree

	db, err := gorm.Open(sqlite.Open("file:usage-log-export-task?mode=memory&cache=shared"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Log{},
		&model.Channel{},
		&model.SystemTask{},
		&model.SystemTaskLock{},
	))
	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	tempDir := t.TempDir()
	*common.LogDir = tempDir
	require.True(t, common.SetUsageLogExportBatchSize(100))

	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		*common.LogDir = originalLogDir
		common.SetUsageLogExportDirectLimit(int64(originalDirectLimit))
		common.SetUsageLogExportMaxRows(originalMaxRows)
		common.SetUsageLogExportBatchSize(int64(originalBatchSize))
		common.SetUsageLogExportRetentionHours(int64(originalRetention))
		usageLogExportDiskFree = originalDiskFree
	})
}

func createClaimedUsageLogExportTask(t *testing.T, payload usageLogExportTaskPayload) (*model.SystemTask, string) {
	t.Helper()
	task, err := model.CreateQueuedSystemTask(
		model.SystemTaskTypeUsageLogExport,
		payload,
		usageLogExportTaskState{Phase: usageLogExportPhasePending, Format: payload.Request.Format},
	)
	require.NoError(t, err)
	runnerID := "test-runner"
	claimed, ok, err := model.ClaimSystemTask(
		task.ID,
		model.SystemTaskTypeUsageLogExport,
		runnerID,
		common.GetTimestamp()+60,
	)
	require.NoError(t, err)
	require.True(t, ok)
	return claimed, runnerID
}

func TestUsageLogExportTaskWritesVerifiedCSVAndExpires(t *testing.T) {
	setupUsageLogExportTaskTest(t)
	for index := 0; index < 3; index++ {
		require.NoError(t, model.LOG_DB.Create(&model.Log{
			UserId:    1,
			CreatedAt: int64(100 + index),
			Type:      model.LogTypeConsume,
			Username:  "alice",
			RequestId: "request-" + string(rune('a'+index)),
			Ip:        "192.168.31.102",
		}).Error)
	}
	payload := usageLogExportTaskPayload{
		Scope: "self",
		Request: usageLogExportRequest{
			SelectionMode: "filtered",
			Format:        "csv",
			IPVisibility:  "full",
		},
		Filters: model.UsageLogExportFilter{UserID: 1},
	}
	task, runnerID := createClaimedUsageLogExportTask(t, payload)

	usageLogExportTaskHandler{}.Run(context.Background(), task, runnerID)

	finished, err := model.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.Equal(t, model.SystemTaskStatusSucceeded, finished.Status)
	state := usageLogExportTaskState{}
	require.NoError(t, finished.DecodeState(&state))
	require.Equal(t, usageLogExportPhaseSucceeded, state.Phase)
	require.Equal(t, int64(3), state.Processed)
	require.Equal(t, 100, state.Progress)

	path := filepath.Join(usageLogExportTaskDir(task.TaskID), state.FileID+".csv")
	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
	}
	hash, size, err := hashUsageLogExportFile(path)
	require.NoError(t, err)
	assert.Equal(t, state.SHA256, hash)
	assert.Equal(t, state.FileSize, size)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}))
	rows, err := csv.NewReader(bytes.NewReader(raw[3:])).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 4)
	assert.Equal(t, "192.168.31.102", rows[1][11])

	state.ExpiresAt = common.GetTimestamp() - 1
	require.NoError(t, model.UpdateCompletedSystemTaskState(task.TaskID, state))
	expireUsageLogExportTaskIfNeeded(finished, &state)
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err))
	assert.Equal(t, usageLogExportPhaseExpired, state.Phase)
}

func TestUsageLogExportTaskFailsWhenRowLimitExceeded(t *testing.T) {
	setupUsageLogExportTaskTest(t)
	require.True(t, common.SetUsageLogExportMaxRows(1))
	for index := 0; index < 2; index++ {
		require.NoError(t, model.LOG_DB.Create(&model.Log{
			UserId: 1, CreatedAt: int64(index + 1), Type: model.LogTypeConsume,
			RequestId: "limit-" + string(rune('a'+index)),
		}).Error)
	}
	payload := usageLogExportTaskPayload{
		Request: usageLogExportRequest{SelectionMode: "filtered", Format: "csv"},
		Filters: model.UsageLogExportFilter{UserID: 1},
	}
	task, runnerID := createClaimedUsageLogExportTask(t, payload)

	usageLogExportTaskHandler{}.Run(context.Background(), task, runnerID)

	finished, err := model.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, model.SystemTaskStatusFailed, finished.Status)
	state := usageLogExportTaskState{}
	require.NoError(t, finished.DecodeState(&state))
	assert.Equal(t, "row_limit_exceeded", state.ErrorCode)
	_, err = os.Stat(usageLogExportTaskDir(task.TaskID))
	assert.True(t, os.IsNotExist(err))
}

func TestUsageLogExportTaskWritesParseableXLSX(t *testing.T) {
	setupUsageLogExportTaskTest(t)
	require.NoError(t, model.LOG_DB.Create(&model.Log{
		UserId: 1, CreatedAt: 1, Type: model.LogTypeConsume,
		RequestId: "xlsx-request", Username: "alice",
	}).Error)
	payload := usageLogExportTaskPayload{
		Request: usageLogExportRequest{SelectionMode: "filtered", Format: "xlsx", IPVisibility: "full"},
		Filters: model.UsageLogExportFilter{UserID: 1},
	}
	task, runnerID := createClaimedUsageLogExportTask(t, payload)

	usageLogExportTaskHandler{}.Run(context.Background(), task, runnerID)

	finished, err := model.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.Equal(t, model.SystemTaskStatusSucceeded, finished.Status)
	state := usageLogExportTaskState{}
	require.NoError(t, finished.DecodeState(&state))
	path := filepath.Join(usageLogExportTaskDir(task.TaskID), state.FileID+".xlsx")
	workbook, err := excelize.OpenFile(path)
	require.NoError(t, err)
	defer workbook.Close()
	for _, sheet := range []string{"日志明细", "计价组成", "导出说明"} {
		index, sheetErr := workbook.GetSheetIndex(sheet)
		require.NoError(t, sheetErr)
		assert.NotEqual(t, -1, index)
	}
}

func TestUsageLogExportTaskExportsOneHundredThousandRows(t *testing.T) {
	setupUsageLogExportTaskTest(t)
	require.True(t, common.SetUsageLogExportMaxRows(100000))
	require.True(t, common.SetUsageLogExportBatchSize(1000))
	usageLogExportDiskFree = func(string) (uint64, error) {
		return usageLogExportMinFreeDiskBytes * 10, nil
	}

	const total = 100000
	sqlDB, err := model.LOG_DB.DB()
	require.NoError(t, err)
	tx, err := sqlDB.Begin()
	require.NoError(t, err)
	statement, err := tx.Prepare("INSERT INTO logs (user_id, created_at, type, request_id) VALUES (?, ?, ?, ?)")
	require.NoError(t, err)
	for index := 0; index < total; index++ {
		_, err = statement.Exec(1, index+1, model.LogTypeConsume, fmt.Sprintf("large-%06d", index))
		require.NoError(t, err)
	}
	require.NoError(t, statement.Close())
	require.NoError(t, tx.Commit())
	payload := usageLogExportTaskPayload{
		Request: usageLogExportRequest{SelectionMode: "filtered", Format: "csv", IPVisibility: "full"},
		Filters: model.UsageLogExportFilter{UserID: 1},
	}
	task, runnerID := createClaimedUsageLogExportTask(t, payload)

	usageLogExportTaskHandler{}.Run(context.Background(), task, runnerID)

	finished, err := model.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.Equal(t, model.SystemTaskStatusSucceeded, finished.Status)
	state := usageLogExportTaskState{}
	require.NoError(t, finished.DecodeState(&state))
	require.Equal(t, int64(total), state.Processed)
	path := filepath.Join(usageLogExportTaskDir(task.TaskID), state.FileID+".csv")
	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()
	bom := make([]byte, 3)
	_, err = io.ReadFull(file, bom)
	require.NoError(t, err)
	require.Equal(t, []byte{0xEF, 0xBB, 0xBF}, bom)
	reader := csv.NewReader(file)
	rowCount := 0
	for {
		_, err = reader.Read()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		rowCount++
	}
	assert.Equal(t, total+1, rowCount)
}

func TestUsageLogExportTaskFailsWithoutLeavingFilesWhenDiskIsLow(t *testing.T) {
	setupUsageLogExportTaskTest(t)
	usageLogExportDiskFree = func(string) (uint64, error) {
		return usageLogExportMinFreeDiskBytes - 1, nil
	}
	require.NoError(t, model.LOG_DB.Create(&model.Log{
		UserId: 1, CreatedAt: 1, Type: model.LogTypeConsume, RequestId: "disk-low",
	}).Error)
	payload := usageLogExportTaskPayload{
		Request: usageLogExportRequest{SelectionMode: "filtered", Format: "csv"},
		Filters: model.UsageLogExportFilter{UserID: 1},
	}
	task, runnerID := createClaimedUsageLogExportTask(t, payload)

	usageLogExportTaskHandler{}.Run(context.Background(), task, runnerID)

	finished, err := model.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.Equal(t, model.SystemTaskStatusFailed, finished.Status)
	state := usageLogExportTaskState{}
	require.NoError(t, finished.DecodeState(&state))
	assert.Equal(t, "insufficient_disk", state.ErrorCode)
	_, err = os.Stat(usageLogExportTaskDir(task.TaskID))
	assert.True(t, os.IsNotExist(err))
}

func TestUsageLogExportTaskAuthorizationUsesCurrentDatabaseRole(t *testing.T) {
	setupUsageLogExportTaskTest(t)
	user := model.User{Username: "downgraded", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&user).Error)
	payload := usageLogExportTaskPayload{
		CreatorID:   user.Id,
		CreatorRole: common.RoleAdminUser,
		Scope:       "admin",
		Request:     usageLogExportRequest{Format: "csv"},
	}
	task, err := model.CreateQueuedSystemTask(
		model.SystemTaskTypeUsageLogExport,
		payload,
		usageLogExportTaskState{Phase: usageLogExportPhasePending},
	)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/log/export/"+task.TaskID, nil)
	ctx.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	ctx.Set("id", user.Id)
	ctx.Set("role", common.RoleAdminUser)

	_, _, _, ok := loadAuthorizedUsageLogExportTask(ctx, false)
	assert.False(t, ok)
	assert.Equal(t, http.StatusForbidden, recorder.Code)
}
