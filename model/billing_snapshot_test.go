package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildBillingSnapshotV1PerTokenUsesFrozenValues(t *testing.T) {
	log := &Log{
		ModelName:        "claude-sonnet",
		PromptTokens:     100,
		CompletionTokens: 50,
		Quota:            420,
	}
	other := map[string]interface{}{
		"model_ratio":                1.5,
		"completion_ratio":           5.0,
		"group_ratio":                1.2,
		"cache_tokens":               10,
		"cache_ratio":                0.1,
		"cache_creation_tokens":      7,
		"cache_creation_ratio":       1.25,
		"billing_pre_consumed_quota": 500,
		"billing_source":             "wallet",
		"usage_semantic":             "openai",
		"web_search_call_count":      2,
		"web_search_price":           10.0,
		"upstream_model_name":        "claude-sonnet-effective",
		"billing_other_ratios":       map[string]interface{}{"priority": 1.1},
	}

	snapshot, err := BuildBillingSnapshotV1(log, other, 500000)
	if err != nil {
		t.Fatalf("BuildBillingSnapshotV1 returned error: %v", err)
	}
	if snapshot.Status != "complete" || snapshot.Mode != "per_token" {
		t.Fatalf("unexpected snapshot status/mode: %+v", snapshot)
	}
	if snapshot.RequestedModel != "claude-sonnet" || snapshot.EffectiveModel != "claude-sonnet-effective" {
		t.Fatalf("model mapping was not frozen: %+v", snapshot)
	}
	if snapshot.PreConsumedQuota != 500 || snapshot.SettlementDelta != -80 || snapshot.FinalChargedQuota != 420 {
		t.Fatalf("unexpected settlement data: %+v", snapshot)
	}
	assertBillingComponentKinds(t, snapshot.Components,
		"input_tokens", "output_tokens", "cache_read", "cache_creation",
		"web_search", "other_ratio_adjustment", "settlement_adjustment")

	// Mutating request-time inputs after construction must not change the snapshot.
	other["model_ratio"] = 99.0
	if snapshot.Components[0].UnitPriceUSD != "3" {
		t.Fatalf("snapshot changed or used an unexpected unit price: %s", snapshot.Components[0].UnitPriceUSD)
	}
}

func TestBuildBillingSnapshotV1AnthropicCacheSplit(t *testing.T) {
	log := &Log{ModelName: "claude", PromptTokens: 80, CompletionTokens: 20, Quota: 250}
	other := map[string]interface{}{
		"model_ratio":              1.0,
		"completion_ratio":         5.0,
		"group_ratio":              1.0,
		"usage_semantic":           "anthropic",
		"cache_tokens":             20,
		"cache_ratio":              0.1,
		"cache_creation_tokens":    15,
		"cache_creation_tokens_5m": 10,
		"cache_creation_ratio_5m":  1.25,
		"cache_creation_tokens_1h": 5,
		"cache_creation_ratio_1h":  2.0,
	}
	snapshot, err := BuildBillingSnapshotV1(log, other, 500000)
	if err != nil {
		t.Fatal(err)
	}
	assertBillingComponentKinds(t, snapshot.Components,
		"input_tokens", "output_tokens", "cache_read",
		"cache_creation_5m", "cache_creation_1h", "settlement_adjustment")
}

func TestBuildBillingSnapshotV1Modes(t *testing.T) {
	tests := []struct {
		name  string
		log   *Log
		other map[string]interface{}
		mode  string
		kind  string
	}{
		{
			name:  "per-call",
			log:   &Log{ModelName: "image", Quota: 1000},
			other: map[string]interface{}{"billing_use_price": true, "model_price": 0.002, "group_ratio": 1.0},
			mode:  "per_call",
			kind:  "per_call",
		},
		{
			name:  "subscription",
			log:   &Log{ModelName: "gpt", PromptTokens: 10, CompletionTokens: 3, Quota: 50},
			other: map[string]interface{}{"billing_source": "subscription", "model_ratio": 1.0, "group_ratio": 1.0, "subscription_consumed": 50, "wallet_quota_deducted": 0},
			mode:  "subscription",
			kind:  "input_tokens",
		},
		{
			name:  "free",
			log:   &Log{ModelName: "free", PromptTokens: 10, Quota: 0},
			other: map[string]interface{}{"billing_free_model": true},
			mode:  "free",
		},
		{
			name:  "violation",
			log:   &Log{ModelName: "grok", Quota: 300},
			other: map[string]interface{}{"violation_fee": true, "base_amount": 0.0006, "group_ratio": 1.0},
			mode:  "violation_fee",
			kind:  "violation_fee",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot, err := BuildBillingSnapshotV1(tt.log, tt.other, 500000)
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.Mode != tt.mode {
				t.Fatalf("mode=%s, want %s", snapshot.Mode, tt.mode)
			}
			if tt.kind != "" {
				assertBillingComponentKinds(t, snapshot.Components, tt.kind)
			}
			if tt.mode == "subscription" && (snapshot.SubscriptionQuota != 50 || snapshot.WalletQuota != 0) {
				t.Fatalf("subscription split is incorrect: %+v", snapshot)
			}
		})
	}
}

