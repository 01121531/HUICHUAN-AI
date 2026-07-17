package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/shopspring/decimal"
)

const (
	BillingSnapshotVersion = "v1"
)

// BillingComponent is one immutable part of the request-time charge.
// Decimal values are serialized as strings so that exports and the UI do not
// expose binary floating-point artifacts.
type BillingComponent struct {
	Kind               string `json:"kind"`
	Quantity           int64  `json:"quantity"`
	Unit               string `json:"unit"`
	UnitPriceUSD       string `json:"unit_price_usd"`
	PriceUnit          int64  `json:"price_unit"`
	Ratio              string `json:"ratio"`
	SubtotalQuota      int    `json:"subtotal_quota"`
	SubtotalQuotaExact string `json:"subtotal_quota_exact"`
	Note               string `json:"note,omitempty"`
}

type BillingClampSnapshot struct {
	Applied  bool   `json:"applied"`
	Kind     string `json:"kind,omitempty"`
	Original string `json:"original,omitempty"`
	Clamped  int    `json:"clamped,omitempty"`
}

// BillingSnapshotV1 records only values frozen for the completed request.
// It is built in the usage-log worker and never queries current pricing.
type BillingSnapshotV1 struct {
	Version           string                 `json:"version"`
	Status            string                 `json:"status"`
	ErrorCode         string                 `json:"error_code,omitempty"`
	Mode              string                 `json:"mode"`
	Source            string                 `json:"source"`
	RequestedModel    string                 `json:"requested_model"`
	EffectiveModel    string                 `json:"effective_model"`
	BaseCurrency      string                 `json:"base_currency"`
	QuotaPerUnit      string                 `json:"quota_per_unit"`
	DisplayCurrency   string                 `json:"display_currency"`
	ExchangeRate      string                 `json:"exchange_rate"`
	GroupRatio        string                 `json:"group_ratio"`
	UserGroupRatio    *string                `json:"user_group_ratio"`
	Components        []BillingComponent     `json:"components"`
	PreConsumedQuota  int                    `json:"pre_consumed_quota"`
	SettlementDelta   int                    `json:"settlement_delta"`
	FinalChargedQuota int                    `json:"final_charged_quota"`
	WalletQuota       int                    `json:"wallet_quota"`
	SubscriptionQuota int                    `json:"subscription_quota"`
	Rounding          string                 `json:"rounding"`
	TierVersion       string                 `json:"tier_version,omitempty"`
	TierHash          string                 `json:"tier_hash,omitempty"`
	MatchedTier       string                 `json:"matched_tier,omitempty"`
	TierInputs        map[string]interface{} `json:"tier_inputs,omitempty"`
	Clamp             *BillingClampSnapshot  `json:"clamp,omitempty"`
	Metadata          map[string]string      `json:"metadata,omitempty"`
}

func failedBillingSnapshot(log *Log, quotaPerUnit float64, code string) BillingSnapshotV1 {
	if code == "" {
		code = "snapshot_build_failed"
	}
	return BillingSnapshotV1{
		Version:           BillingSnapshotVersion,
		Status:            "failed",
		ErrorCode:         code,
		Mode:              "unknown",
		Source:            "unknown",
		RequestedModel:    safeLogModel(log),
		EffectiveModel:    safeLogModel(log),
		BaseCurrency:      "USD",
		QuotaPerUnit:      decimalString(quotaPerUnit),
		DisplayCurrency:   "USD",
		ExchangeRate:      "1",
		GroupRatio:        "1",
		Components:        []BillingComponent{},
		FinalChargedQuota: safeLogQuota(log),
		Rounding:          "round_half_away_from_zero",
	}
}

func safeLogModel(log *Log) string {
	if log == nil {
		return ""
	}
	return log.ModelName
}

func safeLogQuota(log *Log) int {
	if log == nil {
		return 0
	}
	return log.Quota
}

