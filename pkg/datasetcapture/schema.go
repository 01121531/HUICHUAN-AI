package datasetcapture

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

var schemaRootKeys = []string{
	"_meta", "created_at", "cwd", "messages", "model", "response",
	"session_id", "system_prompt", "tools", "user_agent", "user_id_hash",
}

var schemaMetaKeys = []string{
	"is_cold_start_simulation", "raw_finish_reason", "snapshots_in_session",
	"source_file", "source_route", "source_row", "system_prompt_source",
	"user_query", "v",
}

var schemaResponseKeys = []string{"content", "stop_reason", "tool_use", "usage"}
var schemaUsageKeys = []string{"cache", "input_tokens", "output_tokens"}
var schemaToolUseKeys = []string{"calls", "input_already_merged"}
var schemaCacheKeys = []string{"cache_creation", "cache_creation_input_tokens", "cache_read_input_tokens"}
var schemaCacheCreationKeys = []string{"ephemeral_1h_input_tokens", "ephemeral_5m_input_tokens"}

func ValidateJSONLine(line []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if !reflect.DeepEqual(sortedSchemaKeys(raw), schemaRootKeys) {
		return fmt.Errorf("unexpected root fields: %v", sortedSchemaKeys(raw))
	}
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(raw["_meta"], &meta); err != nil {
		return fmt.Errorf("invalid _meta: %w", err)
	}
	if !reflect.DeepEqual(sortedSchemaKeys(meta), schemaMetaKeys) {
		return fmt.Errorf("unexpected _meta fields: %v", sortedSchemaKeys(meta))
	}
	if err := validateSchemaNestedKeys(raw["response"], schemaResponseKeys, "response"); err != nil {
		return err
	}
	var response map[string]json.RawMessage
	_ = json.Unmarshal(raw["response"], &response)
	if err := validateSchemaNestedKeys(response["usage"], schemaUsageKeys, "response.usage"); err != nil {
		return err
	}
	if err := validateSchemaNestedKeys(response["tool_use"], schemaToolUseKeys, "response.tool_use"); err != nil {
		return err
	}
	var usage map[string]json.RawMessage
	_ = json.Unmarshal(response["usage"], &usage)
	if err := validateSchemaNestedKeys(usage["cache"], schemaCacheKeys, "response.usage.cache"); err != nil {
		return err
	}
	var cache map[string]json.RawMessage
	_ = json.Unmarshal(usage["cache"], &cache)
	if err := validateSchemaNestedKeys(cache["cache_creation"], schemaCacheCreationKeys, "response.usage.cache.cache_creation"); err != nil {
		return err
	}
	var record Record
	if err := json.Unmarshal(line, &record); err != nil {
		return fmt.Errorf("invalid schema: %w", err)
	}
	if err := Validate(record); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	return nil
}

func validateSchemaNestedKeys(data json.RawMessage, expected []string, name string) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return fmt.Errorf("invalid %s: %w", name, err)
	}
	if got := sortedSchemaKeys(object); !reflect.DeepEqual(got, expected) {
		return fmt.Errorf("unexpected %s fields: %v", name, got)
	}
	return nil
}

func sortedSchemaKeys[T any](value map[string]T) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
