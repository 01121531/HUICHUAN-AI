package datasetcapture

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func normalizeResponse(protocol string, body []byte) (normalizedResponse, error) {
	objects, streamed, terminal, err := responseObjects(body)
	if err != nil {
		return normalizedResponse{}, err
	}
	if len(objects) == 0 {
		return normalizedResponse{}, ErrIncompleteCapture
	}
	var result normalizedResponse
	switch protocol {
	case "openai-chat":
		result = normalizeOpenAIChatResponse(objects)
	case "openai-responses":
		result = normalizeOpenAIResponsesResponse(objects)
	case "anthropic":
		result = normalizeAnthropicResponse(objects)
	case "gemini":
		result = normalizeGeminiResponse(objects)
	default:
		return normalizedResponse{}, ErrUnsupportedProtocol
	}
	if streamed && !terminal && result.RawFinishReason == nil {
		result.Complete = false
	}
	if streamed && terminal {
		result.Complete = true
	}
	result.StreamTerminated = terminal || !streamed
	if result.ReasoningStatus == "" {
		result.ReasoningStatus = ReasoningStatusNotRequested
	}
	return result, nil
}

func responseObjects(body []byte) ([]map[string]any, bool, bool, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, false, false, ErrIncompleteCapture
	}
	var object map[string]any
	if json.Unmarshal(trimmed, &object) == nil && object != nil {
		return []map[string]any{object}, false, true, nil
	}
	var array []map[string]any
	if json.Unmarshal(trimmed, &array) == nil && array != nil {
		return array, false, true, nil
	}

	var objects []map[string]any
	terminal := false
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 64<<10), 256<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			terminal = true
			continue
		}
		var value map[string]any
		if err := json.Unmarshal([]byte(data), &value); err != nil {
			return nil, true, terminal, fmt.Errorf("invalid JSON response event: %w", err)
		}
		if value == nil {
			return nil, true, terminal, fmt.Errorf("invalid JSON response event: null")
		}
		objects = append(objects, value)
		if eventType := asString(value["type"]); eventType == "message_stop" || eventType == "response.completed" || eventType == "response.done" {
			terminal = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, true, terminal, err
	}
	if len(objects) == 0 {
		return nil, true, terminal, fmt.Errorf("no JSON response events")
	}
	return objects, true, terminal, nil
}

// DecodeResponseEvents exposes the same bounded event decoding used by the
// normalizer for diagnostics and protocol golden tests.
func DecodeResponseEvents(protocol string, body []byte) ([]ResponseEvent, bool, error) {
	objects, streamed, terminal, err := responseObjects(body)
	if err != nil {
		return nil, streamed, err
	}
	events := make([]ResponseEvent, 0, len(objects))
	for index, payload := range objects {
		event := ResponseEvent{
			Protocol: protocol,
			Type:     asString(payload["type"]),
			Sequence: index,
			Payload:  payload,
		}
		if index == len(objects)-1 && terminal {
			event.Terminal = true
		}
		events = append(events, event)
	}
	return events, streamed, nil
}

