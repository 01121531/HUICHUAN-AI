package datasetcapture

import "time"

const SchemaVersion = "v1"

type ContentBlock map[string]any

type Message struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema"`
}

type ToolCall struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name"`
	Input any    `json:"input"`
}

type ToolUse struct {
	InputAlreadyMerged bool       `json:"input_already_merged"`
	Calls              []ToolCall `json:"calls"`
}

type CacheCreation struct {
	Ephemeral5mInputTokens int `json:"ephemeral_5m_input_tokens"`
	Ephemeral1hInputTokens int `json:"ephemeral_1h_input_tokens"`
}

type CacheUsage struct {
	CacheReadInputTokens     int           `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int           `json:"cache_creation_input_tokens"`
	CacheCreation            CacheCreation `json:"cache_creation"`
}

type Usage struct {
	InputTokens  int        `json:"input_tokens"`
	OutputTokens int        `json:"output_tokens"`
	Cache        CacheUsage `json:"cache"`
}

type Response struct {
	Content    *string `json:"content"`
	StopReason *string `json:"stop_reason"`
	ToolUse    ToolUse `json:"tool_use"`
	Usage      Usage   `json:"usage"`
}

type Meta struct {
	Version               string  `json:"v"`
	SourceRoute           string  `json:"source_route"`
	SourceFile            string  `json:"source_file"`
	SourceRow             int64   `json:"source_row"`
	SnapshotsInSession    int     `json:"snapshots_in_session"`
	SystemPromptSource    string  `json:"system_prompt_source"`
	UserQuery             string  `json:"user_query"`
	RawFinishReason       *string `json:"raw_finish_reason"`
	IsColdStartSimulation bool    `json:"is_cold_start_simulation"`
}

// Record intentionally has the same eleven top-level fields, in the same
// order, as the reference sample.jsonl dataset.
type Record struct {
	SessionID    string       `json:"session_id"`
	UserIDHash   *string      `json:"user_id_hash"`
	Model        string       `json:"model"`
	UserAgent    *string      `json:"user_agent"`
	SystemPrompt string       `json:"system_prompt"`
	Tools        []Tool       `json:"tools"`
	Messages     []Message    `json:"messages"`
	Response     Response     `json:"response"`
	CreatedAt    *string      `json:"created_at"`
	CWD          *string      `json:"cwd"`
	Meta         Meta         `json:"_meta"`
	Storage      StorageScope `json:"-"`
}

// StorageScope routes a record to its privacy-preserving on-disk partition.
// It is operational metadata and is never serialized into the training sample.
type StorageScope struct {
	UserKey        string
	TokenKey       string
	UserGroup      string
	RequestedModel string
	ChannelID      int
}

type Capture struct {
	RequestBody    []byte
	ResponseBody   []byte
	Path           string
	ContentType    string
	Model          string
	Route          string
	RequestID      string
	UserID         string
	TokenID        string
	UserGroup      string
	RequestedModel string
	ChannelID      int
	SessionSource  string
	UserAgent      string
	CWD            string
	HMACKey        string
	CreatedAt      time.Time
}

type normalizedRequest struct {
	SessionSource      string
	Model              string
	SystemPrompt       string
	SystemPromptSource string
	Tools              []Tool
	Messages           []Message
}

type normalizedResponse struct {
	Content         *string
	StopReason      *string
	RawFinishReason *string
	ToolCalls       []ToolCall
	Usage           Usage
	Complete        bool
}
