package datasetcapture

import (
	"encoding/json"
	"testing"
)

func TestValidateJSONLineRequiresFixedFieldSets(t *testing.T) {
	record, err := Normalize(testCapture(
		"/v1/chat/completions",
		[]byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}]}`),
		[]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`),
	))
	if err != nil {
		t.Fatal(err)
	}
	line, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateJSONLine(line); err != nil {
		t.Fatalf("valid record rejected: %v", err)
	}

	var root map[string]any
	if err := json.Unmarshal(line, &root); err != nil {
		t.Fatal(err)
	}
	root["unexpected"] = true
	withExtraRoot, _ := json.Marshal(root)
	if err := ValidateJSONLine(withExtraRoot); err == nil {
		t.Fatal("extra root field was accepted")
	}
	delete(root, "unexpected")
	response := root["response"].(map[string]any)
	usage := response["usage"].(map[string]any)
	usage["unexpected"] = 1
	withExtraNested, _ := json.Marshal(root)
	if err := ValidateJSONLine(withExtraNested); err == nil {
		t.Fatal("extra nested field was accepted")
	}
}

func TestValidateJSONLineAllowsReasoningAndCompletenessExtensions(t *testing.T) {
	record, err := Normalize(testCapture(
		"/v1/chat/completions",
		[]byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}]}`),
		[]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`),
	))
	if err != nil {
		t.Fatal(err)
	}
	reasoning := "short summary"
	record.Response.Reasoning = &Reasoning{
		Content:    &reasoning,
		Blocks:     []ContentBlock{{"type": "reasoning", "text": reasoning}},
		Visibility: "captured",
		Source:     "provider_field",
	}
	record.Meta.ReasoningStatus = ReasoningStatusCaptured
	record.Meta.CaptureWarnings = []string{"provider_reasoning_summary"}
	line, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateJSONLine(line); err != nil {
		t.Fatalf("reasoning extension rejected: %v", err)
	}

	var root map[string]any
	if err := json.Unmarshal(line, &root); err != nil {
		t.Fatal(err)
	}
	if len(root) != len(schemaRootKeys) {
		t.Fatalf("top-level field count changed: %d", len(root))
	}
}

func TestNormalizeSetsCompletenessMetadata(t *testing.T) {
	record, err := Normalize(testCapture(
		"/v1/chat/completions",
		[]byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}]}`),
		[]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`),
	))
	if err != nil {
		t.Fatal(err)
	}
	if record.Meta.CaptureStatus != CaptureStatusComplete {
		t.Fatalf("capture status=%q", record.Meta.CaptureStatus)
	}
	if record.Meta.ResponseProtocol != "openai-chat" {
		t.Fatalf("response protocol=%q", record.Meta.ResponseProtocol)
	}
	if record.Meta.ReasoningStatus != ReasoningStatusNotRequested {
		t.Fatalf("reasoning status=%q", record.Meta.ReasoningStatus)
	}
	if !record.Meta.StreamTerminated {
		t.Fatal("non-stream response was not marked terminated")
	}
}
