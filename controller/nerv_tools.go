package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/gin-gonic/gin"
)

const (
	nervToolDefaultTimeoutSeconds = 30
	nervToolMaxTimeoutSeconds     = 120
	nervToolMaxParamLength        = 2000
	nervToolMaxOutputLength       = 20000
)

type nervToolCatalogItem struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Command     string   `json:"command"`
	Params      []string `json:"params"`
	Binary      string   `json:"binary"`
	Checkable   bool     `json:"checkable"`
	Available   bool     `json:"available"`
}

type nervToolCatalogResponse struct {
	Tools         []nervToolCatalogItem `json:"tools"`
	Count         int                   `json:"count"`
	CategoryCount int                   `json:"category_count"`
	BasePath      string                `json:"base_path"`
}

type nervToolRunRequest struct {
	Name           string            `json:"name"`
	Args           map[string]string `json:"args"`
	Backend        string            `json:"backend"`
	TimeoutSeconds int               `json:"timeout_seconds"`
}

type nervToolRunResponse struct {
	Name        string `json:"name"`
	Backend     string `json:"backend"`
	Command     string `json:"command"`
	ExitCode    int    `json:"exit_code"`
	Stdout      string `json:"stdout"`
	Stderr      string `json:"stderr"`
	TimedOut    bool   `json:"timed_out"`
	DurationMs  int64  `json:"duration_ms"`
	OutputBytes int    `json:"output_bytes"`
}

func GetNERVTools(c *gin.Context) {
	catalog, err := loadNERVToolCatalogFromAsset()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, catalog)
}

func RunNERVTool(c *gin.Context) {
	var request nervToolRunRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}

	catalog, err := loadNERVToolCatalogFromAsset()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	tool, ok := findNERVTool(catalog.Tools, request.Name)
	if !ok {
		common.ApiErrorMsg(c, "NERV 工具不存在")
		return
	}

	command, err := renderNERVToolCommand(tool, request.Args)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}

	backend := normalizeNERVToolBackend(request.Backend)
	if backend == "" {
		backend = readNERVConfiguredBackend()
	}
	if backend == "auto" {
		backend = detectNERVToolBackend()
	}

	timeoutSeconds := request.TimeoutSeconds
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
	common.ApiSuccess(c, result)
}

func loadNERVToolCatalogFromAsset() (nervToolCatalogResponse, error) {
	basePath, exists, _ := findNERVAssetBasePath()
	if !exists {
		return nervToolCatalogResponse{}, os.ErrNotExist
	}
	tools, err := loadNERVToolCatalog(filepath.Join(basePath, "tools", "tools.json"))
	if err != nil {
		return nervToolCatalogResponse{}, err
	}
	categories := map[string]bool{}
	for _, tool := range tools {
		if tool.Category != "" {
			categories[tool.Category] = true
		}
	}
	return nervToolCatalogResponse{
		Tools:         tools,
		Count:         len(tools),
		CategoryCount: len(categories),
		BasePath:      basePath,
	}, nil
}

func loadNERVToolCatalog(path string) ([]nervToolCatalogItem, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var payload struct {
		Tools []struct {
			Name        string   `json:"name"`
			Description string   `json:"desc"`
			Command     string   `json:"cmd"`
			Params      []string `json:"params"`
			Category    string   `json:"category"`
		} `json:"tools"`
	}
	if err := common.Unmarshal(data, &payload); err != nil {
		return nil, err
	}

	tools := make([]nervToolCatalogItem, 0, len(payload.Tools))
	for _, source := range payload.Tools {
		binary := extractNERVToolBinary(source.Command)
		checkable := binary != "" && !strings.Contains(binary, "{")
		tools = append(tools, nervToolCatalogItem{
			Name:        strings.TrimSpace(source.Name),
			Description: strings.TrimSpace(source.Description),
			Category:    strings.TrimSpace(source.Category),
			Command:     strings.TrimSpace(source.Command),
			Params:      source.Params,
			Binary:      binary,
			Checkable:   checkable,
			Available:   checkable && nervBinaryAvailable(binary),
		})
	}
	sort.SliceStable(tools, func(i, j int) bool {
		if tools[i].Category == tools[j].Category {
			return tools[i].Name < tools[j].Name
		}
		return tools[i].Category < tools[j].Category
	})
	return tools, nil
}

func findNERVTool(tools []nervToolCatalogItem, name string) (nervToolCatalogItem, bool) {
	name = strings.TrimSpace(name)
	for _, tool := range tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return nervToolCatalogItem{}, false
}

