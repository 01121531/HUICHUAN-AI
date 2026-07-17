package common

import "sync/atomic"

const (
	DefaultUsageLogExportDirectLimit    = int64(5000)
	DefaultUsageLogExportMaxRows        = int64(100000)
	DefaultUsageLogExportBatchSize      = int64(1000)
	DefaultUsageLogExportRetentionHours = int64(24)
)

var (
	usageLogExportDirectLimit    atomic.Int64
	usageLogExportMaxRows        atomic.Int64
	usageLogExportBatchSize      atomic.Int64
	usageLogExportRetentionHours atomic.Int64
)

func init() {
	usageLogExportDirectLimit.Store(DefaultUsageLogExportDirectLimit)
	usageLogExportMaxRows.Store(DefaultUsageLogExportMaxRows)
	usageLogExportBatchSize.Store(DefaultUsageLogExportBatchSize)
	usageLogExportRetentionHours.Store(DefaultUsageLogExportRetentionHours)
}

func UsageLogExportDirectLimit() int {
	return int(usageLogExportDirectLimit.Load())
}

func UsageLogExportMaxRows() int64 {
	return usageLogExportMaxRows.Load()
}

func UsageLogExportBatchSize() int {
	return int(usageLogExportBatchSize.Load())
}

func UsageLogExportRetentionHours() int {
	return int(usageLogExportRetentionHours.Load())
}

func SetUsageLogExportDirectLimit(value int64) bool {
	if value < 1 || value > 50000 {
		return false
	}
	usageLogExportDirectLimit.Store(value)
	return true
}

func SetUsageLogExportMaxRows(value int64) bool {
	if value < 0 {
		return false
	}
	usageLogExportMaxRows.Store(value)
	return true
}

func SetUsageLogExportBatchSize(value int64) bool {
	if value < 100 || value > 5000 {
		return false
	}
	usageLogExportBatchSize.Store(value)
	return true
}

func SetUsageLogExportRetentionHours(value int64) bool {
	if value < 1 || value > 720 {
		return false
	}
	usageLogExportRetentionHours.Store(value)
	return true
}
