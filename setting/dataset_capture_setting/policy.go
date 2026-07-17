package dataset_capture_setting

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
)

const (
	CurrentVersion    = 2
	ModelModeAll      = "all"
	ModelModeSelected = "selected"
	ScopeModeAll      = "all"
	ScopeModeSelected = "selected"

	AccessActionView     = "view"
	AccessActionDownload = "download"
)

var allowedAlertTypes = map[string]struct{}{
	"queue_full": {}, "inflight_bytes_exceeded": {}, "sample_too_large": {},
	"disk_low": {}, "disk_limit_reached": {}, "jsonl_write_failed": {},
	"index_write_failed": {}, "worker_panic": {},
	"spool_write_failed":   {},
	"usage_log_queue_full": {}, "usage_log_write_failed": {},
}

var defaultAlertTypes = []string{
	"disk_limit_reached", "disk_low", "index_write_failed", "inflight_bytes_exceeded",
	"jsonl_write_failed", "queue_full", "sample_too_large", "spool_write_failed",
	"usage_log_queue_full", "usage_log_write_failed", "worker_panic",
}

type PerformancePolicy struct {
	QueueSize            int `json:"queue_size"`
	Workers              int `json:"workers"`
	BufferSegmentKB      int `json:"buffer_segment_kb"`
	MaxSampleMB          int `json:"max_sample_mb"`
	MaxInFlightMB        int `json:"max_inflight_mb"`
	SpoolThresholdMB     int `json:"spool_threshold_mb"`
	IndexQueueSize       int `json:"index_queue_size"`
	IndexBatchSize       int `json:"index_batch_size"`
	IndexFlushIntervalMS int `json:"index_flush_interval_ms"`
	MinFreeDiskGB        int `json:"min_free_disk_gb"`
	MaxDiskGB            int `json:"max_disk_gb"`
	ExportConcurrency    int `json:"export_concurrency"`
	ExportReadMBps       int `json:"export_read_mbps"`
}

type AlertPolicy struct {
	Enabled         bool              `json:"enabled"`
	Recipients      []string          `json:"recipients"`
	Types           []string          `json:"types"`
	SilenceMinutes  int               `json:"silence_minutes"`
	AlertAfterDrops int               `json:"alert_after_drops"`
	SendRecovery    bool              `json:"send_recovery"`
	Access          AccessAlertPolicy `json:"access"`
}

type AccessAlertPolicy struct {
	Enabled         bool     `json:"enabled"`
	Actions         []string `json:"actions"`
	OperatorMode    string   `json:"operator_mode"`
	OperatorUserIDs []int    `json:"operator_user_ids"`
	OwnerMode       string   `json:"owner_mode"`
	OwnerUserIDs    []int    `json:"owner_user_ids"`
}

type Policy struct {
	Version                  int               `json:"version"`
	Enabled                  bool              `json:"enabled"`
	ModelMode                string            `json:"model_mode"`
	Models                   []string          `json:"models"`
	UserMode                 string            `json:"user_mode"`
	UserIDs                  []int             `json:"user_ids"`
	TokenMode                string            `json:"token_mode"`
	TokenIDs                 []int             `json:"token_ids"`
	CaptureStream            bool              `json:"capture_stream"`
	PreserveMultimodalBase64 bool              `json:"preserve_multimodal_base64"`
	Performance              PerformancePolicy `json:"performance"`
	Alerts                   AlertPolicy       `json:"alerts"`
}

type runtimePolicy struct {
	policy   Policy
	models   map[string]struct{}
	userIDs  map[int]struct{}
	tokenIDs map[int]struct{}
}

var current atomic.Value

func DefaultPolicy() Policy {
	return Policy{
		Version: CurrentVersion, ModelMode: ModelModeAll, Models: []string{},
		UserMode: ScopeModeAll, UserIDs: []int{}, TokenMode: ScopeModeAll, TokenIDs: []int{},
		CaptureStream: true, PreserveMultimodalBase64: true,
		Performance: PerformancePolicy{
			QueueSize: 1024, Workers: 2, BufferSegmentKB: 64, MaxSampleMB: 100,
			MaxInFlightMB: 512, SpoolThresholdMB: 2, IndexQueueSize: 2048,
			IndexBatchSize: 50, IndexFlushIntervalMS: 1000, MinFreeDiskGB: 2,
			MaxDiskGB: 10, ExportConcurrency: 1, ExportReadMBps: 32,
		},
		Alerts: AlertPolicy{
			Recipients: []string{}, Types: append([]string(nil), defaultAlertTypes...),
			SilenceMinutes: 10, AlertAfterDrops: 1, SendRecovery: true,
			Access: AccessAlertPolicy{
				Actions:      []string{AccessActionDownload, AccessActionView},
				OperatorMode: ScopeModeAll, OperatorUserIDs: []int{},
				OwnerMode: ScopeModeAll, OwnerUserIDs: []int{},
			},
		},
	}
}