func normalizeOpenAIChatResponse(objects []map[string]any) normalizedResponse {
	var text strings.Builder
	toolBuilders := map[int]*toolCallBuilder{}
	var reasoning reasoningAccumulator
	var rawFinish string
	var usage Usage
	for _, object := range objects {
		mergeOpenAIUsage(&usage, asMap(object["usage"]))
		for _, choiceValue := range asSlice(object["choices"]) {
			choice := asMap(choiceValue)
			if finish := asString(choice["finish_reason"]); finish != "" {
				rawFinish = finish
			}
			message := asMap(choice["message"])
			if len(message) > 0 {
				text.WriteString(contentText(message["content"]))
				reasoning.consumeMap(message, "openai.chat.message")
				mergeOpenAIToolCalls(toolBuilders, asSlice(message["tool_calls"]))
			}
			delta := asMap(choice["delta"])
			if len(delta) > 0 {
				text.WriteString(contentText(delta["content"]))
				reasoning.consumeMap(delta, "openai.chat.delta")
				mergeOpenAIToolCalls(toolBuilders, asSlice(delta["tool_calls"]))
			}
			if completion := asString(choice["text"]); completion != "" {
				text.WriteString(completion)
			}
		}
	}
	toolCalls := builtToolCalls(toolBuilders)
	reasoningValue, reasoningStatus := reasoning.result()
	content := optionalString(text.String())
	stop := mapStopReason(rawFinish, len(toolCalls) > 0)
	return normalizedResponse{
		Content:         content,
		Reasoning:       reasoningValue,
		ReasoningStatus: reasoningStatus,
		StopReason:      stop,
		RawFinishReason: optionalString(rawFinish),
		ToolCalls:       toolCalls,
		Usage:           usage,
		Complete:        rawFinish != "" || len(objects) == 1,
	}
}

func normalizeOpenAIResponsesResponse(objects []map[string]any) normalizedResponse {
	var text strings.Builder
	toolBuilders := map[int]*toolCallBuilder{}
	var reasoning reasoningAccumulator
	var rawFinish string
	var usage Usage
	for _, event := range objects {
		eventType := asString(event["type"])
		switch eventType {
		case "response.output_text.delta":
			text.WriteString(asString(event["delta"]))
		case "response.reasoning_summary_text.delta", "response.reasoning_content.delta":
			reasoning.addText(asString(event["delta"]), "reasoning", "openai.responses.event")
		case "response.reasoning_summary_part.added", "response.reasoning_summary_part.done":
			reasoning.consumeValue(event["part"], "openai.responses.event")
		case "response.function_call_arguments.delta":
			index := asInt(event["output_index"])
			builder := getToolBuilder(toolBuilders, index)
			builder.ID = firstNonEmpty(builder.ID, asString(event["item_id"]))
			builder.Arguments.WriteString(asString(event["delta"]))
		case "response.function_call_arguments.done":
			index := asInt(event["output_index"])
			builder := getToolBuilder(toolBuilders, index)
			builder.ID = firstNonEmpty(builder.ID, asString(event["item_id"]))
			builder.Name = firstNonEmpty(builder.Name, asString(event["name"]))
			if builder.Arguments.Len() == 0 {
				builder.Arguments.WriteString(asString(event["arguments"]))
			}
		case "response.output_item.added", "response.output_item.done":
			item := asMap(event["item"])
			if asString(item["type"]) == "reasoning" {
				reasoning.consumeMap(item, "openai.responses.item")
			} else if asString(item["type"]) == "function_call" {
				index := asInt(event["output_index"])
				builder := getToolBuilder(toolBuilders, index)
				builder.ID = firstNonEmpty(builder.ID, asString(item["call_id"]), asString(item["id"]))
				builder.Name = firstNonEmpty(builder.Name, asString(item["name"]))
				if builder.Arguments.Len() == 0 {
					builder.Arguments.WriteString(asString(item["arguments"]))
				}
			}
		case "response.completed", "response.done":
			rawFinish = "completed"
		}
		response := asMap(event["response"])
		if len(response) == 0 && event["output"] != nil {
			response = event
		}
		if len(response) > 0 {
			mergeOpenAIUsage(&usage, asMap(response["usage"]))
			status := asString(response["status"])
			if status != "" {
				rawFinish = status
			}
			if status == "incomplete" {
				if reason := asString(asMap(response["incomplete_details"])["reason"]); reason != "" {
					rawFinish = reason
				}
			}
			for index, outputValue := range asSlice(response["output"]) {
				output := asMap(outputValue)
				switch asString(output["type"]) {
				case "message":
					text.WriteString(contentText(output["content"]))
					reasoning.consumeMap(output, "openai.responses.output")
				case "reasoning":
					reasoning.consumeMap(output, "openai.responses.output")
				case "function_call":
					builder := getToolBuilder(toolBuilders, index)
					builder.ID = firstNonEmpty(asString(output["call_id"]), asString(output["id"]))
					builder.Name = asString(output["name"])
					builder.Arguments.WriteString(asString(output["arguments"]))
				}
			}
		}
		mergeOpenAIUsage(&usage, asMap(event["usage"]))
	}
	toolCalls := builtToolCalls(toolBuilders)
	reasoningValue, reasoningStatus := reasoning.result()
	stop := mapStopReason(rawFinish, len(toolCalls) > 0)
	return normalizedResponse{
		Content:         optionalString(text.String()),
		Reasoning:       reasoningValue,
		ReasoningStatus: reasoningStatus,
		StopReason:      stop,
		RawFinishReason: optionalString(rawFinish),
		ToolCalls:       toolCalls,
		Usage:           usage,
		Complete:        rawFinish != "" || len(objects) == 1,
	}
}

