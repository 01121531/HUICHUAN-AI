package datasetcapture

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

var (
	ErrUnsupportedProtocol = errors.New("unsupported dataset capture protocol")
	ErrIncompleteCapture   = errors.New("dataset capture is incomplete")
	ErrInvalidCapture      = errors.New("dataset capture is invalid")
)

func Normalize(capture Capture) (Record, error) {
	return normalizeProtocols(capture, capture.Path, capture.Path)
}

// normalizeProtocols allows the final successful upstream request and the
// client-facing response to use different wire protocols. This lets the relay
// preserve effective model/system/tool semantics without buffering the raw
// upstream response before it is delivered to the client.
func normalizeProtocols(capture Capture, requestPath, responsePath string) (Record, error) {
	requestProtocol := detectProtocol(requestPath, capture.RequestBody)
	if requestProtocol == "" {
		return Record{}, ErrUnsupportedProtocol
	}
	request, err := normalizeRequest(requestProtocol, capture.RequestBody)
	if err != nil {
		return Record{}, fmt.Errorf("%w: request: %v", ErrInvalidCapture, err)
	}
	responseProtocol := detectProtocol(responsePath, nil)
	if responseProtocol == "" {
		responseProtocol = requestProtocol
	}
	response, err := normalizeResponse(responseProtocol, capture.ResponseBody)
	if err != nil {
		return Record{}, fmt.Errorf("%w: response: %v", ErrInvalidCapture, err)
	}
	if !response.Complete {
		return Record{}, ErrIncompleteCapture
	}
	if response.Content == nil && len(response.ToolCalls) == 0 {
		return Record{}, ErrInvalidCapture
	}

	model := firstNonEmpty(request.Model, capture.Model)
	if model == "" && requestProtocol == "gemini" {
		model = modelFromPath(requestPath)
	}
	if model == "" || len(request.Messages) == 0 {
		return Record{}, ErrInvalidCapture
	}
	createdAt := capture.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	createdAtString := createdAt.Format(time.RFC3339Nano)
	sessionSource := firstNonEmpty(request.SessionSource, capture.SessionSource, capture.RequestID)
	if sessionSource == "" {
		sessionSource = fmt.Sprintf("%s:%d", model, createdAt.UnixNano())
	}
	sessionID := shortHMAC(sessionSource, capture.HMACKey)
	userIDHash := optionalHMAC(capture.UserID, capture.HMACKey)
	userAgent := optionalString(capture.UserAgent)
	cwd := optionalString(capture.CWD)

	record := Record{
		SessionID:    sessionID,
		UserIDHash:   userIDHash,
		Model:        model,
		UserAgent:    userAgent,
		SystemPrompt: request.SystemPrompt,
		Tools:        nonNilTools(request.Tools),
		Messages:     request.Messages,
		Response: Response{
			Content:    response.Content,
			StopReason: response.StopReason,
			ToolUse: ToolUse{
				InputAlreadyMerged: true,
				Calls:              nonNilToolCalls(response.ToolCalls),
			},
			Usage: response.Usage,
		},
		CreatedAt: &createdAtString,
		CWD:       cwd,
		Meta: Meta{
			Version:               SchemaVersion,
			SourceRoute:           firstNonEmpty(capture.Route, requestProtocol),
			SourceFile:            "",
			SourceRow:             0,
			SnapshotsInSession:    len(request.Messages),
			SystemPromptSource:    request.SystemPromptSource,
			UserQuery:             lastUserQuery(request.Messages),
			RawFinishReason:       response.RawFinishReason,
			IsColdStartSimulation: false,
		},
		Storage: StorageScope{
			UserKey:        storageScopeKey(capture.UserID),
			TokenKey:       storageScopeKey(capture.TokenID),
			UserGroup:      strings.TrimSpace(capture.UserGroup),
			RequestedModel: strings.TrimSpace(capture.RequestedModel),
			ChannelID:      capture.ChannelID,
		},
	}
	if capture.StripMultimodalBase64 {
		stripMultimodalBase64(&record)
	}
	return record, Validate(record)
}