// BuildBillingSnapshotV1 creates a snapshot exclusively from the immutable log
// job payload. It must not read global price settings.
func BuildBillingSnapshotV1(log *Log, other map[string]interface{}, quotaPerUnit float64) (BillingSnapshotV1, error) {
	if log == nil {
		return failedBillingSnapshot(nil, quotaPerUnit, "missing_log"), errors.New("missing log")
	}
	if quotaPerUnit <= 0 || math.IsNaN(quotaPerUnit) || math.IsInf(quotaPerUnit, 0) {
		return failedBillingSnapshot(log, quotaPerUnit, "invalid_quota_per_unit"), errors.New("invalid quota per unit")
	}

	snapshot := BillingSnapshotV1{
		Version:           BillingSnapshotVersion,
		Status:            "complete",
		Mode:              billingMode(log, other),
		Source:            stringValue(other, "billing_source", "wallet"),
		RequestedModel:    log.ModelName,
		EffectiveModel:    stringValue(other, "upstream_model_name", log.ModelName),
		BaseCurrency:      "USD",
		QuotaPerUnit:      decimalString(quotaPerUnit),
		DisplayCurrency:   stringValue(other, "display_currency", "USD"),
		ExchangeRate:      decimalValueString(other, "exchange_rate", 1),
		GroupRatio:        decimalValueString(other, "group_ratio", 1),
		Components:        make([]BillingComponent, 0, 12),
		PreConsumedQuota:  intValue(other, "billing_pre_consumed_quota", 0),
		FinalChargedQuota: log.Quota,
		Rounding:          "round_half_away_from_zero",
	}
	snapshot.SettlementDelta = snapshot.FinalChargedQuota - snapshot.PreConsumedQuota
	if userGroupRatio, ok := numberValue(other["user_group_ratio"]); ok {
		value := decimalString(userGroupRatio)
		snapshot.UserGroupRatio = &value
	}

	if snapshot.Source == "subscription" {
		snapshot.SubscriptionQuota = intValue(other, "subscription_consumed", log.Quota)
		snapshot.WalletQuota = intValue(other, "wallet_quota_deducted", 0)
	} else {
		snapshot.WalletQuota = log.Quota
	}

	if snapshot.Mode == "tiered_expr" {
		snapshot.TierVersion = stringValue(other, "tier_version", "v1")
		snapshot.TierHash = stringValue(other, "tier_hash", "")
		if snapshot.TierHash == "" {
			if encoded := stringValue(other, "expr_b64", ""); encoded != "" {
				sum := sha256.Sum256([]byte(encoded))
				snapshot.TierHash = hex.EncodeToString(sum[:])
			}
		}
		if matched, ok := other["matched_tier"]; ok {
			snapshot.MatchedTier = fmt.Sprint(matched)
		}
		if inputs, ok := other["tier_inputs"].(map[string]interface{}); ok {
			snapshot.TierInputs = cloneUsageLogMap(inputs)
		}
	}

	if clamp := billingClamp(other); clamp != nil {
		snapshot.Clamp = clamp
	}

	components, err := buildBillingComponents(log, other, snapshot.Mode, quotaPerUnit)
	if err != nil {
		failed := failedBillingSnapshot(log, quotaPerUnit, "component_build_failed")
		failed.Source = snapshot.Source
		failed.PreConsumedQuota = snapshot.PreConsumedQuota
		failed.SettlementDelta = snapshot.SettlementDelta
		return failed, err
	}
	snapshot.Components = components
	return snapshot, nil
}

func billingMode(log *Log, other map[string]interface{}) string {
	if boolValue(other, "violation_fee") {
		return "violation_fee"
	}
	if stringValue(other, "billing_mode", "") == "tiered_expr" {
		return "tiered_expr"
	}
	if stringValue(other, "billing_source", "") == "subscription" {
		return "subscription"
	}
	if boolValue(other, "billing_free_model") || (log != nil && log.Quota == 0 && log.PromptTokens+log.CompletionTokens > 0) {
		return "free"
	}
	if boolValue(other, "billing_use_price") {
		return "per_call"
	}
	if modelPrice, ok := numberValue(other["model_price"]); ok && modelPrice > 0 {
		return "per_call"
	}
	if log != nil && (log.PromptTokens > 0 || log.CompletionTokens > 0) {
		return "per_token"
	}
	return "unknown"
}