func init() {
	_ = Apply(DefaultPolicy())
}

func Normalize(policy Policy) (Policy, error) {
	defaults := DefaultPolicy()
	if policy.Version == 0 {
		policy.Version = CurrentVersion
		policy.CaptureStream = true
		policy.PreserveMultimodalBase64 = true
		policy.Performance = defaults.Performance
		policy.Alerts = defaults.Alerts
	}
	if policy.Version != CurrentVersion {
		return Policy{}, fmt.Errorf("unsupported dataset capture policy version %d", policy.Version)
	}
	policy.ModelMode = defaultString(policy.ModelMode, ModelModeAll)
	policy.UserMode = defaultString(policy.UserMode, ScopeModeAll)
	policy.TokenMode = defaultString(policy.TokenMode, ScopeModeAll)
	if err := validateMode("model", policy.ModelMode); err != nil {
		return Policy{}, err
	}
	if err := validateMode("user", policy.UserMode); err != nil {
		return Policy{}, err
	}
	if err := validateMode("token", policy.TokenMode); err != nil {
		return Policy{}, err
	}
	policy.Models = normalizeStrings(policy.Models)
	policy.UserIDs = normalizePositiveInts(policy.UserIDs)
	policy.TokenIDs = normalizePositiveInts(policy.TokenIDs)
	if policy.ModelMode == ModelModeSelected && len(policy.Models) == 0 {
		return Policy{}, fmt.Errorf("selected dataset capture model mode requires at least one model")
	}
	if policy.UserMode == ScopeModeSelected && len(policy.UserIDs) == 0 {
		return Policy{}, fmt.Errorf("selected dataset capture user mode requires at least one user")
	}
	if policy.TokenMode == ScopeModeSelected && len(policy.TokenIDs) == 0 {
		return Policy{}, fmt.Errorf("selected dataset capture token mode requires at least one token")
	}
	if isZeroPerformance(policy.Performance) {
		policy.Performance = defaults.Performance
	}
	if err := validatePerformance(policy.Performance); err != nil {
		return Policy{}, err
	}
	policy.Alerts.Recipients = normalizeEmails(policy.Alerts.Recipients)
	for _, recipient := range policy.Alerts.Recipients {
		if _, err := mail.ParseAddress(recipient); err != nil {
			return Policy{}, fmt.Errorf("invalid dataset capture alert recipient %q", recipient)
		}
	}
	if policy.Alerts.Types == nil {
		policy.Alerts.Types = append([]string(nil), defaults.Alerts.Types...)
	} else {
		policy.Alerts.Types = normalizeStrings(policy.Alerts.Types)
	}
	for _, alertType := range policy.Alerts.Types {
		if _, ok := allowedAlertTypes[alertType]; !ok {
			return Policy{}, fmt.Errorf("invalid dataset capture alert type %q", alertType)
		}
	}
	if policy.Alerts.SilenceMinutes == 0 {
		policy.Alerts.SilenceMinutes = defaults.Alerts.SilenceMinutes
	}
	if policy.Alerts.AlertAfterDrops == 0 {
		policy.Alerts.AlertAfterDrops = defaults.Alerts.AlertAfterDrops
	}
	if policy.Alerts.SilenceMinutes < 1 || policy.Alerts.SilenceMinutes > 1440 {
		return Policy{}, fmt.Errorf("dataset capture alert silence_minutes must be between 1 and 1440")
	}
	if policy.Alerts.AlertAfterDrops < 1 || policy.Alerts.AlertAfterDrops > 1000000 {
		return Policy{}, fmt.Errorf("dataset capture alert_after_drops must be between 1 and 1000000")
	}
	if policy.Alerts.Access.Actions == nil {
		policy.Alerts.Access.Actions = append([]string(nil), defaults.Alerts.Access.Actions...)
	} else {
		policy.Alerts.Access.Actions = normalizeStrings(policy.Alerts.Access.Actions)
	}
	for _, action := range policy.Alerts.Access.Actions {
		if action != AccessActionView && action != AccessActionDownload {
			return Policy{}, fmt.Errorf("invalid dataset capture access alert action %q", action)
		}
	}
	policy.Alerts.Access.OperatorMode = defaultString(policy.Alerts.Access.OperatorMode, ScopeModeAll)
	policy.Alerts.Access.OwnerMode = defaultString(policy.Alerts.Access.OwnerMode, ScopeModeAll)
	if err := validateMode("access alert operator", policy.Alerts.Access.OperatorMode); err != nil {
		return Policy{}, err
	}
	if err := validateMode("access alert owner", policy.Alerts.Access.OwnerMode); err != nil {
		return Policy{}, err
	}
	policy.Alerts.Access.OperatorUserIDs = normalizePositiveInts(policy.Alerts.Access.OperatorUserIDs)
	policy.Alerts.Access.OwnerUserIDs = normalizePositiveInts(policy.Alerts.Access.OwnerUserIDs)
	if policy.Alerts.Access.Enabled && len(policy.Alerts.Access.Actions) == 0 {
		return Policy{}, fmt.Errorf("enabled dataset capture access alerts require at least one action")
	}
	if policy.Alerts.Access.Enabled && policy.Alerts.Access.OperatorMode == ScopeModeSelected && len(policy.Alerts.Access.OperatorUserIDs) == 0 {
		return Policy{}, fmt.Errorf("selected dataset capture access alert operator mode requires at least one user")
	}
	if policy.Alerts.Access.Enabled && policy.Alerts.Access.OwnerMode == ScopeModeSelected && len(policy.Alerts.Access.OwnerUserIDs) == 0 {
		return Policy{}, fmt.Errorf("selected dataset capture access alert owner mode requires at least one user")
	}
	if (policy.Alerts.Enabled || policy.Alerts.Access.Enabled) && len(policy.Alerts.Recipients) == 0 {
		return Policy{}, fmt.Errorf("enabled dataset capture alerts require at least one recipient")
	}
	return policy, nil
}