func stripMultimodalBase64(record *Record) {
	if record == nil {
		return
	}
	for messageIndex := range record.Messages {
		for blockIndex := range record.Messages[messageIndex].Content {
			block := record.Messages[messageIndex].Content[blockIndex]
			if source, ok := block["source"].(map[string]any); ok {
				stripBase64Source(source)
			}
		}
	}
}

func stripBase64Source(value map[string]any) {
	if value == nil {
		return
	}
	if asString(value["type"]) == "base64" {
		delete(value, "data")
		value["omitted"] = true
	}
	for key, item := range value {
		switch nested := item.(type) {
		case map[string]any:
			stripBase64Source(nested)
		case string:
			if (key == "url" || key == "data") && strings.HasPrefix(strings.ToLower(strings.TrimSpace(nested)), "data:") {
				delete(value, key)
				value["omitted"] = true
			}
		}
	}
}

func Validate(record Record) error {
	if len(record.SessionID) != 16 || record.Model == "" || record.CreatedAt == nil {
		return ErrInvalidCapture
	}
	if record.Tools == nil || record.Messages == nil || record.Response.ToolUse.Calls == nil {
		return ErrInvalidCapture
	}
	for _, message := range record.Messages {
		if (message.Role != "user" && message.Role != "assistant") || message.Content == nil {
			return ErrInvalidCapture
		}
	}
	return nil
}

func detectProtocol(path string, body []byte) string {
	lowerPath := strings.ToLower(path)
	switch {
	case strings.Contains(lowerPath, "generatecontent"), strings.Contains(lowerPath, "streamgeneratecontent"):
		return "gemini"
	case strings.HasSuffix(lowerPath, "/messages"):
		return "anthropic"
	case strings.Contains(lowerPath, "/responses"):
		return "openai-responses"
	case strings.Contains(lowerPath, "/chat/completions"):
		return "openai-chat"
	}
	if !gjson.ValidBytes(body) {
		return ""
	}
	if gjson.GetBytes(body, "contents").Exists() {
		return "gemini"
	}
	if gjson.GetBytes(body, "messages").Exists() {
		if gjson.GetBytes(body, "system").Exists() {
			return "anthropic"
		}
		return "openai-chat"
	}
	if gjson.GetBytes(body, "input").Exists() {
		return "openai-responses"
	}
	return ""
}

func IsSupportedPath(value string) bool {
	lower := strings.TrimSuffix(strings.ToLower(value), "/")
	return strings.HasSuffix(lower, "/chat/completions") ||
		strings.HasSuffix(lower, "/responses") ||
		strings.HasSuffix(lower, "/messages") ||
		strings.Contains(lower, "generatecontent")
}

type RequestMetadata struct {
	Model  string
	Stream bool
}

// InspectRequest performs the only request-body scan needed by the capture hot
// path. It extracts shallow policy/session fields without materializing the
// messages, tools, or multimodal payload into a generic object graph.
func InspectRequest(path string, body []byte) (RequestMetadata, error) {
	protocol := detectProtocol(path, body)
	if protocol == "" {
		return RequestMetadata{}, ErrUnsupportedProtocol
	}
	// GetBytes uses gjson's zero-copy []byte path. Two shallow lookups avoid the
	// full-body allocation caused by ParseBytes on large inline Base64 payloads.
	metadata := RequestMetadata{
		Model:  strings.TrimSpace(gjson.GetBytes(body, "model").String()),
		Stream: gjson.GetBytes(body, "stream").Bool() || strings.Contains(strings.ToLower(path), "streamgeneratecontent"),
	}
	if metadata.Model == "" && protocol == "gemini" {
		metadata.Model = modelFromPath(path)
	}
	if metadata.Model == "" {
		return RequestMetadata{}, fmt.Errorf("%w: request model is empty", ErrInvalidCapture)
	}
	return metadata, nil
}

func RequestedModel(path string, body []byte) (string, error) {
	metadata, err := InspectRequest(path, body)
	return metadata.Model, err
}

func RequestIsStream(path string, body []byte) bool {
	return gjson.GetBytes(body, "stream").Bool() || strings.Contains(strings.ToLower(path), "streamgeneratecontent")
}