func buildBillingComponents(log *Log, other map[string]interface{}, mode string, quotaPerUnit float64) ([]BillingComponent, error) {
	groupRatio := decimalFromMap(other, "group_ratio", decimal.NewFromInt(1))
	modelRatio := decimalFromMap(other, "model_ratio", decimal.Zero)
	components := make([]BillingComponent, 0, 12)

	switch mode {
	case "violation_fee":
		components = append(components, quotaComponent("violation_fee", 1, "request", decimalFromMap(other, "base_amount", decimal.Zero), 1, groupRatio, log.Quota, "policy violation fee"))
	case "tiered_expr":
		components = append(components, quotaComponent("tiered_charge", 1, "request", decimal.NewFromInt(int64(log.Quota)).Div(decimal.NewFromFloat(quotaPerUnit)), 1, decimal.NewFromInt(1), log.Quota, "frozen tier result"))
	case "free":
		components = []BillingComponent{}
	case "per_call":
		modelPrice := decimalFromMap(other, "model_price", decimal.Zero)
		baseQuota := modelPrice.Mul(decimal.NewFromFloat(quotaPerUnit)).Mul(groupRatio)
		components = append(components, decimalComponent("per_call", 1, "request", modelPrice, 1, groupRatio, baseQuota, "request-time per-call price"))
	default:
		inputTokens := int64(log.PromptTokens)
		outputTokens := int64(log.CompletionTokens)
		cacheRead := int64(intValue(other, "cache_tokens", 0))
		cacheCreate := int64(intValue(other, "cache_creation_tokens", 0))
		cacheCreate5m := int64(intValue(other, "cache_creation_tokens_5m", 0))
		cacheCreate1h := int64(intValue(other, "cache_creation_tokens_1h", 0))
		imageTokens := int64(intValue(other, "image_output", 0))
		audioTokens := int64(intValue(other, "audio_input_token_count", 0))
		audioOutputTokens := int64(0)
		isAudioUsage := boolValue(other, "audio") || boolValue(other, "ws")
		if isAudioUsage {
			inputTokens = int64(intValue(other, "text_input", log.PromptTokens))
			outputTokens = int64(intValue(other, "text_output", log.CompletionTokens))
			audioTokens = int64(intValue(other, "audio_input", 0))
			audioOutputTokens = int64(intValue(other, "audio_output", 0))
		}
		if stringValue(other, "usage_semantic", "openai") != "anthropic" {
			inputTokens -= cacheRead
			inputTokens -= cacheCreate
		}
		if imageTokens > 0 {
			inputTokens -= imageTokens
		}
		if audioTokens > 0 {
			inputTokens -= audioTokens
		}
		if inputTokens < 0 {
			inputTokens = 0
		}
		baseUnitPrice := modelRatio.Mul(decimal.NewFromInt(1000000)).Div(decimal.NewFromFloat(quotaPerUnit))
		components = appendPositiveTokenComponent(components, "input_tokens", inputTokens, baseUnitPrice, decimal.NewFromInt(1), groupRatio, quotaPerUnit)
		components = appendPositiveTokenComponent(components, "output_tokens", outputTokens, baseUnitPrice, decimalFromMap(other, "completion_ratio", decimal.NewFromInt(1)), groupRatio, quotaPerUnit)
		components = appendPositiveTokenComponent(components, "cache_read", cacheRead, baseUnitPrice, decimalFromMap(other, "cache_ratio", decimal.NewFromInt(1)), groupRatio, quotaPerUnit)
		remainingCacheCreate := cacheCreate - cacheCreate5m - cacheCreate1h
		if remainingCacheCreate < 0 {
			remainingCacheCreate = 0
		}
		components = appendPositiveTokenComponent(components, "cache_creation", remainingCacheCreate, baseUnitPrice, decimalFromMap(other, "cache_creation_ratio", decimal.NewFromInt(1)), groupRatio, quotaPerUnit)
		components = appendPositiveTokenComponent(components, "cache_creation_5m", cacheCreate5m, baseUnitPrice, decimalFromMap(other, "cache_creation_ratio_5m", decimal.NewFromInt(1)), groupRatio, quotaPerUnit)
		components = appendPositiveTokenComponent(components, "cache_creation_1h", cacheCreate1h, baseUnitPrice, decimalFromMap(other, "cache_creation_ratio_1h", decimal.NewFromInt(1)), groupRatio, quotaPerUnit)
		if imageTokens > 0 {
			components = appendPositiveTokenComponent(components, "image_input", imageTokens, baseUnitPrice, decimalFromMap(other, "image_ratio", decimal.NewFromInt(1)), groupRatio, quotaPerUnit)
		}
		if audioTokens > 0 {
			audioPrice := decimalFromMap(other, "audio_input_price", decimal.Zero)
			if !audioPrice.IsZero() {
				components = appendPriceComponent(components, "audio_input", audioTokens, "token", audioPrice, 1000000, groupRatio, quotaPerUnit)
			} else {
				components = appendPositiveTokenComponent(components, "audio_input", audioTokens, baseUnitPrice, decimalFromMap(other, "audio_ratio", decimal.NewFromInt(1)), groupRatio, quotaPerUnit)
			}
		}
		if audioOutputTokens > 0 {
			audioRatio := decimalFromMap(other, "audio_ratio", decimal.NewFromInt(1))
			audioCompletionRatio := decimalFromMap(other, "audio_completion_ratio", decimal.NewFromInt(1))
			components = appendPositiveTokenComponent(components, "audio_output", audioOutputTokens, baseUnitPrice, audioRatio.Mul(audioCompletionRatio), groupRatio, quotaPerUnit)
		}
	}

	components = appendToolComponents(components, other, groupRatio, quotaPerUnit)
	components = appendOtherRatioAdjustment(components, other, log.Quota)
	components = appendRoundingAdjustment(components, log.Quota)
	return components, nil
}

