package codex

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/dto"
	"github.com/01121531/HUICHUAN-AI/relay/channel"
	"github.com/01121531/HUICHUAN-AI/relay/channel/openai"
	relaycommon "github.com/01121531/HUICHUAN-AI/relay/common"
	relayconstant "github.com/01121531/HUICHUAN-AI/relay/constant"
	"github.com/01121531/HUICHUAN-AI/types"

	"github.com/gin-gonic/gin"
)

type Adaptor struct {
}

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	return nil, errors.New("codex channel: endpoint not supported")
}

func (a *Adaptor) ConvertClaudeRequest(*gin.Context, *relaycommon.RelayInfo, *dto.ClaudeRequest) (any, error) {
	return nil, errors.New("codex channel: /v1/messages endpoint not supported")
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	return nil, errors.New("codex channel: endpoint not supported")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	return nil, errors.New("codex channel: endpoint not supported")
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	return nil, errors.New("codex channel: /v1/chat/completions endpoint not supported")
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, errors.New("codex channel: /v1/rerank endpoint not supported")
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return nil, errors.New("codex channel: /v1/embeddings endpoint not supported")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	isCompact := info != nil && info.RelayMode == relayconstant.RelayModeResponsesCompact

	if info != nil && info.ChannelSetting.SystemPrompt != "" {
		systemPrompt := info.ChannelSetting.SystemPrompt

		if len(request.Instructions) == 0 {
			if b, err := common.Marshal(systemPrompt); err == nil {
				request.Instructions = b
			} else {
				return nil, err
			}
		} else if info.ChannelSetting.SystemPromptOverride {
			var existing string
			if err := common.Unmarshal(request.Instructions, &existing); err == nil {
				existing = strings.TrimSpace(existing)
				if existing == "" {
					if b, err := common.Marshal(systemPrompt); err == nil {
						request.Instructions = b
					} else {
						return nil, err
					}
				} else {
					if b, err := common.Marshal(systemPrompt + "\n" + existing); err == nil {
						request.Instructions = b
					} else {
						return nil, err
					}
				}
			} else {
				if b, err := common.Marshal(systemPrompt); err == nil {
					request.Instructions = b
				} else {
					return nil, err
				}
			}
		}
	}
	if err := applyNERVCodexInstructions(&request); err != nil {
		return nil, err
	}
	// Codex backend requires the `instructions` field to be present.
	// Keep it consistent with Codex CLI behavior by defaulting to an empty string.
	if len(request.Instructions) == 0 {
		request.Instructions = json.RawMessage(`""`)
	}

	if isCompact {
		return request, nil
	}
	// codex: store must be false
	request.Store = json.RawMessage("false")
	// rm max_output_tokens
	request.MaxOutputTokens = nil
	request.Temperature = nil
	return request, nil
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.HUICHUANError) {
	if info.RelayMode != relayconstant.RelayModeResponses && info.RelayMode != relayconstant.RelayModeResponsesCompact {
		return nil, types.NewError(errors.New("codex channel: endpoint not supported"), types.ErrorCodeInvalidRequest)
	}

	if info.RelayMode == relayconstant.RelayModeResponsesCompact {
		return openai.OaiResponsesCompactionHandler(c, resp)
	}

	if info.IsStream {
		return openai.OaiResponsesStreamHandler(c, info, resp)
	}
	return openai.OaiResponsesHandler(c, info, resp)
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}

const (
	nervCodexEnabledKey = "nerv_setting.enabled"
	nervCodexPromptKey  = "nerv_setting.prompt"
	nervCodexModeKey    = "nerv_setting.mode"
	nervCodexModelsKey  = "nerv_setting.models"
)

