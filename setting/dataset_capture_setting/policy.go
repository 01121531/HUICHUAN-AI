package dataset_capture_setting

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
)

const (
	ModelModeAll      = "all"
	ModelModeSelected = "selected"
)

type Policy struct {
	Enabled   bool     `json:"enabled"`
	ModelMode string   `json:"model_mode"`
	Models    []string `json:"models"`
}

type runtimePolicy struct {
	policy Policy
	models map[string]struct{}
}

var current atomic.Value

func init() {
	current.Store(runtimePolicy{
		policy: Policy{ModelMode: ModelModeAll, Models: []string{}},
		models: map[string]struct{}{},
	})
}

func Normalize(policy Policy) (Policy, error) {
	mode := strings.TrimSpace(policy.ModelMode)
	if mode == "" {
		mode = ModelModeAll
	}
	if mode != ModelModeAll && mode != ModelModeSelected {
		return Policy{}, fmt.Errorf("invalid dataset capture model mode %q", mode)
	}

	seen := make(map[string]struct{}, len(policy.Models))
	models := make([]string, 0, len(policy.Models))
	for _, rawModel := range policy.Models {
		model := strings.TrimSpace(rawModel)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		models = append(models, model)
	}
	sort.Strings(models)
	if mode == ModelModeSelected && len(models) == 0 {
		return Policy{}, fmt.Errorf("selected dataset capture model mode requires at least one model")
	}

	return Policy{Enabled: policy.Enabled, ModelMode: mode, Models: models}, nil
}

func Apply(policy Policy) error {
	normalized, err := Normalize(policy)
	if err != nil {
		return err
	}
	models := make(map[string]struct{}, len(normalized.Models))
	for _, model := range normalized.Models {
		models[model] = struct{}{}
	}
	current.Store(runtimePolicy{policy: normalized, models: models})
	return nil
}

func Get() Policy {
	runtime := current.Load().(runtimePolicy)
	policy := runtime.policy
	policy.Models = append(make([]string, 0, len(runtime.policy.Models)), runtime.policy.Models...)
	return policy
}

func IsEnabled() bool {
	return current.Load().(runtimePolicy).policy.Enabled
}

func AllowsModel(model string) bool {
	runtime := current.Load().(runtimePolicy)
	if !runtime.policy.Enabled {
		return false
	}
	if runtime.policy.ModelMode == ModelModeAll {
		return true
	}
	_, allowed := runtime.models[strings.TrimSpace(model)]
	return allowed
}

func SetEnabled(enabled bool) error {
	policy := Get()
	policy.Enabled = enabled
	return Apply(policy)
}

func Parse(enabledValue, modeValue, modelsValue string) (Policy, error) {
	enabled, err := strconv.ParseBool(strings.TrimSpace(enabledValue))
	if err != nil {
		return Policy{}, fmt.Errorf("invalid dataset capture enabled value: %w", err)
	}
	models := make([]string, 0)
	if strings.TrimSpace(modelsValue) != "" {
		if err := json.Unmarshal([]byte(modelsValue), &models); err != nil {
			return Policy{}, fmt.Errorf("invalid dataset capture models: %w", err)
		}
	}
	return Normalize(Policy{Enabled: enabled, ModelMode: modeValue, Models: models})
}
