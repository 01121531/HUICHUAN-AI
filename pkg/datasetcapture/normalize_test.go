package datasetcapture

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestNormalizeOpenAIChat(t *testing.T) {
	request := []byte(`{
      "model":"gpt-test",
      "metadata":{"session_id":"conversation-1"},
      "messages":[
        {"role":"system","content":"system rules"},
        {"role":"user","content":[{"type":"text","text":"hello"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]},
        {"role":"assistant","content":null,"tool_calls":[{"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]},
        {"role":"tool","tool_call_id":"call-1","content":"result"}
      ],
      "tools":[{"type":"function","function":{"name":"lookup","description":"find data","parameters":{"type":"object"}}}]
    }`)
	response := []byte(`{
      "choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],
      "usage":{"prompt_tokens":12,"completion_tokens":3,"prompt_tokens_details":{"cached_tokens":4,"cache_write_tokens":2}}
    }`)
	record, err := Normalize(testCapture("/v1/chat/completions", request, response))
	if err != nil {
		t.Fatal(err)
	}
	if record.SystemPrompt != "system rules" || record.Meta.UserQuery != "hello" {
		t.Fatalf("unexpected normalized request: %#v", record)
	}
	if len(record.Tools) != 1 || len(record.Messages) != 3 {
		t.Fatalf("tools=%d messages=%d", len(record.Tools), len(record.Messages))
	}
	if got := record.Messages[0].Content[1]["source"].(map[string]any)["image_url"].(map[string]any)["url"]; got != "data:image/png;base64,AAAA" {
		t.Fatalf("multimodal data changed: %v", got)
	}
	if record.Response.Content == nil || *record.Response.Content != "done" || *record.Response.StopReason != "end_turn" {
		t.Fatalf("unexpected response: %#v", record.Response)
	}
	if record.Response.Usage.Cache.CacheReadInputTokens != 4 || record.Response.Usage.Cache.CacheCreationInputTokens != 2 {
		t.Fatalf("unexpected usage: %#v", record.Response.Usage)
	}
	assertSchemaKeys(t, record)
}

func TestRequestedModelAcrossProtocols(t *testing.T) {
	tests := []struct {
		name string
		path string
		body string
		want string
	}{
		{name: "openai chat", path: "/v1/chat/completions", body: `{"model":"gpt-5.2","messages":[]}`, want: "gpt-5.2"},
		{name: "openai responses", path: "/v1/responses", body: `{"model":"gpt-5.3-codex","input":"hello"}`, want: "gpt-5.3-codex"},
		{name: "anthropic", path: "/v1/messages", body: `{"model":"claude-sonnet-4","messages":[]}`, want: "claude-sonnet-4"},
		{name: "gemini path", path: "/v1beta/models/gemini-2.5-pro:generateContent", body: `{"contents":[]}`, want: "gemini-2.5-pro"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model, err := RequestedModel(test.path, []byte(test.body))
			if err != nil {
				t.Fatal(err)
			}
			if model != test.want {
				t.Fatalf("model = %q, want %q", model, test.want)
			}
		})
	}
}

