package model

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/gin-gonic/gin"
)

const (
	usageLogQueueCapacity = 4096
	usageLogWorkerCount   = 2
	deferredUsageLogsKey  = "deferred_usage_log_jobs"
)

type usageLogJob struct {
	Log          *Log
	Other        map[string]interface{}
	QuotaData    *QuotaDataLogParams
	QuotaPerUnit float64
}

type UsageLogQueueStatus struct {
	Depth          int    `json:"depth"`
	Capacity       int    `json:"capacity"`
	Submitted      uint64 `json:"submitted"`
	Written        uint64 `json:"written"`
	Dropped        uint64 `json:"dropped"`
	WriteFailed    uint64 `json:"write_failed"`
	SnapshotFailed uint64 `json:"snapshot_failed"`
	LastError      string `json:"last_error,omitempty"`
}

var (
	usageLogQueue          = make(chan usageLogJob, usageLogQueueCapacity)
	usageLogWorkerOnce     sync.Once
	usageLogSubmitted      atomic.Uint64
	usageLogWritten        atomic.Uint64
	usageLogDropped        atomic.Uint64
	usageLogFailed         atomic.Uint64
	usageLogSnapshotFailed atomic.Uint64
	usageLogActive         atomic.Int64
	usageLogLastError      atomic.Value
	usageLogQueueAlert     atomic.Bool
	usageLogWriteAlert     atomic.Bool
)

func startUsageLogWorkers() {
	usageLogWorkerOnce.Do(func() {
		for index := 0; index < usageLogWorkerCount; index++ {
			go usageLogWorker()
		}
	})
}

func usageLogWorker() {
	for job := range usageLogQueue {
		usageLogActive.Add(1)
		func() {
			defer usageLogActive.Add(-1)
			defer func() {
				if recovered := recover(); recovered != nil {
					recordUsageLogQueueError(fmt.Sprintf("worker panic: %v", recovered))
				}
			}()
			if job.Log == nil {
				return
			}
			if job.Log.Username == "" {
				job.Log.Username, _ = GetUsernameById(job.Log.UserId, false)
			}
			if job.Log.Type == LogTypeConsume {
				snapshot, err := BuildBillingSnapshotV1(job.Log, job.Other, job.QuotaPerUnit)
				if err != nil {
					usageLogSnapshotFailed.Add(1)
					common.SysLog("failed to build usage log billing snapshot: " + err.Error())
				}
				if job.Other == nil {
					job.Other = make(map[string]interface{}, 1)
				}
				job.Other["billing_snapshot_v1"] = snapshot
			}
			job.Log.Other = common.MapToJsonStr(job.Other)
			if err := createLog(job.Log); err != nil {
				recordUsageLogQueueError(err.Error())
				return
			}
			if usageLogWriteAlert.Swap(false) {
				common.NotifyUsageLogAlert(common.UsageLogAlertEvent{
					Type: common.UsageLogAlertWriteFailed, Resolved: true,
				})
			}
			if job.QuotaData != nil && common.DataExportEnabled {
				LogQuotaData(*job.QuotaData)
			}
			usageLogWritten.Add(1)
		}()
	}
}

func recordUsageLogQueueError(message string) {
	usageLogFailed.Add(1)
	usageLogLastError.Store(message)
	usageLogWriteAlert.Store(true)
	common.NotifyUsageLogAlert(common.UsageLogAlertEvent{Type: common.UsageLogAlertWriteFailed})
	common.SysLog("failed to persist asynchronous usage log: " + message)
}

func submitUsageLog(job usageLogJob) bool {
	startUsageLogWorkers()
	if job.QuotaPerUnit <= 0 {
		job.QuotaPerUnit = common.QuotaPerUnit
	}
	select {
	case usageLogQueue <- job:
		usageLogSubmitted.Add(1)
		if usageLogQueueAlert.Swap(false) {
			common.NotifyUsageLogAlert(common.UsageLogAlertEvent{
				Type: common.UsageLogAlertQueueFull, Resolved: true,
			})
		}
		return true
	default:
		usageLogDropped.Add(1)
		usageLogLastError.Store("usage log queue is full")
		usageLogQueueAlert.Store(true)
		common.NotifyUsageLogAlert(common.UsageLogAlertEvent{
			Type: common.UsageLogAlertQueueFull, Dropped: 1,
		})
		common.SysLog("usage log queue is full; dropping non-financial log record")
		return false
	}
}

func deferUsageLog(c *gin.Context, job usageLogJob) {
	job.Other = cloneUsageLogMap(job.Other)
	if job.QuotaPerUnit <= 0 {
		job.QuotaPerUnit = common.QuotaPerUnit
	}
	if c == nil {
		submitUsageLog(job)
		return
	}
	current, _ := c.Get(deferredUsageLogsKey)
	jobs, _ := current.([]usageLogJob)
	jobs = append(jobs, job)
	c.Set(deferredUsageLogsKey, jobs)
}

func cloneUsageLogMap(source map[string]interface{}) map[string]interface{} {
	if source == nil {
		return nil
	}
	target := make(map[string]interface{}, len(source))
	for key, value := range source {
		target[key] = cloneUsageLogValue(value)
	}
	return target
}

func cloneUsageLogValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return cloneUsageLogMap(typed)
	case []interface{}:
		result := make([]interface{}, len(typed))
		for index, item := range typed {
			result[index] = cloneUsageLogValue(item)
		}
		return result
	case []string:
		return append([]string(nil), typed...)
	case []int:
		return append([]int(nil), typed...)
	default:
		return value
	}
}

// FlushDeferredUsageLogs must be called by response middleware after c.Next.
// It only performs non-blocking queue submissions.
func FlushDeferredUsageLogs(c *gin.Context) {
	if c == nil {
		return
	}
	current, exists := c.Get(deferredUsageLogsKey)
	if !exists {
		return
	}
	c.Set(deferredUsageLogsKey, nil)
	jobs, _ := current.([]usageLogJob)
	for _, job := range jobs {
		submitUsageLog(job)
	}
}

func GetUsageLogQueueStatus() UsageLogQueueStatus {
	status := UsageLogQueueStatus{
		Depth:          len(usageLogQueue),
		Capacity:       cap(usageLogQueue),
		Submitted:      usageLogSubmitted.Load(),
		Written:        usageLogWritten.Load(),
		Dropped:        usageLogDropped.Load(),
		WriteFailed:    usageLogFailed.Load(),
		SnapshotFailed: usageLogSnapshotFailed.Load(),
	}
	if value := usageLogLastError.Load(); value != nil {
		status.LastError, _ = value.(string)
	}
	return status
}

func WaitForUsageLogQueue(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(usageLogQueue) == 0 && usageLogActive.Load() == 0 {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return len(usageLogQueue) == 0 && usageLogActive.Load() == 0
}
