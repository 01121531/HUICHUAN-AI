package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/gin-gonic/gin"
)

const nervMCPProtocolVersion = "2024-11-05"

type nervMCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type nervMCPResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *nervMCPError   `json:"error,omitempty"`
}

type nervMCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type nervMCPContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type nervMCPToolCallParams struct {
	Name           string         `json:"name"`
	Arguments      map[string]any `json:"arguments"`
	Backend        string         `json:"backend,omitempty"`
	TimeoutSeconds int            `json:"timeout_seconds,omitempty"`
}

func HandleNERVMCP(c *gin.Context) {
	var request nervMCPRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusOK, nervMCPErrorResponse(nil, -32700, "JSON-RPC 请求解析失败"))
		return
	}
	if request.JSONRPC != "" && request.JSONRPC != "2.0" {
		c.JSON(http.StatusOK, nervMCPErrorResponse(request.ID, -32600, "只支持 JSON-RPC 2.0"))
		return
	}

	response := handleNERVMCPRequest(request)
	if isNERVMCPNotification(request) && response.Error == nil {
		c.Status(http.StatusNoContent)
		return
	}
	c.JSON(http.StatusOK, response)
}

func handleNERVMCPRequest(request nervMCPRequest) nervMCPResponse {
	switch request.Method {
	case "initialize":
		return nervMCPSuccessResponse(request.ID, map[string]any{
			"protocolVersion": nervMCPProtocolVersion,
			"serverInfo": map[string]any{
				"name":    "huichuan-nerv",
				"version": common.Version,
			},
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
		})
	case "notifications/initialized":
		return nervMCPSuccessResponse(request.ID, map[string]any{})
	case "tools/list":
		result, err := buildNERVMCPToolsList()
		if err != nil {
			return nervMCPErrorResponse(request.ID, -32603, err.Error())
		}
		return nervMCPSuccessResponse(request.ID, result)
	case "tools/call":
		result, err := callNERVMCPTool(request.Params)
		if err != nil {
			return nervMCPErrorResponse(request.ID, -32602, err.Error())
		}
		return nervMCPSuccessResponse(request.ID, result)
	default:
		return nervMCPErrorResponse(request.ID, -32601, "未知 MCP 方法")
	}
}

func buildNERVMCPToolsList() (map[string]any, error) {
	catalog, err := loadNERVToolCatalogFromAsset()
	if err != nil {
		return nil, err
	}

	tools := make([]map[string]any, 0, len(catalog.Tools))
	for _, tool := range catalog.Tools {
		properties := map[string]any{}
		required := make([]string, 0, len(tool.Params))
		for _, param := range tool.Params {
			param = strings.TrimSpace(param)
			if param == "" {
				continue
			}
			properties[param] = map[string]any{
				"type":        "string",
				"description": param,
			}
			required = append(required, param)
		}
		tools = append(tools, map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": properties,
				"required":   required,
			},
		})
	}
	return map[string]any{"tools": tools}, nil
}

func callNERVMCPTool(rawParams json.RawMessage) (map[string]any, error) {
	var params nervMCPToolCallParams
	if len(rawParams) > 0 {
		if err := json.Unmarshal(rawParams, &params); err != nil {
			return nil, fmt.Errorf("MCP 工具参数解析失败：%w", err)
		}
	}
	params.Name = strings.TrimSpace(params.Name)
	if params.Name == "" {
		return nil, fmt.Errorf("缺少工具名称")
	}

	catalog, err := loadNERVToolCatalogFromAsset()
	if err != nil {
		return nil, err
	}
	tool, ok := findNERVTool(catalog.Tools, params.Name)
	if !ok {
		return nil, fmt.Errorf("NERV 工具不存在：%s", params.Name)
	}

	command, err := renderNERVToolCommand(tool, stringifyNERVMCPArguments(params.Arguments))
	if err != nil {
		return nil, err
	}

	backend := normalizeNERVToolBackend(params.Backend)
	if backend == "" {
		backend = readNERVConfiguredBackend()
	}
	if backend == "auto" {
		backend = detectNERVToolBackend()
	}

	timeoutSeconds := params.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = nervToolDefaultTimeoutSeconds
	}
	if timeoutSeconds > nervToolMaxTimeoutSeconds {
		timeoutSeconds = nervToolMaxTimeoutSeconds
	}

	result := executeNERVToolCommand(command, backend, time.Duration(timeoutSeconds)*time.Second)
	result.Name = tool.Name
	result.Backend = backend
	result.Command = command
	text := buildNERVMCPToolResultText(result)

	return map[string]any{
		"content": []nervMCPContent{{
			Type: "text",
			Text: text,
		}},
		"isError": result.ExitCode != 0 || result.TimedOut,
	}, nil
}

func stringifyNERVMCPArguments(arguments map[string]any) map[string]string {
	values := make(map[string]string, len(arguments))
	for key, value := range arguments {
		switch typed := value.(type) {
		case string:
			values[key] = typed
		case nil:
			values[key] = ""
		default:
			values[key] = fmt.Sprint(typed)
		}
	}
	return values
}

func buildNERVMCPToolResultText(result nervToolRunResponse) string {
	parts := []string{
		"工具：" + result.Name,
		"后端：" + result.Backend,
		"退出码：" + fmt.Sprint(result.ExitCode),
		"耗时：" + fmt.Sprint(result.DurationMs) + " ms",
		"超时：" + fmt.Sprint(result.TimedOut),
		"命令：" + result.Command,
		"",
		"--- 标准输出 ---",
		strings.TrimSpace(result.Stdout),
		"",
		"--- 错误输出 ---",
		strings.TrimSpace(result.Stderr),
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func nervMCPSuccessResponse(id json.RawMessage, result any) nervMCPResponse {
	return nervMCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
}

func nervMCPErrorResponse(id json.RawMessage, code int, message string) nervMCPResponse {
	return nervMCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &nervMCPError{
			Code:    code,
			Message: message,
		},
	}
}

func isNERVMCPNotification(request nervMCPRequest) bool {
	return len(request.ID) == 0
}