func Apply(policy Policy) error {
	normalized, err := Normalize(policy)
	if err != nil {
		return err
	}
	runtime := runtimePolicy{
		policy: normalized, models: make(map[string]struct{}, len(normalized.Models)),
		userIDs: make(map[int]struct{}, len(normalized.UserIDs)), tokenIDs: make(map[int]struct{}, len(normalized.TokenIDs)),
	}
	for _, model := range normalized.Models {
		runtime.models[model] = struct{}{}
	}
	for _, id := range normalized.UserIDs {
		runtime.userIDs[id] = struct{}{}
	}
	for _, id := range normalized.TokenIDs {
		runtime.tokenIDs[id] = struct{}{}
	}
	current.Store(runtime)
	return nil
}

func Get() Policy {
	runtime := current.Load().(runtimePolicy)
	policy := runtime.policy
	policy.Models = append([]string{}, policy.Models...)
	policy.UserIDs = append([]int{}, policy.UserIDs...)
	policy.TokenIDs = append([]int{}, policy.TokenIDs...)
	policy.Alerts.Recipients = append([]string{}, policy.Alerts.Recipients...)
	policy.Alerts.Types = append([]string{}, policy.Alerts.Types...)
	policy.Alerts.Access.Actions = append([]string{}, policy.Alerts.Access.Actions...)
	policy.Alerts.Access.OperatorUserIDs = append([]int{}, policy.Alerts.Access.OperatorUserIDs...)
	policy.Alerts.Access.OwnerUserIDs = append([]int{}, policy.Alerts.Access.OwnerUserIDs...)
	return policy
}

func IsEnabled() bool { return current.Load().(runtimePolicy).policy.Enabled }

func AllowsModel(model string) bool {
	runtime := current.Load().(runtimePolicy)
	return allowsString(runtime.policy.Enabled, runtime.policy.ModelMode, runtime.models, strings.TrimSpace(model))
}

func AllowsRequest(model string, userID, tokenID int, stream bool) bool {
	allowed, _ := RequestCaptureOptions(model, userID, tokenID, stream)
	return allowed
}