func applyNERVCodexInstructions(request *dto.OpenAIResponsesRequest) error {
	if request == nil {
		return nil
	}
	enabled, prompt, mode, models := getNERVCodexOptions()
	if !enabled || !nervCodexModelAllowed(request.Model, models) {
		return nil
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil
	}

	existing, hasExisting := decodeResponsesInstructions(request.Instructions)
	combined := combineNERVCodexInstructions(prompt, existing, hasExisting, mode)
	data, err := common.Marshal(combined)
	if err != nil {
		return err
	}
	request.Instructions = data
	return nil
}

func getNERVCodexOptions() (enabled bool, prompt string, mode string, models string) {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return common.OptionMap[nervCodexEnabledKey] == "true",
		common.OptionMap[nervCodexPromptKey],
		common.OptionMap[nervCodexModeKey],
		common.OptionMap[nervCodexModelsKey]
}

func decodeResponsesInstructions(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return "", false
	}
	var existing string
	if err := common.Unmarshal(raw, &existing); err == nil {
		existing = strings.TrimSpace(existing)
		return existing, existing != ""
	}
	return strings.TrimSpace(string(raw)), true
}

func combineNERVCodexInstructions(prompt, existing string, hasExisting bool, mode string) string {
	if !hasExisting {
		return prompt
	}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "override":
		return prompt
	case "append":
		return existing + "\n\n" + prompt
	default:
		return prompt + "\n\n" + existing
	}
}

func nervCodexModelAllowed(model string, patterns string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	patterns = strings.TrimSpace(patterns)
	if patterns == "" || patterns == "*" {
		return true
	}
	for _, pattern := range strings.FieldsFunc(patterns, func(r rune) bool {
		return r == ',' || r == '\n' || r == ';'
	}) {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		if matchNERVCodexPattern(model, pattern) {
			return true
		}
	}
	return false
}

func matchNERVCodexPattern(model string, pattern string) bool {
	if pattern == "*" || pattern == model {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return false
	}
	parts := strings.SplitN(pattern, "*", 2)
	prefix := parts[0]
	suffix := parts[1]
	return (prefix == "" || strings.HasPrefix(model, prefix)) &&
		(suffix == "" || strings.HasSuffix(model, suffix))
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if info.RelayMode != relayconstant.RelayModeResponses && info.RelayMode != relayconstant.RelayModeResponsesCompact {
		return "", errors.New("codex channel: only /v1/responses and /v1/responses/compact are supported")
	}
	path := "/backend-api/codex/responses"
	if info.RelayMode == relayconstant.RelayModeResponsesCompact {
		path = "/backend-api/codex/responses/compact"
	}
	return relaycommon.GetFullRequestURL(info.ChannelBaseUrl, path, info.ChannelType), nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)

	key := strings.TrimSpace(info.ApiKey)
	if !strings.HasPrefix(key, "{") {
		return errors.New("codex channel: key must be a JSON object")
	}

	oauthKey, err := ParseOAuthKey(key)
	if err != nil {
		return err
	}

	accessToken := strings.TrimSpace(oauthKey.AccessToken)
	accountID := strings.TrimSpace(oauthKey.AccountID)

	if accessToken == "" {
		return errors.New("codex channel: access_token is required")
	}
	if accountID == "" {
		return errors.New("codex channel: account_id is required")
	}

	req.Set("Authorization", "Bearer "+accessToken)
	req.Set("chatgpt-account-id", accountID)

	if req.Get("OpenAI-Beta") == "" {
		req.Set("OpenAI-Beta", "responses=experimental")
	}
	if req.Get("originator") == "" {
		req.Set("originator", "codex_cli_rs")
	}

	// chatgpt.com/backend-api/codex/responses is strict about Content-Type.
	// Clients may omit it or include parameters like `application/json; charset=utf-8`,
	// which can be rejected by the upstream. Force the exact media type.
	req.Set("Content-Type", "application/json")
	if info.IsStream {
		req.Set("Accept", "text/event-stream")
	} else if req.Get("Accept") == "" {
		req.Set("Accept", "application/json")
	}

	return nil
}