func TestInspectRequestExtractsPolicyMetadata(t *testing.T) {
	metadata, err := InspectRequest("/v1/chat/completions", []byte(`{"model":"gpt-test","stream":true,"metadata":{"conversation_id":"conversation-7"},"messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Model != "gpt-test" || !metadata.Stream {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
}

func TestInspectRequestGetsGeminiModelFromPath(t *testing.T) {
	metadata, err := InspectRequest("/v1beta/models/gemini-2.5-pro:streamGenerateContent", []byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Model != "gemini-2.5-pro" || !metadata.Stream {
		t.Fatalf("unexpected Gemini metadata: %#v", metadata)
	}
}

func TestNormalizeAnthropicStream(t *testing.T) {
	request := []byte(`{"model":"claude-test","system":"be useful","messages":[{"role":"user","content":[{"type":"text","text":"run"}]}],"tools":[{"name":"shell","description":"run command","input_schema":{"type":"object"}}]}`)
	response := []byte("event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10,\"cache_read_input_tokens\":5}}}\n\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"tool-1\",\"name\":\"shell\",\"input\":{}}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"command\\\":\\\"pwd\\\"}\"}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":8}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n")
	record, err := Normalize(testCapture("/v1/messages", request, response))
	if err != nil {
		t.Fatal(err)
	}
	if record.Response.StopReason == nil || *record.Response.StopReason != "tool_use" || len(record.Response.ToolUse.Calls) != 1 {
		t.Fatalf("unexpected tool response: %#v", record.Response)
	}
	call := record.Response.ToolUse.Calls[0]
	if call.Name != "shell" || call.Input.(map[string]any)["command"] != "pwd" {
		t.Fatalf("unexpected call: %#v", call)
	}
	if record.Response.Usage.InputTokens != 10 || record.Response.Usage.OutputTokens != 8 || record.Response.Usage.Cache.CacheReadInputTokens != 5 {
		t.Fatalf("unexpected usage: %#v", record.Response.Usage)
	}
}

func TestNormalizeGeminiAndResponses(t *testing.T) {
	geminiRequest := []byte(`{"systemInstruction":{"parts":[{"text":"rules"}]},"contents":[{"role":"user","parts":[{"text":"hi"},{"inlineData":{"mimeType":"image/png","data":"AAAA"}}]}],"tools":[{"functionDeclarations":[{"name":"lookup","parameters":{"type":"OBJECT"}}]}]}`)
	geminiResponse := []byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":2,"cachedContentTokenCount":3}}`)
	gemini, err := Normalize(testCapture("/v1beta/models/gemini-test:generateContent", geminiRequest, geminiResponse))
	if err != nil {
		t.Fatal(err)
	}
	if gemini.Model != "gemini-test" || *gemini.Response.Content != "hello" || gemini.Response.Usage.Cache.CacheReadInputTokens != 3 {
		t.Fatalf("unexpected Gemini record: %#v", gemini)
	}

	responsesRequest := []byte(`{"model":"gpt-response","instructions":"rules","input":[{"role":"user","content":[{"type":"input_text","text":"question"}]}]}`)
	responsesBody := []byte(`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"answer"}]}],"usage":{"input_tokens":5,"output_tokens":2}}`)
	responses, err := Normalize(testCapture("/v1/responses", responsesRequest, responsesBody))
	if err != nil {
		t.Fatal(err)
	}
	if responses.SystemPrompt != "rules" || *responses.Response.Content != "answer" || *responses.Response.StopReason != "end_turn" {
		t.Fatalf("unexpected Responses record: %#v", responses)
	}
}

func TestNormalizeCanOmitMultimodalBase64Payload(t *testing.T) {
	capture := testCapture(
		"/v1beta/models/gemini-test:generateContent",
		[]byte(`{"model":"gemini-test","contents":[{"role":"user","parts":[{"text":"inspect"},{"inlineData":{"mimeType":"image/png","data":"sensitive-base64"}}]}]}`),
		[]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`),
	)
	capture.StripMultimodalBase64 = true
	record, err := Normalize(capture)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("sensitive-base64")) {
		t.Fatalf("base64 payload was retained: %s", encoded)
	}
	if !bytes.Contains(encoded, []byte(`"omitted":true`)) {
		t.Fatalf("omission marker missing: %s", encoded)
	}
}

func TestIncompleteStreamIsRejected(t *testing.T) {
	request := []byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}]}`)
	response := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"},\"finish_reason\":null}]}\n\n")
	_, err := Normalize(testCapture("/v1/chat/completions", request, response))
	if err == nil {
		t.Fatal("expected incomplete stream error")
	}
}

func TestSupportedPathExcludesNonTrainingEndpoints(t *testing.T) {
	for _, value := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages", "/v1beta/models/gemini:streamGenerateContent"} {
		if !IsSupportedPath(value) {
			t.Fatalf("expected supported path: %s", value)
		}
	}
	for _, value := range []string{"/v1/responses/compact", "/v1/embeddings", "/v1/audio/transcriptions", "/v1/realtime"} {
		if IsSupportedPath(value) {
			t.Fatalf("unexpected supported path: %s", value)
		}
	}
}