func appendPositiveTokenComponent(components []BillingComponent, kind string, quantity int64, baseUnitPrice, componentRatio, groupRatio decimal.Decimal, quotaPerUnit float64) []BillingComponent {
	if quantity <= 0 {
		return components
	}
	effectiveRatio := componentRatio.Mul(groupRatio)
	exact := decimal.NewFromInt(quantity).
		Mul(baseUnitPrice).
		Div(decimal.NewFromInt(1000000)).
		Mul(decimal.NewFromFloat(quotaPerUnit)).
		Mul(effectiveRatio)
	return append(components, decimalComponent(kind, quantity, "token", baseUnitPrice, 1000000, effectiveRatio, exact, ""))
}

func appendPriceComponent(components []BillingComponent, kind string, quantity int64, unit string, price decimal.Decimal, priceUnit int64, ratio decimal.Decimal, quotaPerUnit float64) []BillingComponent {
	if quantity <= 0 || price.IsZero() {
		return components
	}
	exact := decimal.NewFromInt(quantity).Mul(price).Div(decimal.NewFromInt(priceUnit)).Mul(ratio).Mul(decimal.NewFromFloat(quotaPerUnit))
	return append(components, decimalComponent(kind, quantity, unit, price, priceUnit, ratio, exact, ""))
}

func appendToolComponents(components []BillingComponent, other map[string]interface{}, groupRatio decimal.Decimal, quotaPerUnit float64) []BillingComponent {
	components = appendPriceComponent(components, "web_search", int64(intValue(other, "web_search_call_count", 0)), "call", decimalFromMap(other, "web_search_price", decimal.Zero), 1000, groupRatio, quotaPerUnit)
	components = appendPriceComponent(components, "file_search", int64(intValue(other, "file_search_call_count", 0)), "call", decimalFromMap(other, "file_search_price", decimal.Zero), 1000, groupRatio, quotaPerUnit)
	if price := decimalFromMap(other, "image_generation_call_price", decimal.Zero); !price.IsZero() {
		components = appendPriceComponent(components, "image_generation", 1, "call", price, 1, groupRatio, quotaPerUnit)
	}
	return components
}

func appendOtherRatioAdjustment(components []BillingComponent, other map[string]interface{}, finalQuota int) []BillingComponent {
	raw, ok := other["billing_other_ratios"].(map[string]interface{})
	if !ok || len(raw) == 0 {
		return components
	}
	multiplier := decimal.NewFromInt(1)
	for _, value := range raw {
		if number, ok := numberValue(value); ok && number > 0 {
			multiplier = multiplier.Mul(decimal.NewFromFloat(number))
		}
	}
	if multiplier.Equal(decimal.NewFromInt(1)) {
		return components
	}
	before := componentSubtotal(components)
	after := before.Mul(multiplier)
	return append(components, decimalComponent("other_ratio_adjustment", 1, "request", decimal.Zero, 1, multiplier, after.Sub(before), "request-time additional multiplier"))
}

