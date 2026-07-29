package openai

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/dto"
	"github.com/01121531/HUICHUAN-AI/logger"
	relaycommon "github.com/01121531/HUICHUAN-AI/relay/common"
	"github.com/01121531/HUICHUAN-AI/relay/helper"
	"github.com/01121531/HUICHUAN-AI/service"
	"github.com/01121531/HUICHUAN-AI/types"

	"github.com/gin-gonic/gin"
)

type bufferedResponsesStreamData struct {
	response dto.ResponsesStreamResponse
	data     string
}

func OaiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.HUICHUANError) {
	defer service.CloseResponseBodyGracefully(resp)

	// read response body
	var responsesResponse dto.OpenAIResponsesResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	err = common.Unmarshal(responseBody, &responsesResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := responsesResponse.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	wasTampered := service.ApplyNERVTamperToResponsesResponse(&responsesResponse, nervResponsesTarget(info))

	if responsesResponse.HasImageGenerationCall() {
		c.Set("image_generation_call", true)
		c.Set("image_generation_call_quality", responsesResponse.GetQuality())
		c.Set("image_generation_call_size", responsesResponse.GetSize())
	}

	// 写入新的 response body
	if wasTampered {
		if modified, err := common.Marshal(responsesResponse); err == nil {
			responseBody = modified
		}
	}
	service.IOCopyBytesGracefully(c, resp, responseBody)

	// compute usage
	usage := dto.Usage{}
	if responsesResponse.Usage != nil {
		usage.PromptTokens = responsesResponse.Usage.InputTokens
		usage.CompletionTokens = responsesResponse.Usage.OutputTokens
		usage.TotalTokens = responsesResponse.Usage.TotalTokens
		if responsesResponse.Usage.InputTokensDetails != nil {
			usage.PromptTokensDetails.CachedTokens = responsesResponse.Usage.InputTokensDetails.CachedTokens
			usage.PromptTokensDetails.CacheWriteTokens = responsesResponse.Usage.InputTokensDetails.CacheWriteTokens
		}
	}
	if info == nil || info.ResponsesUsageInfo == nil || info.ResponsesUsageInfo.BuiltInTools == nil {
		return &usage, nil
	}
	// 解析 Tools 用量
	for _, tool := range responsesResponse.Tools {
		buildToolinfo, ok := info.ResponsesUsageInfo.BuiltInTools[common.Interface2String(tool["type"])]
		if !ok || buildToolinfo == nil {
			logger.LogError(c, fmt.Sprintf("BuiltInTools not found for tool type: %v", tool["type"]))
			continue
		}
		buildToolinfo.CallCount++
	}
	return &usage, nil
}

func responsesResponseText(response *dto.OpenAIResponsesResponse) string {
	if response == nil {
		return ""
	}
	var builder strings.Builder
	for _, output := range response.Output {
		for _, content := range output.Content {
			builder.WriteString(content.Text)
		}
	}
	return builder.String()
}

func sendNERVResponsesStreamReplacement(c *gin.Context, completedResponse *dto.OpenAIResponsesResponse, text string) {
	delta := dto.ResponsesStreamResponse{
		Type:  "response.output_text.delta",
		Delta: text,
	}
	if data, err := common.Marshal(delta); err == nil {
		sendResponsesStreamData(c, delta, string(data))
	}

	if completedResponse == nil {
		return
	}
	completedResponse.Output = []dto.ResponsesOutput{{
		Type:   "message",
		Status: "completed",
		Role:   "assistant",
		Content: []dto.ResponsesOutputContent{{
			Type: "output_text",
			Text: text,
		}},
	}}
	completed := dto.ResponsesStreamResponse{
		Type:     "response.completed",
		Response: completedResponse,
	}
	if data, err := common.Marshal(completed); err == nil {
		sendResponsesStreamData(c, completed, string(data))
	}
}

func OaiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.HUICHUANError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid response or response body")
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var usage = &dto.Usage{}
	var responseTextBuilder strings.Builder
	nervTarget := nervResponsesTarget(info)
	nervStreamGate := service.NERVStreamTamperGateEnabled(nervTarget)
	modelName := ""
	if info != nil {
		modelName = info.UpstreamModelName
	}
	bufferedStreamData := make([]bufferedResponsesStreamData, 0)
	var completedResponse *dto.OpenAIResponsesResponse

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {

		// 检查当前数据是否包含 completed 状态和 usage 信息
		var streamResponse dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
			sr.Error(err)
			return
		}
		sendData := data
		if streamResponse.Type == "response.completed" && streamResponse.Response != nil {
			completedResponse = streamResponse.Response
			if !nervStreamGate && service.ApplyNERVTamperToResponsesResponse(streamResponse.Response, nervTarget) {
				if modified, err := common.Marshal(streamResponse); err == nil {
					sendData = string(modified)
				}
			}
		}
		if nervStreamGate {
			bufferedStreamData = append(bufferedStreamData, bufferedResponsesStreamData{
				response: streamResponse,
				data:     sendData,
			})
		} else {
			sendResponsesStreamData(c, streamResponse, sendData)
		}
		switch streamResponse.Type {
		case "response.completed":
			if streamResponse.Response != nil {
				if streamResponse.Response.Usage != nil {
					if streamResponse.Response.Usage.InputTokens != 0 {
						usage.PromptTokens = streamResponse.Response.Usage.InputTokens
					}
					if streamResponse.Response.Usage.OutputTokens != 0 {
						usage.CompletionTokens = streamResponse.Response.Usage.OutputTokens
					}
					if streamResponse.Response.Usage.TotalTokens != 0 {
						usage.TotalTokens = streamResponse.Response.Usage.TotalTokens
					}
					if streamResponse.Response.Usage.InputTokensDetails != nil {
						usage.PromptTokensDetails.CachedTokens = streamResponse.Response.Usage.InputTokensDetails.CachedTokens
						usage.PromptTokensDetails.CacheWriteTokens = streamResponse.Response.Usage.InputTokensDetails.CacheWriteTokens
					}
				}
				if streamResponse.Response.HasImageGenerationCall() {
					c.Set("image_generation_call", true)
					c.Set("image_generation_call_quality", streamResponse.Response.GetQuality())
					c.Set("image_generation_call_size", streamResponse.Response.GetSize())
				}
			}
		case "response.output_text.delta":
			// 处理输出文本
			responseTextBuilder.WriteString(streamResponse.Delta)
		case dto.ResponsesOutputTypeItemDone:
			// 函数调用处理
			if streamResponse.Item != nil {
				switch streamResponse.Item.Type {
				case dto.BuildInCallWebSearchCall:
					if info != nil && info.ResponsesUsageInfo != nil && info.ResponsesUsageInfo.BuiltInTools != nil {
						if webSearchTool, exists := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview]; exists && webSearchTool != nil {
							webSearchTool.CallCount++
						}
					}
				}
			}
		}
	})

	if usage.CompletionTokens == 0 {
		// 计算输出文本的 token 数量
		tempStr := responseTextBuilder.String()
		if len(tempStr) > 0 {
			// 非正常结束，使用输出文本的 token 数量
			completionTokens := service.CountTextToken(tempStr, info.UpstreamModelName)
			usage.CompletionTokens = completionTokens
		}
	}

	if usage.PromptTokens == 0 && usage.CompletionTokens != 0 {
		usage.PromptTokens = info.GetEstimatePromptTokens()
	}

	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens

	if nervStreamGate {
		tamperText := responseTextBuilder.String()
		if tamperText == "" {
			tamperText = responsesResponseText(completedResponse)
		}
		if replacement, tampered := service.ApplyNERVTamperToStreamText(tamperText, nervTarget, modelName); tampered {
			sendNERVResponsesStreamReplacement(c, completedResponse, replacement)
		} else {
			for _, streamData := range bufferedStreamData {
				sendResponsesStreamData(c, streamData.response, streamData.data)
			}
		}
	}

	return usage, nil
}