func renderNERVToolCommand(tool nervToolCatalogItem, args map[string]string) (string, error) {
	command := tool.Command
	if strings.TrimSpace(command) == "" {
		return "", errors.New("NERV 工具命令为空")
	}
	if args == nil {
		args = map[string]string{}
	}
	for _, param := range tool.Params {
		param = strings.TrimSpace(param)
		if param == "" {
			continue
		}
		value := strings.TrimSpace(args[param])
		if len([]rune(value)) > nervToolMaxParamLength {
			return "", fmt.Errorf("参数过长：%s", param)
		}
		if strings.ContainsRune(value, '\x00') {
			return "", fmt.Errorf("参数包含非法字符：%s", param)
		}
		command = strings.ReplaceAll(command, "{"+param+"}", escapeNERVToolArgument(value))
	}

	if missing := findNERVCommandPlaceholder(command); missing != "" {
		return "", fmt.Errorf("缺少参数：%s", missing)
	}
	return command, nil
}

func findNERVCommandPlaceholder(command string) string {
	matches := regexp.MustCompile(`\{([A-Za-z0-9_]+)\}`).FindStringSubmatch(command)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}

func readNERVConfiguredBackend() string {
	common.OptionMapRWMutex.RLock()
	backend := common.OptionMap["nerv_setting.mcp_backend"]
	common.OptionMapRWMutex.RUnlock()
	backend = normalizeNERVToolBackend(backend)
	if backend == "" {
		return "local"
	}
	return backend
}

func normalizeNERVToolBackend(backend string) string {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "auto", "local", "wsl", "docker", "ssh":
		return strings.ToLower(strings.TrimSpace(backend))
	default:
		return ""
	}
}

func detectNERVToolBackend() string {
	distro := readNERVOptionString("nerv_setting.wsl_distro", "kali-linux")
	if commandOutputContains(5*time.Second, "OK", "wsl", "-d", distro, "echo", "OK") {
		return "wsl"
	}

	container := readNERVOptionString("nerv_setting.docker_container", "kali-tools")
	if commandOutputContains(5*time.Second, container, "docker", "ps", "--filter", "name="+container, "--format", "{{.Names}}") {
		return "docker"
	}

	return "local"
}

func commandOutputContains(timeout time.Duration, expected string, name string, args ...string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	output, err := osexec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(strings.TrimSpace(string(output)), expected)
}

func escapeNERVToolArgument(value string) string {
	return strings.ReplaceAll(value, "'", `'"'"'`)
}

func executeNERVToolCommand(command string, backend string, timeout time.Duration) nervToolRunResponse {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := buildNERVToolExecCommand(ctx, command, backend)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := nervToolRunResponse{
		ExitCode:   0,
		TimedOut:   ctx.Err() == context.DeadlineExceeded,
		DurationMs: time.Since(start).Milliseconds(),
	}
	if err != nil {
		result.ExitCode = 1
		var exitErr *osexec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
		}
		if stderr.Len() == 0 {
			stderr.WriteString(err.Error())
		}
	}

	result.Stdout = limitNERVToolOutput(stdout.String())
	result.Stderr = limitNERVToolOutput(stderr.String())
	result.OutputBytes = len(stdout.String()) + len(stderr.String())
	return result
}

func buildNERVToolExecCommand(ctx context.Context, command string, backend string) *osexec.Cmd {
	switch backend {
	case "wsl":
		distro := readNERVOptionString("nerv_setting.wsl_distro", "kali-linux")
		return osexec.CommandContext(ctx, "wsl", "-d", distro, "bash", "-lc", command)
	case "docker":
		container := readNERVOptionString("nerv_setting.docker_container", "kali-tools")
		return osexec.CommandContext(ctx, "docker", "exec", container, "bash", "-lc", command)
	case "ssh":
		host := readNERVOptionString("nerv_setting.ssh_host", "")
		return osexec.CommandContext(ctx, "ssh", "-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=5", host, command)
	default:
		if runtime.GOOS == "windows" {
			return osexec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", command)
		}
		return osexec.CommandContext(ctx, "bash", "-lc", command)
	}
}

func readNERVOptionString(key string, fallback string) string {
	common.OptionMapRWMutex.RLock()
	value := strings.TrimSpace(common.OptionMap[key])
	common.OptionMapRWMutex.RUnlock()
	if value == "" {
		return fallback
	}
	return value
}

func limitNERVToolOutput(value string) string {
	if len([]rune(value)) <= nervToolMaxOutputLength {
		return value
	}
	runes := []rune(value)
	return string(runes[:nervToolMaxOutputLength]) + "\n...[输出过长，已截断]"
}