func normalizeAnthropicResponse(objects []map[string]any) normalizedResponse {
	var text strings.Builder
	toolBuilders := map[int]*toolCallBuilder{}
	var reasoning reasoningAccumulator
	var rawFinish string
	var usage Usage
	for _, event := range objects {
		eventType := asString(event["type"])
		mergeAnthropicUsage(&usage, asMap(event["usage"]))
		message := asMap(event["message"])
		if len(message) > 0 {
			mergeAnthropicUsage(&usage, asMap(message["usage"]))
			if model := asString(message["stop_reason"]); model != "" {
				rawFinish = model
			}
		}
		if stop := asString(event["stop_reason"]); stop != "" {
			rawFinish = stop
		}
		delta := asMap(event["delta"])
		if stop := asString(delta["stop_reason"]); stop != "" {
			rawFinish = stop
		}
		if asString(delta["type"]) == "text_delta" {
			text.WriteString(asString(delta["text"]))
		}
		if asString(delta["type"]) == "thinking_delta" {
			reasoning.addText(asString(delta["thinking"]), "thinking", "anthropic.delta")
		}
		if asString(delta["type"]) == "input_json_delta" {
			getToolBuilder(toolBuilders, asInt(event["index"])).Arguments.WriteString(asString(delta["partial_json"]))
		}
		contentBlock := asMap(event["content_block"])
		if len(contentBlock) > 0 {
			consumeAnthropicBlock(&text, &reasoning, toolBuilders, asInt(event["index"]), contentBlock)
		}
		for index, blockValue := range asSlice(event["content"]) {
			consumeAnthropicBlock(&text, &reasoning, toolBuilders, index, asMap(blockValue))
		}
		if eventType == "message_stop" && rawFinish == "" {
			rawFinish = "end_turn"
		}
	}
	toolCalls := builtToolCalls(toolBuilders)
	reasoningValue, reasoningStatus := reasoning.result()
	return normalizedResponse{
		Content:         optionalString(text.String()),
		Reasoning:       reasoningValue,
		ReasoningStatus: reasoningStatus,
		StopReason:      mapStopReason(rawFinish, len(toolCalls) > 0),
		RawFinishReason: optionalString(rawFinish),
		ToolCalls:       toolCalls,
		Usage:           usage,
		Complete:        rawFinish != "" || len(objects) == 1,
	}
}

