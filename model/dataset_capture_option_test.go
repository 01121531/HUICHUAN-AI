package model

import (
	"encoding/json"
	"testing"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/setting/dataset_capture_setting"
)

func TestDatasetCapturePolicyFromEnvironmentIncludesPerformanceAndAlertSettings(t *testing.T) {
	t.Setenv("DATASET_CAPTURE_ENABLED", "true")
	t.Setenv("DATASET_CAPTURE_EXPORT_CONCURRENCY", "3")
	t.Setenv("DATASET_CAPTURE_EXPORT_READ_MBPS", "48")
	t.Setenv("DATASET_CAPTURE_ALERT_SILENCE_MINUTES", "17")
	t.Setenv("DATASET_CAPTURE_ALERT_AFTER_DROPS", "4")

	policy := datasetCapturePolicyFromEnvironment()
	if !policy.Enabled {
		t.Fatal("DATASET_CAPTURE_ENABLED was not applied")
	}
	if policy.Performance.ExportConcurrency != 3 || policy.Performance.ExportReadMBps != 48 {
		t.Fatalf("export performance environment settings = %#v", policy.Performance)
	}
	if policy.Alerts.SilenceMinutes != 17 || policy.Alerts.AlertAfterDrops != 4 {
		t.Fatalf("alert environment settings = %#v", policy.Alerts)
	}
}

func TestUpdateOptionMapUpdatesDatasetCaptureRuntimeState(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	originalMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalMap
		common.OptionMapRWMutex.Unlock()
		_ = dataset_capture_setting.Apply(dataset_capture_setting.Policy{ModelMode: dataset_capture_setting.ModelModeAll})
	})

	if err := updateOptionMap("DatasetCaptureEnabled", "true"); err != nil {
		t.Fatal(err)
	}
	if !dataset_capture_setting.IsEnabled() {
		t.Fatal("dataset capture enabled state was not updated")
	}
}

func TestUpdateDatasetCapturePolicyPersistsAndAppliesTogether(t *testing.T) {
	if err := DB.AutoMigrate(&Option{}); err != nil {
		t.Fatal(err)
	}
	keys := []string{"DatasetCaptureEnabled", "DatasetCaptureModelMode", "DatasetCaptureModels", "DatasetCapturePolicyV2"}
	if err := DB.Where("key IN ?", keys).Delete(&Option{}).Error; err != nil {
		t.Fatal(err)
	}
	common.OptionMapRWMutex.Lock()
	originalMap := common.OptionMap
	common.OptionMap = map[string]string{
		"DatasetCaptureEnabled":   "false",
		"DatasetCaptureModelMode": dataset_capture_setting.ModelModeAll,
		"DatasetCaptureModels":    "[]",
		"DatasetCapturePolicyV2":  "",
	}
	common.OptionMapRWMutex.Unlock()
	originalPolicy := dataset_capture_setting.Get()
	t.Cleanup(func() {
		_ = DB.Where("key IN ?", keys).Delete(&Option{}).Error
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalMap
		common.OptionMapRWMutex.Unlock()
		_ = dataset_capture_setting.Apply(originalPolicy)
	})

	want := dataset_capture_setting.DefaultPolicy()
	want.Enabled = true
	want.ModelMode = dataset_capture_setting.ModelModeSelected
	want.Models = []string{"claude-sonnet-4", "gpt-5.2"}
	want.Alerts.Access.Enabled = true
	want.Alerts.Recipients = []string{"audit@example.com"}
	want.Alerts.Access.OperatorMode = dataset_capture_setting.ScopeModeSelected
	want.Alerts.Access.OperatorUserIDs = []int{100}
	want.Alerts.Access.OwnerMode = dataset_capture_setting.ScopeModeSelected
	want.Alerts.Access.OwnerUserIDs = []int{200}
	if err := UpdateDatasetCapturePolicy(want); err != nil {
		t.Fatal(err)
	}
	if got := dataset_capture_setting.Get(); got.Enabled != want.Enabled || got.ModelMode != want.ModelMode || len(got.Models) != 2 {
		t.Fatalf("runtime policy = %#v, want %#v", got, want)
	}

	var options []Option
	if err := DB.Where("key IN ?", keys).Find(&options).Error; err != nil {
		t.Fatal(err)
	}
	if len(options) != len(keys) {
		t.Fatalf("persisted options = %d, want %d", len(options), len(keys))
	}
	values := make(map[string]string, len(options))
	for _, option := range options {
		values[option.Key] = option.Value
	}
	var persistedModels []string
	if err := json.Unmarshal([]byte(values["DatasetCaptureModels"]), &persistedModels); err != nil {
		t.Fatal(err)
	}
	if values["DatasetCaptureEnabled"] != "true" || values["DatasetCaptureModelMode"] != "selected" || len(persistedModels) != 2 {
		t.Fatalf("persisted policy = %#v", values)
	}
	persistedPolicy, err := dataset_capture_setting.ParseJSON(values["DatasetCapturePolicyV2"])
	if err != nil {
		t.Fatal(err)
	}
	if persistedPolicy.Version != dataset_capture_setting.CurrentVersion ||
		persistedPolicy.Performance.QueueSize != 1024 ||
		!persistedPolicy.Alerts.Access.Enabled ||
		len(persistedPolicy.Alerts.Access.OperatorUserIDs) != 1 ||
		len(persistedPolicy.Alerts.Access.OwnerUserIDs) != 1 {
		t.Fatalf("persisted v2 policy = %#v", persistedPolicy)
	}
}