func appendRoundingAdjustment(components []BillingComponent, finalQuota int) []BillingComponent {
	sum := 0
	for _, component := range components {
		sum += component.SubtotalQuota
	}
	if sum == finalQuota {
		return components
	}
	delta := finalQuota - sum
	return append(components, BillingComponent{
		Kind:               "settlement_adjustment",
		Quantity:           1,
		Unit:               "request",
		UnitPriceUSD:       "0",
		PriceUnit:          1,
		Ratio:              "1",
		SubtotalQuota:      delta,
		SubtotalQuotaExact: strconv.Itoa(delta),
		Note:               "rounding, clamp, overlap normalization, or final settlement adjustment",
	})
}

func componentSubtotal(components []BillingComponent) decimal.Decimal {
	total := decimal.Zero
	for _, component := range components {
		value, err := decimal.NewFromString(component.SubtotalQuotaExact)
		if err == nil {
			total = total.Add(value)
		}
	}
	return total
}

func quotaComponent(kind string, quantity int64, unit string, price decimal.Decimal, priceUnit int64, ratio decimal.Decimal, quota int, note string) BillingComponent {
	return BillingComponent{
		Kind:               kind,
		Quantity:           quantity,
		Unit:               unit,
		UnitPriceUSD:       price.String(),
		PriceUnit:          priceUnit,
		Ratio:              ratio.String(),
		SubtotalQuota:      quota,
		SubtotalQuotaExact: strconv.Itoa(quota),
		Note:               note,
	}
}

func decimalComponent(kind string, quantity int64, unit string, price decimal.Decimal, priceUnit int64, ratio, exact decimal.Decimal, note string) BillingComponent {
	return BillingComponent{
		Kind:               kind,
		Quantity:           quantity,
		Unit:               unit,
		UnitPriceUSD:       price.String(),
		PriceUnit:          priceUnit,
		Ratio:              ratio.String(),
		SubtotalQuota:      int(exact.Round(0).IntPart()),
		SubtotalQuotaExact: exact.String(),
		Note:               note,
	}
}

func decimalFromMap(values map[string]interface{}, key string, fallback decimal.Decimal) decimal.Decimal {
	value, ok := numberValue(values[key])
	if !ok {
		return fallback
	}
	return decimal.NewFromFloat(value)
}

func decimalString(value float64) string {
	return decimal.NewFromFloat(value).String()
}

func decimalValueString(values map[string]interface{}, key string, fallback float64) string {
	value, ok := numberValue(values[key])
	if !ok {
		value = fallback
	}
	return decimalString(value)
}

func numberValue(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, !math.IsNaN(typed) && !math.IsInf(typed, 0)
	case float32:
		result := float64(typed)
		return result, !math.IsNaN(result) && !math.IsInf(result, 0)
	case int:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case json.Number:
		result, err := typed.Float64()
		return result, err == nil
	case string:
		result, err := strconv.ParseFloat(typed, 64)
		return result, err == nil
	default:
		return 0, false
	}
}

func intValue(values map[string]interface{}, key string, fallback int) int {
	value, ok := numberValue(values[key])
	maxInt := float64(^uint(0) >> 1)
	minInt := -maxInt - 1
	if !ok || value > maxInt || value < minInt {
		return fallback
	}
	return int(math.Round(value))
}

func boolValue(values map[string]interface{}, key string) bool {
	value, ok := values[key]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		result, _ := strconv.ParseBool(typed)
		return result
	default:
		return false
	}
}

func stringValue(values map[string]interface{}, key, fallback string) string {
	if value, ok := values[key]; ok {
		switch typed := value.(type) {
		case string:
			if typed != "" {
				return typed
			}
		}
	}
	return fallback
}

func billingClamp(other map[string]interface{}) *BillingClampSnapshot {
	admin, ok := other["admin_info"].(map[string]interface{})
	if !ok {
		return nil
	}
	raw, ok := admin["quota_saturation"].(map[string]interface{})
	if !ok {
		return nil
	}
	return &BillingClampSnapshot{
		Applied:  true,
		Kind:     stringValue(raw, "kind", ""),
		Original: fmt.Sprint(raw["original"]),
		Clamped:  intValue(raw, "clamped", 0),
	}
}