func normalizeGeminiResponse(objects []map[string]any) normalizedResponse {
	var text strings.Builder
	var reasoning reasoningAccumulator
	var rawFinish string
	toolBuilders := map[int]*toolCallBuilder{}
	toolIndexes := map[string]int{}
	nextToolIndex := 0
	var usage Usage
	for _, object := range objects {
		mergeGeminiUsage(&usage, asMap(object["usageMetadata"]))
		for _, candidateValue := range asSlice(object["candidates"]) {
			candidate := asMap(candidateValue)
			if finish := asString(candidate["finishReason"]); finish != "" {
				rawFinish = finish
			}
			content := asMap(candidate["content"])
			for partIndex, partValue := range asSlice(content["parts"]) {
				part := asMap(partValue)
				if part["text"] != nil {
					if thought, ok := part["thought"].(bool); ok && thought {
						reasoning.addText(asString(part["text"]), "thinking", "gemini.part")
					} else {
						text.WriteString(asString(part["text"]))
					}
				}
				if part["functionCall"] != nil {
					call := asMap(part["functionCall"])
					id := asString(call["id"])
					index, hasExplicitIndex := part["index"], part["index"] != nil
					toolIndex := asInt(index)
					if !hasExplicitIndex && id != "" {
						if previous, ok := toolIndexes[id]; ok {
							toolIndex = previous
						} else {
							toolIndex = nextToolIndex
							toolIndexes[id] = toolIndex
							nextToolIndex++
						}
					}
					if !hasExplicitIndex && id == "" {
						toolIndex = partIndex
					}
					builder := getToolBuilder(toolBuilders, toolIndex)
					builder.ID = firstNonEmpty(builder.ID, id)
					builder.Name = firstNonEmpty(builder.Name, asString(call["name"]))
					if call["args"] != nil {
						builder.Input = call["args"]
					}
				}
			}
		}
	}
	reasoningValue, reasoningStatus := reasoning.result()
	toolCalls := builtToolCalls(toolBuilders)
	return normalizedResponse{
		Content:         optionalString(text.String()),
		Reasoning:       reasoningValue,
		ReasoningStatus: reasoningStatus,
		StopReason:      mapStopReason(rawFinish, len(toolCalls) > 0),
		RawFinishReason: optionalString(rawFinish),
		ToolCalls:       toolCalls,
		Usage:           usage,
		Complete:        rawFinish != "" || len(objects) == 1,
	}
}

type toolCallBuilder struct {
	ID        string
	Name      string
	Arguments strings.Builder
	Input     any
}

func getToolBuilder(builders map[int]*toolCallBuilder, index int) *toolCallBuilder {
	if builders[index] == nil {
		builders[index] = &toolCallBuilder{}
	}
	return builders[index]
}

func mergeOpenAIToolCalls(builders map[int]*toolCallBuilder, calls []any) {
	for position, callValue := range calls {
		call := asMap(callValue)
		index := position
		if call["index"] != nil {
			index = asInt(call["index"])
		}
		builder := getToolBuilder(builders, index)
		builder.ID = firstNonEmpty(builder.ID, asString(call["id"]))
		function := asMap(call["function"])
		builder.Name = firstNonEmpty(builder.Name, asString(function["name"]))
		builder.Arguments.WriteString(asString(function["arguments"]))
	}
}

