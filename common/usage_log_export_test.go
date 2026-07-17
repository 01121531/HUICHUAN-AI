package common

import "testing"

func TestUsageLogExportSettingsValidateBounds(t *testing.T) {
	originalDirect := UsageLogExportDirectLimit()
	originalMax := UsageLogExportMaxRows()
	originalBatch := UsageLogExportBatchSize()
	originalRetention := UsageLogExportRetentionHours()
	t.Cleanup(func() {
		SetUsageLogExportDirectLimit(int64(originalDirect))
		SetUsageLogExportMaxRows(originalMax)
		SetUsageLogExportBatchSize(int64(originalBatch))
		SetUsageLogExportRetentionHours(int64(originalRetention))
	})

	if SetUsageLogExportDirectLimit(0) || SetUsageLogExportDirectLimit(50001) {
		t.Fatal("invalid direct limit was accepted")
	}
	if !SetUsageLogExportDirectLimit(5000) {
		t.Fatal("valid direct limit was rejected")
	}
	if SetUsageLogExportMaxRows(-1) || !SetUsageLogExportMaxRows(0) {
		t.Fatal("maximum row limit validation failed")
	}
	if SetUsageLogExportBatchSize(99) || SetUsageLogExportBatchSize(5001) || !SetUsageLogExportBatchSize(1000) {
		t.Fatal("batch size validation failed")
	}
	if SetUsageLogExportRetentionHours(0) || SetUsageLogExportRetentionHours(721) || !SetUsageLogExportRetentionHours(24) {
		t.Fatal("retention validation failed")
	}
}