func normalizeRequest(protocol string, body []byte) (normalizedRequest, error) {
	var object map[string]any
	if err := json.Unmarshal(body, &object); err != nil {
		return normalizedRequest{}, err
	}
	switch protocol {
	case "anthropic":
		return normalizeAnthropicRequest(object), nil
	case "gemini":
		return normalizeGeminiRequest(object), nil
	case "openai-chat":
		return normalizeOpenAIRequest(object, false), nil
	case "openai-responses":
		return normalizeOpenAIRequest(object, true), nil
	default:
		return normalizedRequest{}, ErrUnsupportedProtocol
	}
}

func normalizeOpenAIRequest(object map[string]any, responses bool) normalizedRequest {
	result := normalizedRequest{
		Model:              asString(object["model"]),
		SystemPromptSource: "none",
		Tools:              normalizeOpenAITools(asSlice(object["tools"])),
	}
	result.SessionSource = sessionSource(object)
	if responses {
		if instructions := contentText(object["instructions"]); instructions != "" {
			result.SystemPrompt = instructions
			result.SystemPromptSource = "request.instructions"
		}
		input := object["input"]
		if text, ok := input.(string); ok {
			result.Messages = []Message{{Role: "user", Content: []ContentBlock{textBlock(text)}}}
		} else {
			result.Messages = normalizeOpenAIMessages(asSlice(input), &result)
		}
		return result
	}
	result.Messages = normalizeOpenAIMessages(asSlice(object["messages"]), &result)
	return result
}

func normalizeOpenAIMessages(items []any, request *normalizedRequest) []Message {
	messages := make([]Message, 0, len(items))
	for _, item := range items {
		message := asMap(item)
		role := asString(message["role"])
		if role == "" {
			switch asString(message["type"]) {
			case "function_call":
				messages = append(messages, Message{Role: "assistant", Content: []ContentBlock{{
					"type":  "tool_use",
					"id":    firstNonEmpty(asString(message["call_id"]), asString(message["id"])),
					"name":  asString(message["name"]),
					"input": parseJSONObject(asString(message["arguments"])),
				}}})
			case "function_call_output":
				messages = append(messages, Message{Role: "user", Content: []ContentBlock{{
					"type":        "tool_result",
					"tool_use_id": firstNonEmpty(asString(message["call_id"]), asString(message["id"])),
					"content":     normalizeContent(message["output"]),
				}}})
			}
			continue
		}
		if role == "system" || role == "developer" {
			text := contentText(message["content"])
			if text != "" {
				request.SystemPrompt = joinPrompt(request.SystemPrompt, text)
				request.SystemPromptSource = "messages." + role
			}
			continue
		}
		if role == "tool" {
			block := ContentBlock{
				"type":        "tool_result",
				"tool_use_id": asString(message["tool_call_id"]),
				"content":     normalizeContent(message["content"]),
			}
			messages = append(messages, Message{Role: "user", Content: []ContentBlock{block}})
			continue
		}
		if role != "user" && role != "assistant" {
			continue
		}
		blocks := normalizeContent(message["content"])
		if role == "assistant" {
			for _, callValue := range asSlice(message["tool_calls"]) {
				call := asMap(callValue)
				function := asMap(call["function"])
				blocks = append(blocks, ContentBlock{
					"type":  "tool_use",
					"id":    asString(call["id"]),
					"name":  asString(function["name"]),
					"input": parseJSONObject(asString(function["arguments"])),
				})
			}
		}
		messages = append(messages, Message{Role: role, Content: nonNilBlocks(blocks)})
	}
	return messages
}

func normalizeAnthropicRequest(object map[string]any) normalizedRequest {
	result := normalizedRequest{
		Model:              asString(object["model"]),
		SessionSource:      sessionSource(object),
		SystemPromptSource: "none",
		Tools:              normalizeAnthropicTools(asSlice(object["tools"])),
	}
	if system := contentText(object["system"]); system != "" {
		result.SystemPrompt = system
		result.SystemPromptSource = "request.system"
	}
	for _, item := range asSlice(object["messages"]) {
		message := asMap(item)
		role := asString(message["role"])
		if role != "user" && role != "assistant" {
			continue
		}
		result.Messages = append(result.Messages, Message{Role: role, Content: nonNilBlocks(normalizeContent(message["content"]))})
	}
	return result
}