func TestNormalizeOpenAIStreamToolFragments(t *testing.T) {
	request := []byte(`{"model":"gpt-test","messages":[{"role":"user","content":"find"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`)
	response := []byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","function":{"name":"lookup","arguments":"{\"q\":\""}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"x\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}

data: [DONE]

`)
	record, err := Normalize(testCapture("/v1/chat/completions", request, response))
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Response.ToolUse.Calls) != 1 || record.Response.ToolUse.Calls[0].Input.(map[string]any)["q"] != "x" {
		t.Fatalf("unexpected merged tool call: %#v", record.Response.ToolUse.Calls)
	}
}

func TestNormalizeOpenAIChatReasoning(t *testing.T) {
	record, err := Normalize(testCapture(
		"/v1/chat/completions",
		[]byte(`{"model":"gpt-reasoning","messages":[{"role":"user","content":"solve"}]}`),
		[]byte(`{"choices":[{"message":{"reasoning_content":"first check","content":"answer"},"finish_reason":"stop"}]}`),
	))
	if err != nil {
		t.Fatal(err)
	}
	if record.Response.Content == nil || *record.Response.Content != "answer" {
		t.Fatalf("visible content=%v", record.Response.Content)
	}
	if record.Response.Reasoning == nil || record.Meta.ReasoningStatus != ReasoningStatusCaptured {
		t.Fatalf("reasoning was not captured: %#v", record.Response)
	}
	if record.Response.Reasoning.Content == nil || *record.Response.Reasoning.Content != "first check" {
		t.Fatalf("reasoning content=%v", record.Response.Reasoning.Content)
	}
}

func TestNormalizeOpenAIResponsesReasoningSummary(t *testing.T) {
	record, err := Normalize(testCapture(
		"/v1/responses",
		[]byte(`{"model":"gpt-response","input":"solve"}`),
		[]byte(`{"status":"completed","output":[{"type":"reasoning","summary":[{"type":"summary_text","text":"inspect constraints"}]},{"type":"message","content":[{"type":"output_text","text":"answer"}]}]}`),
	))
	if err != nil {
		t.Fatal(err)
	}
	if record.Response.Reasoning == nil || record.Response.Reasoning.Content == nil {
		t.Fatalf("reasoning summary missing: %#v", record.Response)
	}
	if *record.Response.Reasoning.Content != "inspect constraints" {
		t.Fatalf("unexpected reasoning: %q", *record.Response.Reasoning.Content)
	}
	if record.Response.Content == nil || *record.Response.Content != "answer" {
		if record.Response.Content == nil {
			t.Fatal("visible response content is nil")
		}
		t.Fatalf("unexpected visible content: %q", *record.Response.Content)
	}
}

func TestNormalizeAnthropicThinkingAndRedactedThinking(t *testing.T) {
	record, err := Normalize(testCapture(
		"/v1/messages",
		[]byte(`{"model":"claude-reasoning","messages":[{"role":"user","content":"solve"}]}`),
		[]byte(`{"type":"message","stop_reason":"end_turn","content":[{"type":"thinking","thinking":"private check"},{"type":"text","text":"answer"},{"type":"redacted_thinking","signature":"sig"}]}`),
	))
	if err != nil {
		t.Fatal(err)
	}
	if record.Response.Reasoning == nil || record.Meta.ReasoningStatus != ReasoningStatusRedacted {
		t.Fatalf("thinking status=%q response=%#v", record.Meta.ReasoningStatus, record.Response)
	}
	if record.Response.Reasoning.Content == nil || *record.Response.Reasoning.Content != "private check" {
		t.Fatalf("thinking content=%v", record.Response.Reasoning.Content)
	}
	if len(record.Response.Reasoning.Blocks) != 2 {
		t.Fatalf("reasoning blocks=%#v", record.Response.Reasoning.Blocks)
	}
}

func TestNormalizeGeminiThoughtPart(t *testing.T) {
	record, err := Normalize(testCapture(
		"/v1beta/models/gemini-reasoning:generateContent",
		[]byte(`{"contents":[{"role":"user","parts":[{"text":"solve"}]}]}`),
		[]byte(`{"candidates":[{"content":{"parts":[{"thought":true,"text":"check"},{"text":"answer"}]},"finishReason":"STOP"}]}`),
	))
	if err != nil {
		t.Fatal(err)
	}
	if record.Response.Reasoning == nil || record.Response.Reasoning.Content == nil || *record.Response.Reasoning.Content != "check" {
		t.Fatalf("Gemini reasoning=%#v", record.Response.Reasoning)
	}
	if record.Response.Content == nil || *record.Response.Content != "answer" {
		t.Fatalf("Gemini visible content=%v", record.Response.Content)
	}
}

func TestNormalizeOpenAIStreamCompletesOnDoneAndCapturesReasoning(t *testing.T) {
	record, err := Normalize(testCapture(
		"/v1/chat/completions",
		[]byte(`{"model":"gpt-stream","messages":[{"role":"user","content":"solve"}]}`),
		[]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"check \"},\"finish_reason\":null}]}\n\n"+
			"data: {\"choices\":[{\"delta\":{\"content\":\"answer\"},\"finish_reason\":null}]}\n\n"+
			"data: [DONE]\n\n"),
	))
	if err != nil {
		t.Fatal(err)
	}
	if !record.Meta.StreamTerminated || record.Response.Content == nil || *record.Response.Content != "answer" {
		t.Fatalf("stream completion/content mismatch: %#v", record)
	}
	if record.Response.Reasoning == nil || record.Response.Reasoning.Content == nil || *record.Response.Reasoning.Content != "check " {
		t.Fatalf("stream reasoning missing: %#v", record.Response.Reasoning)
	}
}

func TestNormalizeAnthropicThinkingStream(t *testing.T) {
	record, err := Normalize(testCapture(
		"/v1/messages",
		[]byte(`{"model":"claude-stream","messages":[{"role":"user","content":"solve"}]}`),
		[]byte("event: message_start\n"+
			"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":2}}}\n\n"+
			"event: content_block_delta\n"+
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"check\"}}\n\n"+
			"event: content_block_delta\n"+
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"answer\"}}\n\n"+
			"event: message_delta\n"+
			"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n"+
			"event: message_stop\n"+
			"data: {\"type\":\"message_stop\"}\n\n"),
	))
	if err != nil {
		t.Fatal(err)
	}
	if record.Response.Reasoning == nil || record.Response.Content == nil {
		t.Fatalf("Anthropic stream output missing: %#v", record.Response)
	}
	if *record.Response.Reasoning.Content != "check" || *record.Response.Content != "answer" {
		t.Fatalf("Anthropic stream output mismatch: %#v", record.Response)
	}
}

func TestNormalizeResponsesReasoningStream(t *testing.T) {
	record, err := Normalize(testCapture(
		"/v1/responses",
		[]byte(`{"model":"gpt-responses-stream","input":"solve"}`),
		[]byte("data: {\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"check\"}\n\n"+
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"answer\"}\n\n"+
			"data: {\"type\":\"response.completed\"}\n\n"),
	))
	if err != nil {
		t.Fatal(err)
	}
	if record.Response.Reasoning == nil || record.Response.Content == nil || !record.Meta.StreamTerminated {
		t.Fatalf("Responses stream output missing: %#v", record)
	}
}

func TestNormalizeRejectsMalformedSSEEvent(t *testing.T) {
	request := []byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}]}`)
	response := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n" + "data: {not-json}\n\n")
	_, err := Normalize(testCapture("/v1/chat/completions", request, response))
	if err == nil {
		t.Fatal("malformed SSE event was accepted")
	}
}