// RequestCaptureOptions evaluates the immutable runtime policy without
// cloning its configuration slices. It is intended for the API hot path.
func RequestCaptureOptions(model string, userID, tokenID int, stream bool) (bool, bool) {
	runtime := current.Load().(runtimePolicy)
	if !allowsString(runtime.policy.Enabled, runtime.policy.ModelMode, runtime.models, strings.TrimSpace(model)) {
		return false, runtime.policy.PreserveMultimodalBase64
	}
	if stream && !runtime.policy.CaptureStream {
		return false, runtime.policy.PreserveMultimodalBase64
	}
	if runtime.policy.UserMode == ScopeModeSelected {
		if _, ok := runtime.userIDs[userID]; !ok {
			return false, runtime.policy.PreserveMultimodalBase64
		}
	}
	if runtime.policy.TokenMode == ScopeModeSelected {
		if _, ok := runtime.tokenIDs[tokenID]; !ok {
			return false, runtime.policy.PreserveMultimodalBase64
		}
	}
	return true, runtime.policy.PreserveMultimodalBase64
}

func SetEnabled(enabled bool) error {
	policy := Get()
	policy.Enabled = enabled
	return Apply(policy)
}

func Parse(enabledValue, modeValue, modelsValue string) (Policy, error) {
	policy := DefaultPolicy()
	enabled, err := strconv.ParseBool(strings.TrimSpace(enabledValue))
	if err != nil {
		return Policy{}, fmt.Errorf("invalid dataset capture enabled value: %w", err)
	}
	policy.Enabled = enabled
	policy.ModelMode = modeValue
	if strings.TrimSpace(modelsValue) != "" {
		if err := json.Unmarshal([]byte(modelsValue), &policy.Models); err != nil {
			return Policy{}, fmt.Errorf("invalid dataset capture models: %w", err)
		}
	}
	return Normalize(policy)
}

func ParseJSON(value string) (Policy, error) {
	var policy Policy
	if err := json.Unmarshal([]byte(value), &policy); err != nil {
		return Policy{}, fmt.Errorf("invalid dataset capture policy: %w", err)
	}
	return Normalize(policy)
}

func validateMode(name, mode string) error {
	if mode != ScopeModeAll && mode != ScopeModeSelected {
		return fmt.Errorf("invalid dataset capture %s mode %q", name, mode)
	}
	return nil
}

func validatePerformance(value PerformancePolicy) error {
	ranges := []struct {
		name            string
		value, min, max int
	}{
		{"queue_size", value.QueueSize, 16, 65536}, {"workers", value.Workers, 1, 32},
		{"buffer_segment_kb", value.BufferSegmentKB, 4, 1024}, {"max_sample_mb", value.MaxSampleMB, 1, 1024},
		{"max_inflight_mb", value.MaxInFlightMB, 16, 65536}, {"spool_threshold_mb", value.SpoolThresholdMB, 1, 1024},
		{"index_queue_size", value.IndexQueueSize, 16, 131072}, {"index_batch_size", value.IndexBatchSize, 1, 1000},
		{"index_flush_interval_ms", value.IndexFlushIntervalMS, 100, 60000}, {"min_free_disk_gb", value.MinFreeDiskGB, 0, 10240},
		{"max_disk_gb", value.MaxDiskGB, 0, 1048576}, {"export_concurrency", value.ExportConcurrency, 1, 8},
		{"export_read_mbps", value.ExportReadMBps, 0, 1024},
	}
	for _, item := range ranges {
		if item.value < item.min || item.value > item.max {
			return fmt.Errorf("dataset capture %s must be between %d and %d", item.name, item.min, item.max)
		}
	}
	if value.SpoolThresholdMB > value.MaxSampleMB {
		return fmt.Errorf("dataset capture spool_threshold_mb cannot exceed max_sample_mb")
	}
	return nil
}

func normalizeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func normalizePositiveInts(values []int) []int {
	seen := make(map[int]struct{}, len(values))
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Ints(result)
	return result
}

func normalizeEmails(values []string) []string {
	result := normalizeStrings(values)
	for index := range result {
		result[index] = strings.ToLower(result[index])
	}
	return result
}

func isZeroPerformance(value PerformancePolicy) bool { return value == (PerformancePolicy{}) }
func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
func allowsString(enabled bool, mode string, selected map[string]struct{}, value string) bool {
	if !enabled {
		return false
	}
	if mode == ScopeModeAll {
		return true
	}
	_, ok := selected[value]
	return ok
}