func normalizeGeminiRequest(object map[string]any) normalizedRequest {
	result := normalizedRequest{
		Model:              asString(object["model"]),
		SessionSource:      sessionSource(object),
		SystemPromptSource: "none",
		Tools:              normalizeGeminiTools(asSlice(object["tools"])),
	}
	if system := contentText(asMap(object["systemInstruction"])["parts"]); system != "" {
		result.SystemPrompt = system
		result.SystemPromptSource = "request.systemInstruction"
	}
	for _, item := range asSlice(object["contents"]) {
		content := asMap(item)
		role := asString(content["role"])
		if role == "model" {
			role = "assistant"
		}
		if role == "" {
			role = "user"
		}
		if role != "user" && role != "assistant" {
			continue
		}
		result.Messages = append(result.Messages, Message{Role: role, Content: nonNilBlocks(normalizeGeminiParts(asSlice(content["parts"])))})
	}
	return result
}

func normalizeOpenAITools(items []any) []Tool {
	tools := make([]Tool, 0, len(items))
	for _, item := range items {
		value := asMap(item)
		function := asMap(value["function"])
		if len(function) == 0 && asString(value["type"]) == "function" {
			function = value
		}
		name := asString(function["name"])
		if name == "" {
			continue
		}
		schema := function["parameters"]
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		tools = append(tools, Tool{Name: name, Description: asString(function["description"]), InputSchema: schema})
	}
	return nonNilTools(tools)
}

func normalizeAnthropicTools(items []any) []Tool {
	tools := make([]Tool, 0, len(items))
	for _, item := range items {
		value := asMap(item)
		name := asString(value["name"])
		if name == "" {
			continue
		}
		schema := value["input_schema"]
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		tools = append(tools, Tool{Name: name, Description: asString(value["description"]), InputSchema: schema})
	}
	return nonNilTools(tools)
}

func normalizeGeminiTools(items []any) []Tool {
	var tools []Tool
	for _, item := range items {
		for _, declarationValue := range asSlice(asMap(item)["functionDeclarations"]) {
			declaration := asMap(declarationValue)
			name := asString(declaration["name"])
			if name == "" {
				continue
			}
			schema := declaration["parameters"]
			if schema == nil {
				schema = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			tools = append(tools, Tool{Name: name, Description: asString(declaration["description"]), InputSchema: schema})
		}
	}
	return nonNilTools(tools)
}

func normalizeContent(value any) []ContentBlock {
	if value == nil {
		return []ContentBlock{}
	}
	if text, ok := value.(string); ok {
		return []ContentBlock{textBlock(text)}
	}
	items := asSlice(value)
	if items == nil {
		return []ContentBlock{{"type": "text", "text": fmt.Sprint(value)}}
	}
	blocks := make([]ContentBlock, 0, len(items))
	for _, item := range items {
		block := asMap(item)
		if len(block) == 0 {
			blocks = append(blocks, textBlock(fmt.Sprint(item)))
			continue
		}
		typeName := asString(block["type"])
		switch typeName {
		case "text", "input_text", "output_text":
			blocks = append(blocks, textBlock(firstNonEmpty(asString(block["text"]), asString(block["content"]))))
		case "image_url", "input_image", "input_audio", "file", "video_url":
			blocks = append(blocks, ContentBlock{"type": typeName, "source": block})
		default:
			blocks = append(blocks, ContentBlock(block))
		}
	}
	return blocks
}

func normalizeGeminiParts(items []any) []ContentBlock {
	blocks := make([]ContentBlock, 0, len(items))
	for _, item := range items {
		part := asMap(item)
		switch {
		case part["text"] != nil:
			blocks = append(blocks, textBlock(asString(part["text"])))
		case part["functionCall"] != nil:
			call := asMap(part["functionCall"])
			blocks = append(blocks, ContentBlock{"type": "tool_use", "id": asString(call["id"]), "name": asString(call["name"]), "input": call["args"]})
		case part["functionResponse"] != nil:
			response := asMap(part["functionResponse"])
			blocks = append(blocks, ContentBlock{"type": "tool_result", "tool_use_id": firstNonEmpty(asString(response["id"]), asString(response["name"])), "content": response["response"]})
		case part["inlineData"] != nil:
			data := asMap(part["inlineData"])
			blocks = append(blocks, ContentBlock{"type": "media", "source": map[string]any{"type": "base64", "media_type": data["mimeType"], "data": data["data"]}})
		case part["fileData"] != nil:
			data := asMap(part["fileData"])
			blocks = append(blocks, ContentBlock{"type": "media", "source": map[string]any{"type": "url", "media_type": data["mimeType"], "url": data["fileUri"]}})
		default:
			blocks = append(blocks, ContentBlock(part))
		}
	}
	return blocks
}

func textBlock(text string) ContentBlock { return ContentBlock{"type": "text", "text": text} }

func contentText(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	var texts []string
	for _, block := range normalizeContent(value) {
		if asString(block["type"]) == "text" {
			texts = append(texts, asString(block["text"]))
		}
	}
	return strings.Join(texts, "")
}

func lastUserQuery(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		var texts []string
		for _, block := range messages[i].Content {
			if asString(block["type"]) == "text" {
				texts = append(texts, asString(block["text"]))
			}
		}
		if len(texts) > 0 {
			return strings.Join(texts, "")
		}
	}
	return ""
}