func TestBuildBillingSnapshotV1TieredDoesNotLeakExpression(t *testing.T) {
	log := &Log{ModelName: "tiered", Quota: 777}
	other := map[string]interface{}{
		"billing_mode": "tiered_expr",
		"tier_version": "v2",
		"tier_hash":    strings.Repeat("a", 64),
		"matched_tier": "long-context",
		"expr_b64":     "c2Vuc2l0aXZlLWV4cHJlc3Npb24=",
		"tier_inputs": map[string]interface{}{
			"actual_quota_before_group": 777.0,
		},
	}
	snapshot, err := BuildBillingSnapshotV1(log, other, 500000)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Mode != "tiered_expr" || snapshot.TierVersion != "v2" || snapshot.MatchedTier != "long-context" {
		t.Fatalf("unexpected tier snapshot: %+v", snapshot)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "c2Vuc2l0aXZl") || strings.Contains(string(encoded), "expr_b64") {
		t.Fatalf("snapshot leaked the pricing expression: %s", encoded)
	}
}

func TestBuildBillingSnapshotV1ClampAndFailure(t *testing.T) {
	log := &Log{ModelName: "gpt", PromptTokens: 1, Quota: 2147483647}
	other := map[string]interface{}{
		"model_ratio": 1.0,
		"group_ratio": 1.0,
		"admin_info": map[string]interface{}{
			"quota_saturation": map[string]interface{}{
				"kind":     "overflow",
				"original": "999999999999",
				"clamped":  2147483647,
			},
		},
	}
	snapshot, err := BuildBillingSnapshotV1(log, other, 500000)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Clamp == nil || !snapshot.Clamp.Applied || snapshot.Clamp.Kind != "overflow" {
		t.Fatalf("clamp not preserved: %+v", snapshot.Clamp)
	}

	failed, err := BuildBillingSnapshotV1(log, other, 0)
	if err == nil || failed.Status != "failed" || failed.ErrorCode != "invalid_quota_per_unit" {
		t.Fatalf("invalid quota must produce a failed snapshot: %+v, %v", failed, err)
	}
}

func TestBuildBillingSnapshotV1AudioAndProtocolSemantics(t *testing.T) {
	protocols := []struct {
		name     string
		semantic string
	}{
		{name: "OpenAI", semantic: "openai"},
		{name: "Anthropic", semantic: "anthropic"},
		{name: "Gemini", semantic: "openai"},
	}
	for _, protocol := range protocols {
		t.Run(protocol.name, func(t *testing.T) {
			snapshot, err := BuildBillingSnapshotV1(
				&Log{ModelName: strings.ToLower(protocol.name), PromptTokens: 100, CompletionTokens: 20, Quota: 200},
				map[string]interface{}{
					"model_ratio":    1.0,
					"group_ratio":    1.0,
					"usage_semantic": protocol.semantic,
					"cache_tokens":   10,
					"cache_ratio":    0.1,
				},
				500000,
			)
			if err != nil {
				t.Fatal(err)
			}
			assertBillingComponentKinds(t, snapshot.Components, "input_tokens", "output_tokens", "cache_read")
		})
	}

	audio, err := BuildBillingSnapshotV1(
		&Log{ModelName: "realtime", PromptTokens: 16, CompletionTokens: 8, Quota: 150},
		map[string]interface{}{
			"model_ratio":            1.0,
			"group_ratio":            1.0,
			"audio":                  true,
			"text_input":             10,
			"text_output":            5,
			"audio_input":            6,
			"audio_output":           3,
			"audio_ratio":            4.0,
			"audio_completion_ratio": 2.0,
		},
		500000,
	)
	if err != nil {
		t.Fatal(err)
	}
	assertBillingComponentKinds(t, audio.Components, "input_tokens", "output_tokens", "audio_input", "audio_output")
}

func assertBillingComponentKinds(t *testing.T, components []BillingComponent, expected ...string) {
	t.Helper()
	seen := make(map[string]bool, len(components))
	for _, component := range components {
		seen[component.Kind] = true
	}
	for _, kind := range expected {
		if !seen[kind] {
			t.Fatalf("missing component %q in %+v", kind, components)
		}
	}
}
