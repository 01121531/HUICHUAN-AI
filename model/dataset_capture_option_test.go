package model

import (
	"encoding/json"
	"testing"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/setting/dataset_capture_setting"
)

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
	keys := []string{"DatasetCaptureEnabled", "DatasetCaptureModelMode", "DatasetCaptureModels"}
	if err := DB.Where("key IN ?", keys).Delete(&Option{}).Error; err != nil {
		t.Fatal(err)
	}
	common.OptionMapRWMutex.Lock()
	originalMap := common.OptionMap
	common.OptionMap = map[string]string{
		"DatasetCaptureEnabled":   "false",
		"DatasetCaptureModelMode": dataset_capture_setting.ModelModeAll,
		"DatasetCaptureModels":    "[]",
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

	want := dataset_capture_setting.Policy{
		Enabled:   true,
		ModelMode: dataset_capture_setting.ModelModeSelected,
		Models:    []string{"claude-sonnet-4", "gpt-5.2"},
	}
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
}