func sessionSource(object map[string]any) string {
	metadata := asMap(object["metadata"])
	return firstNonEmpty(asString(object["session_id"]), asString(object["conversation_id"]), asString(metadata["session_id"]), asString(metadata["conversation_id"]))
}

func sessionSourceFromBody(body []byte) string {
	values := gjson.GetManyBytes(body,
		"session_id",
		"conversation_id",
		"metadata.session_id",
		"metadata.conversation_id",
	)
	return firstNonEmpty(values[0].String(), values[1].String(), values[2].String(), values[3].String())
}

func storageScopeKey(value string) string {
	if strings.TrimSpace(value) == "" {
		return "anonymous"
	}
	if value == "playground" {
		return value
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return "anonymous"
		}
	}
	value = strings.TrimLeft(value, "0")
	if value == "" {
		return "anonymous"
	}
	return value
}

func parseJSONObject(value string) any {
	if value == "" {
		return map[string]any{}
	}
	var result any
	if json.Unmarshal([]byte(value), &result) == nil {
		return result
	}
	return value
}

func shortHMAC(value, key string) string {
	return fullHMAC(value, key)[:16]
}

func optionalHMAC(value, key string) *string {
	if value == "" {
		return nil
	}
	hash := fullHMAC(value, key)
	return &hash
}

func fullHMAC(value, key string) string {
	hash := hmac.New(sha256.New, []byte(key))
	_, _ = hash.Write([]byte(value))
	return hex.EncodeToString(hash.Sum(nil))
}

func optionalString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func asMap(value any) map[string]any {
	result, _ := value.(map[string]any)
	return result
}

func asSlice(value any) []any {
	result, _ := value.([]any)
	return result
}

func asString(value any) string {
	result, _ := value.(string)
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func modelFromPath(value string) string {
	lower := strings.ToLower(value)
	marker := "/models/"
	index := strings.Index(lower, marker)
	if index < 0 {
		return ""
	}
	model := value[index+len(marker):]
	if colon := strings.IndexByte(model, ':'); colon >= 0 {
		model = model[:colon]
	}
	if slash := strings.IndexByte(model, '/'); slash >= 0 {
		model = model[:slash]
	}
	return model
}

func joinPrompt(current, addition string) string {
	if current == "" {
		return addition
	}
	return current + "\n\n" + addition
}

func nonNilBlocks(value []ContentBlock) []ContentBlock {
	if value == nil {
		return []ContentBlock{}
	}
	return value
}

func nonNilTools(value []Tool) []Tool {
	if value == nil {
		return []Tool{}
	}
	return value
}

func nonNilToolCalls(value []ToolCall) []ToolCall {
	if value == nil {
		return []ToolCall{}
	}
	return value
}

func sourceFile(path string) string { return filepath.Base(path) }