func TestNormalizeAppliesReasoningCapturePolicy(t *testing.T) {
	capture := testCapture(
		"/v1/chat/completions",
		[]byte(`{"model":"gpt-policy","messages":[{"role":"user","content":"solve"}]}`),
		[]byte(`{"choices":[{"message":{"reasoning_content":"private","content":"answer"},"finish_reason":"stop"}]}`),
	)
	capture.ReasoningMode = ReasoningModeRedacted
	redacted, err := Normalize(capture)
	if err != nil {
		t.Fatal(err)
	}
	if redacted.Response.Reasoning == nil || redacted.Response.Reasoning.Content != nil || redacted.Meta.ReasoningStatus != ReasoningStatusRedacted {
		t.Fatalf("reasoning was not redacted: %#v", redacted.Response.Reasoning)
	}

	capture.ReasoningMode = ReasoningModeDisabled
	disabled, err := Normalize(capture)
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Response.Reasoning == nil || len(disabled.Response.Reasoning.Blocks) != 0 || disabled.Meta.ReasoningStatus != ReasoningStatusRedacted {
		t.Fatalf("reasoning was not disabled: %#v", disabled.Response.Reasoning)
	}
}

func TestNormalizeKeepsReasoningOnlyResponseWhenPolicyDisablesBody(t *testing.T) {
	capture := testCapture(
		"/v1/chat/completions",
		[]byte(`{"model":"gpt-policy","messages":[{"role":"user","content":"solve"}]}`),
		[]byte(`{"choices":[{"message":{"reasoning_content":"private"},"finish_reason":"stop"}]}`),
	)
	capture.ReasoningMode = ReasoningModeDisabled
	record, err := Normalize(capture)
	if err != nil {
		t.Fatal(err)
	}
	if record.Response.Content != nil || record.Response.Reasoning == nil {
		t.Fatalf("reasoning-only response was not retained as metadata: %#v", record.Response)
	}
}

func TestDecodeResponseEventsProvidesProtocolNeutralEnvelope(t *testing.T) {
	events, streamed, err := DecodeResponseEvents(
		"openai-chat",
		[]byte("data: {\"type\":\"chunk\"}\n\n"+"data: [DONE]\n\n"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !streamed || len(events) != 1 || events[0].Protocol != "openai-chat" || !events[0].Terminal {
		t.Fatalf("unexpected response events: streamed=%v events=%#v", streamed, events)
	}
}

func testCapture(path string, request, response []byte) Capture {
	return Capture{
		RequestBody:  request,
		ResponseBody: response,
		Path:         path,
		RequestID:    "request-1",
		UserID:       "42",
		UserAgent:    "test-agent",
		HMACKey:      "test-key",
		CreatedAt:    time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
	}
}

func assertSchemaKeys(t *testing.T, record Record) {
	t.Helper()
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	want := []string{"_meta", "created_at", "cwd", "messages", "model", "response", "session_id", "system_prompt", "tools", "user_agent", "user_id_hash"}
	got := make([]string, 0, len(object))
	for key := range object {
		got = append(got, key)
	}
	for i := 0; i < len(got); i++ {
		for j := i + 1; j < len(got); j++ {
			if got[j] < got[i] {
				got[i], got[j] = got[j], got[i]
			}
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schema keys=%v want=%v", got, want)
	}
}