func builtToolCalls(builders map[int]*toolCallBuilder) []ToolCall {
	indexes := make([]int, 0, len(builders))
	for index := range builders {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	calls := make([]ToolCall, 0, len(indexes))
	for _, index := range indexes {
		builder := builders[index]
		input := builder.Input
		if builder.Arguments.Len() > 0 {
			input = parseJSONObject(builder.Arguments.String())
		} else if input == nil {
			input = map[string]any{}
		}
		calls = append(calls, ToolCall{ID: builder.ID, Name: builder.Name, Input: valueOrEmptyMap(input)})
	}
	return nonNilToolCalls(calls)
}

func consumeAnthropicBlock(text *strings.Builder, reasoning *reasoningAccumulator, builders map[int]*toolCallBuilder, index int, block map[string]any) {
	switch asString(block["type"]) {
	case "text":
		text.WriteString(asString(block["text"]))
	case "thinking":
		reasoning.addText(asString(block["thinking"]), "thinking", "anthropic.block")
	case "redacted_thinking":
		reasoning.addRedacted(block, "anthropic.block")
	case "tool_use":
		builder := getToolBuilder(builders, index)
		builder.ID = asString(block["id"])
		builder.Name = asString(block["name"])
		builder.Input = block["input"]
	}
}

type reasoningAccumulator struct {
	text     strings.Builder
	blocks   []ContentBlock
	seen     bool
	redacted bool
	source   string
}

func (r *reasoningAccumulator) addText(value, blockType, source string) {
	if r == nil || value == "" {
		return
	}
	r.seen = true
	if r.source == "" {
		r.source = source
	}
	r.text.WriteString(value)
	key := "text"
	if blockType == "thinking" {
		key = "thinking"
	}
	r.blocks = append(r.blocks, ContentBlock{"type": blockType, key: value})
}

func (r *reasoningAccumulator) addRedacted(block map[string]any, source string) {
	if r == nil {
		return
	}
	r.seen = true
	r.redacted = true
	if r.source == "" {
		r.source = source
	}
	value := ContentBlock{"type": "redacted_thinking", "redacted": true}
	if signature := asString(block["signature"]); signature != "" {
		value["signature"] = signature
	}
	r.blocks = append(r.blocks, value)
}

func (r *reasoningAccumulator) consumeValue(value any, source string) {
	if r == nil || value == nil {
		return
	}
	switch typed := value.(type) {
	case string:
		r.addText(typed, "reasoning", source)
	case map[string]any:
		r.consumeMap(typed, source)
	case []any:
		for _, item := range typed {
			r.consumeValue(item, source)
		}
	}
}

func (r *reasoningAccumulator) consumeMap(value map[string]any, source string) {
	if r == nil || len(value) == 0 {
		return
	}
	typeName := asString(value["type"])
	if typeName == "thinking" || typeName == "reasoning" || typeName == "reasoning_summary_text" {
		text := firstNonEmpty(asString(value["text"]), asString(value["thinking"]), asString(value["summary_text"]))
		if text != "" {
			blockType := "reasoning"
			if typeName == "thinking" {
				blockType = "thinking"
			}
			r.addText(text, blockType, source)
		}
	}
	if typeName == "redacted_thinking" || typeName == "redacted_reasoning" {
		r.addRedacted(value, source)
	}
	for _, key := range []string{"reasoning_content", "reasoning", "thinking"} {
		if item, ok := value[key]; ok {
			r.consumeValue(item, source)
		}
	}
	for _, key := range []string{"reasoning_details", "summary", "content"} {
		for _, item := range asSlice(value[key]) {
			block := asMap(item)
			typeName := asString(block["type"])
			if typeName == "redacted_thinking" || typeName == "redacted_reasoning" {
				r.addRedacted(block, source)
				continue
			}
			if key == "content" && typeName != "thinking" && typeName != "reasoning" {
				continue
			}
			text := firstNonEmpty(asString(block["text"]), asString(block["thinking"]), asString(block["summary_text"]))
			if text != "" {
				r.addText(text, "reasoning", source)
			}
		}
	}
	for _, item := range asSlice(value["content"]) {
		block := asMap(item)
		typeName := asString(block["type"])
		if typeName == "thinking" || typeName == "reasoning" {
			r.consumeMap(block, source)
		} else if typeName == "redacted_thinking" || typeName == "redacted_reasoning" {
			r.addRedacted(block, source)
		}
	}
}

func (r *reasoningAccumulator) result() (*Reasoning, string) {
	if r == nil || !r.seen {
		return nil, ReasoningStatusNotRequested
	}
	status := ReasoningStatusCaptured
	visibility := "captured"
	if r.redacted {
		status = ReasoningStatusRedacted
		visibility = "redacted"
	}
	return &Reasoning{
		Content:    optionalString(r.text.String()),
		Blocks:     nonNilBlocks(r.blocks),
		Visibility: visibility,
		Source:     firstNonEmpty(r.source, "provider_event"),
	}, status
}

func mergeOpenAIUsage(target *Usage, value map[string]any) {
	if len(value) == 0 {
		return
	}
	target.InputTokens = maxInt(target.InputTokens, firstPositiveInt(value["prompt_tokens"], value["input_tokens"]))
	target.OutputTokens = maxInt(target.OutputTokens, firstPositiveInt(value["completion_tokens"], value["output_tokens"]))
	details := asMap(value["prompt_tokens_details"])
	if len(details) == 0 {
		details = asMap(value["input_tokens_details"])
	}
	target.Cache.CacheReadInputTokens = maxInt(target.Cache.CacheReadInputTokens, asInt(details["cached_tokens"]))
	creation := maxInt(asInt(details["cached_creation_tokens"]), asInt(details["cache_write_tokens"]))
	target.Cache.CacheCreationInputTokens = maxInt(target.Cache.CacheCreationInputTokens, creation)
	target.Cache.CacheCreation.Ephemeral5mInputTokens = target.Cache.CacheCreationInputTokens
}

func mergeAnthropicUsage(target *Usage, value map[string]any) {
	if len(value) == 0 {
		return
	}
	target.InputTokens = maxInt(target.InputTokens, asInt(value["input_tokens"]))
	target.OutputTokens = maxInt(target.OutputTokens, asInt(value["output_tokens"]))
	target.Cache.CacheReadInputTokens = maxInt(target.Cache.CacheReadInputTokens, asInt(value["cache_read_input_tokens"]))
	creation5m := asInt(value["cache_creation_input_tokens"])
	creation := asMap(value["cache_creation"])
	if len(creation) > 0 {
		creation5m = maxInt(creation5m, asInt(creation["ephemeral_5m_input_tokens"]))
		target.Cache.CacheCreation.Ephemeral1hInputTokens = maxInt(target.Cache.CacheCreation.Ephemeral1hInputTokens, asInt(creation["ephemeral_1h_input_tokens"]))
	}
	target.Cache.CacheCreation.Ephemeral5mInputTokens = maxInt(target.Cache.CacheCreation.Ephemeral5mInputTokens, creation5m)
	target.Cache.CacheCreationInputTokens = target.Cache.CacheCreation.Ephemeral5mInputTokens + target.Cache.CacheCreation.Ephemeral1hInputTokens
}

func mergeGeminiUsage(target *Usage, value map[string]any) {
	if len(value) == 0 {
		return
	}
	target.InputTokens = maxInt(target.InputTokens, asInt(value["promptTokenCount"]))
	target.OutputTokens = maxInt(target.OutputTokens, asInt(value["candidatesTokenCount"]))
	target.Cache.CacheReadInputTokens = maxInt(target.Cache.CacheReadInputTokens, asInt(value["cachedContentTokenCount"]))
}

func mapStopReason(raw string, hasTools bool) *string {
	if hasTools {
		value := "tool_use"
		return &value
	}
	var value string
	switch strings.ToLower(raw) {
	case "stop", "end_turn", "completed", "complete", "stop_sequence", "finish_reason_unspecified":
		value = "end_turn"
	case "length", "max_tokens", "max_output_tokens":
		value = "max_tokens"
	case "tool_calls", "function_call", "tool_use":
		value = "tool_use"
	case "safety", "content_filter", "recitation", "blocked":
		value = "stop_sequence"
	default:
		if raw == "" {
			return nil
		}
		value = "end_turn"
	}
	return &value
}

func asInt(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case json.Number:
		result, _ := strconv.Atoi(typed.String())
		return result
	case string:
		result, _ := strconv.Atoi(typed)
		return result
	default:
		return 0
	}
}

func firstPositiveInt(values ...any) int {
	for _, value := range values {
		if result := asInt(value); result > 0 {
			return result
		}
	}
	return 0
}

func maxInt(a, b int) int {
	if b > a {
		return b
	}
	return a
}

func valueOrEmptyMap(value any) any {
	if value == nil {
		return map[string]any{}
	}
	return value
}
