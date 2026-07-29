package controller

import (
	"encoding/json"
	"io/fs"
	"os"
	osexec "os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/service"
	"github.com/gin-gonic/gin"
)

const nervBundledAssetRelativePath = "nerv/5.6-JAILBREAK-NERV"

var nervRequiredAssetFiles = []string{
	"bridge.md",
	"proxy_relay.py",
	"direct_setup.py",
	"deploy.py",
	"verify.py",
	"mcp_server.py",
	"tools/tools.json",
	"skills/rei-fallback/SKILL.md",
}

type nervSelfCheckRequiredFile struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Size   int64  `json:"size"`
}

type nervSelfCheckAssets struct {
	BasePath       string                      `json:"base_path"`
	Exists         bool                        `json:"exists"`
	FileCount      int                         `json:"file_count"`
	TotalSizeBytes int64                       `json:"total_size_bytes"`
	RequiredFiles  []nervSelfCheckRequiredFile `json:"required_files"`
	Candidates     []string                    `json:"candidates"`
}

type nervSelfCheckCatalog struct {
	ToolsJSONExists  bool                            `json:"tools_json_exists"`
	ToolsParsed      bool                            `json:"tools_parsed"`
	ToolCount        int                             `json:"tool_count"`
	CategoryCount    int                             `json:"category_count"`
	ToolAvailable    int                             `json:"tool_available"`
	ToolMissing      int                             `json:"tool_missing"`
	ToolUncheckable  int                             `json:"tool_uncheckable"`
	ToolAvailability []nervSelfCheckToolAvailability `json:"tool_availability"`
	SkillCount       int                             `json:"skill_count"`
	SkillDirCount    int                             `json:"skill_dir_count"`
	Error            string                          `json:"error,omitempty"`
}

type nervSelfCheckToolAvailability struct {
	Name      string `json:"name"`
	Category  string `json:"category"`
	Binary    string `json:"binary"`
	Checkable bool   `json:"checkable"`
	Available bool   `json:"available"`
}

type nervSelfCheckConfig struct {
	Enabled          bool   `json:"enabled"`
	ChatEnabled      bool   `json:"chat_enabled"`
	ResponsesEnabled bool   `json:"responses_enabled"`
	TamperEnabled    bool   `json:"tamper_enabled"`
	Mode             string `json:"mode"`
	Models           string `json:"models"`
	Targets          string `json:"targets"`
	PromptConfigured bool   `json:"prompt_configured"`
	PromptLength     int    `json:"prompt_length"`
	TamperRuleLines  int    `json:"tamper_rule_lines"`
	MCPBackend       string `json:"mcp_backend"`
	WSLDistro        string `json:"wsl_distro"`
	DockerContainer  string `json:"docker_container"`
	SSHHost          string `json:"ssh_host"`
}

type nervSelfCheckRecentEvent struct {
	TS     int64  `json:"ts"`
	Event  string `json:"event"`
	Target string `json:"target"`
	Model  string `json:"model"`
}

type nervSelfCheckStats struct {
	Total           int64                      `json:"total"`
	Inject          int64                      `json:"inject"`
	Tamper          int64                      `json:"tamper"`
	ChatInject      int64                      `json:"chat_inject"`
	ResponsesInject int64                      `json:"responses_inject"`
	ChatTamper      int64                      `json:"chat_tamper"`
	ResponsesTamper int64                      `json:"responses_tamper"`
	LastEventAt     int64                      `json:"last_event_at"`
	LastEvent       string                     `json:"last_event"`
	LastTarget      string                     `json:"last_target"`
	LastModel       string                     `json:"last_model"`
	Recent          []nervSelfCheckRecentEvent `json:"recent"`
	RecentValid     bool                       `json:"recent_valid"`
}

type nervSelfCheckItem struct {
	Key     string `json:"key"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

type nervSelfCheckResponse struct {
	Assets        nervSelfCheckAssets  `json:"assets"`
	Catalog       nervSelfCheckCatalog `json:"catalog"`
	Config        nervSelfCheckConfig  `json:"config"`
	Stats         nervSelfCheckStats   `json:"stats"`
	Checks        []nervSelfCheckItem  `json:"checks"`
	WorkingDir    string               `json:"working_dir"`
	ExecutableDir string               `json:"executable_dir"`
}

func GetNERVSelfCheck(c *gin.Context) {
	status := buildNERVSelfCheck()
	common.ApiSuccess(c, status)
}

func buildNERVSelfCheck() nervSelfCheckResponse {
	basePath, exists, candidates := findNERVAssetBasePath()
	assets := buildNERVAssetStatus(basePath, exists, candidates)
	catalog := buildNERVCatalogStatus(basePath, exists)
	config := buildNERVConfigStatus()
	stats := buildNERVStatsStatus()

	workingDir, _ := os.Getwd()
	executableDir := ""
	if executablePath, err := os.Executable(); err == nil {
		executableDir = filepath.Dir(executablePath)
	}

	status := nervSelfCheckResponse{
		Assets:        assets,
		Catalog:       catalog,
		Config:        config,
		Stats:         stats,
		WorkingDir:    workingDir,
		ExecutableDir: executableDir,
	}
	status.Checks = buildNERVChecks(status)
	return status
}

func findNERVAssetBasePath() (string, bool, []string) {
	candidates := make([]string, 0, 8)
	seen := map[string]bool{}
	addCandidate := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		path = filepath.Clean(path)
		if seen[path] {
			return
		}
		seen[path] = true
		candidates = append(candidates, path)
	}

	addCandidate(os.Getenv("NERV_ASSET_PATH"))

	if workingDir, err := os.Getwd(); err == nil {
		addCandidate(filepath.Join(workingDir, nervBundledAssetRelativePath))
		addCandidate(filepath.Join(workingDir, "build", "HUICHUAN-AI", nervBundledAssetRelativePath))
		addCandidate(filepath.Join(workingDir, ".deploy", "HUICHUAN-AI", nervBundledAssetRelativePath))
	}

	if executablePath, err := os.Executable(); err == nil {
		executableDir := filepath.Dir(executablePath)
		addCandidate(filepath.Join(executableDir, nervBundledAssetRelativePath))
		addCandidate(filepath.Join(executableDir, "build", "HUICHUAN-AI", nervBundledAssetRelativePath))
		addCandidate(filepath.Join(filepath.Dir(executableDir), nervBundledAssetRelativePath))
	}

	for _, candidate := range candidates {
		if stat, err := os.Stat(candidate); err == nil && stat.IsDir() {
			return candidate, true, candidates
		}
	}
	if len(candidates) == 0 {
		return "", false, candidates
	}
	return candidates[0], false, candidates
}

func buildNERVAssetStatus(basePath string, exists bool, candidates []string) nervSelfCheckAssets {
	requiredFiles := make([]nervSelfCheckRequiredFile, 0, len(nervRequiredAssetFiles))
	for _, requiredFile := range nervRequiredAssetFiles {
		fileStatus := nervSelfCheckRequiredFile{Path: requiredFile}
		if exists {
			fullPath := filepath.Join(basePath, filepath.FromSlash(requiredFile))
			if stat, err := os.Stat(fullPath); err == nil && !stat.IsDir() {
				fileStatus.Exists = true
				fileStatus.Size = stat.Size()
			}
		}
		requiredFiles = append(requiredFiles, fileStatus)
	}

	fileCount, totalSize := countNERVAssetFiles(basePath, exists)
	return nervSelfCheckAssets{
		BasePath:       basePath,
		Exists:         exists,
		FileCount:      fileCount,
		TotalSizeBytes: totalSize,
		RequiredFiles:  requiredFiles,
		Candidates:     candidates,
	}
}

func countNERVAssetFiles(basePath string, exists bool) (int, int64) {
	if !exists {
		return 0, 0
	}

	var fileCount int
	var totalSize int64
	_ = filepath.WalkDir(basePath, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry == nil || entry.IsDir() {
			return nil
		}
		fileCount++
		if info, statErr := entry.Info(); statErr == nil {
			totalSize += info.Size()
		}
		return nil
	})
	return fileCount, totalSize
}

func buildNERVCatalogStatus(basePath string, assetsExist bool) nervSelfCheckCatalog {
	status := nervSelfCheckCatalog{
		ToolAvailability: []nervSelfCheckToolAvailability{},
	}
	if !assetsExist {
		status.Error = "NERV 内置资产目录不存在"
		return status
	}

	toolsPath := filepath.Join(basePath, "tools", "tools.json")
	if stat, err := os.Stat(toolsPath); err == nil && !stat.IsDir() {
		status.ToolsJSONExists = true
	}
	if status.ToolsJSONExists {
		toolCount, categoryCount, availability, err := parseNERVToolsJSON(toolsPath)
		if err != nil {
			status.Error = err.Error()
		} else {
			status.ToolsParsed = true
			status.ToolCount = toolCount
			status.CategoryCount = categoryCount
			status.ToolAvailability = availability
			for _, item := range availability {
				if item.Available {
					status.ToolAvailable++
				} else if item.Checkable {
					status.ToolMissing++
				} else {
					status.ToolUncheckable++
				}
			}
		}
	}

	status.SkillDirCount, status.SkillCount = countNERVSkills(filepath.Join(basePath, "skills"))
	return status
}

func parseNERVToolsJSON(path string) (int, int, []nervSelfCheckToolAvailability, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, nil, err
	}

	var payload struct {
		Tools []struct {
			Name     string `json:"name"`
			Category string `json:"category"`
			Command  string `json:"cmd"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return 0, 0, nil, err
	}

	categories := map[string]bool{}
	availability := make([]nervSelfCheckToolAvailability, 0, len(payload.Tools))
	for _, tool := range payload.Tools {
		category := strings.TrimSpace(tool.Category)
		if category != "" {
			categories[category] = true
		}
		binary := extractNERVToolBinary(tool.Command)
		checkable := binary != "" && !strings.Contains(binary, "{")
		availability = append(availability, nervSelfCheckToolAvailability{
			Name:      strings.TrimSpace(tool.Name),
			Category:  category,
			Binary:    binary,
			Checkable: checkable,
			Available: checkable && nervBinaryAvailable(binary),
		})
	}
	sort.SliceStable(availability, func(i, j int) bool {
		if availability[i].Category == availability[j].Category {
			return availability[i].Name < availability[j].Name
		}
		return availability[i].Category < availability[j].Category
	})
	return len(payload.Tools), len(categories), availability, nil
}

func extractNERVToolBinary(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	binary := strings.Trim(fields[0], `"'`)
	binary = filepath.Base(binary)
	return binary
}

func nervBinaryAvailable(binary string) bool {
	if _, err := osexec.LookPath(binary); err == nil {
		return true
	}
	if filepath.Ext(binary) == "" {
		if _, err := osexec.LookPath(binary + ".exe"); err == nil {
			return true
		}
	}
	return false
}

func countNERVSkills(skillsPath string) (int, int) {
	entries, err := os.ReadDir(skillsPath)
	if err != nil {
		return 0, 0
	}

	var dirCount int
	var skillCount int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirCount++
		if stat, err := os.Stat(filepath.Join(skillsPath, entry.Name(), "SKILL.md")); err == nil && !stat.IsDir() {
			skillCount++
		}
	}
	return dirCount, skillCount
}

func buildNERVConfigStatus() nervSelfCheckConfig {
	options := service.LoadNERVBridgeOptions()

	common.OptionMapRWMutex.RLock()
	mcpBackend := common.OptionMap["nerv_setting.mcp_backend"]
	wslDistro := common.OptionMap["nerv_setting.wsl_distro"]
	dockerContainer := common.OptionMap["nerv_setting.docker_container"]
	sshHost := common.OptionMap["nerv_setting.ssh_host"]
	common.OptionMapRWMutex.RUnlock()

	return nervSelfCheckConfig{
		Enabled:          options.Enabled,
		ChatEnabled:      options.ChatEnabled,
		ResponsesEnabled: options.ResponsesEnabled,
		TamperEnabled:    options.TamperEnabled,
		Mode:             options.Mode,
		Models:           options.Models,
		Targets:          options.Targets,
		PromptConfigured: strings.TrimSpace(options.Prompt) != "",
		PromptLength:     len([]rune(options.Prompt)),
		TamperRuleLines:  countNonEmptyLines(options.TamperPatterns),
		MCPBackend:       mcpBackend,
		WSLDistro:        wslDistro,
		DockerContainer:  dockerContainer,
		SSHHost:          sshHost,
	}
}

func buildNERVStatsStatus() nervSelfCheckStats {
	common.OptionMapRWMutex.RLock()
	rawRecent := common.OptionMap[service.NERVStatsRecentKey]
	stats := nervSelfCheckStats{
		Total:           parseNERVInt64Locked(service.NERVStatsTotalKey),
		Inject:          parseNERVInt64Locked(service.NERVStatsInjectKey),
		Tamper:          parseNERVInt64Locked(service.NERVStatsTamperKey),
		ChatInject:      parseNERVInt64Locked(service.NERVStatsChatInjectKey),
		ResponsesInject: parseNERVInt64Locked(service.NERVStatsResponsesInjectKey),
		ChatTamper:      parseNERVInt64Locked(service.NERVStatsChatTamperKey),
		ResponsesTamper: parseNERVInt64Locked(service.NERVStatsResponsesTamperKey),
		LastEventAt:     parseNERVInt64Locked(service.NERVStatsLastEventAtKey),
		LastEvent:       common.OptionMap[service.NERVStatsLastEventKey],
		LastTarget:      common.OptionMap[service.NERVStatsLastTargetKey],
		LastModel:       common.OptionMap[service.NERVStatsLastModelKey],
	}
	common.OptionMapRWMutex.RUnlock()

	if strings.TrimSpace(rawRecent) == "" {
		stats.Recent = []nervSelfCheckRecentEvent{}
		stats.RecentValid = true
		return stats
	}
	if err := json.Unmarshal([]byte(rawRecent), &stats.Recent); err == nil {
		stats.RecentValid = true
	} else {
		stats.Recent = []nervSelfCheckRecentEvent{}
	}
	return stats
}

func parseNERVInt64Locked(key string) int64 {
	value := strings.TrimSpace(common.OptionMap[key])
	if value == "" {
		return 0
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func countNonEmptyLines(value string) int {
	count := 0
	for _, line := range strings.Split(value, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func buildNERVChecks(status nervSelfCheckResponse) []nervSelfCheckItem {
	requiredFilesOK := true
	for _, requiredFile := range status.Assets.RequiredFiles {
		if !requiredFile.Exists {
			requiredFilesOK = false
			break
		}
	}

	checks := []nervSelfCheckItem{
		{
			Key:     "assets",
			OK:      status.Assets.Exists,
			Message: boolMessage(status.Assets.Exists, "NERV 内置资产目录已找到", "NERV 内置资产目录未找到"),
		},
		{
			Key:     "required_files",
			OK:      requiredFilesOK,
			Message: boolMessage(requiredFilesOK, "核心脚本和配置文件完整", "存在缺失的核心脚本或配置文件"),
		},
		{
			Key:     "tools_catalog",
			OK:      status.Catalog.ToolsParsed && status.Catalog.ToolCount > 0,
			Message: boolMessage(status.Catalog.ToolsParsed && status.Catalog.ToolCount > 0, "工具目录读取正常", "工具目录未读取成功"),
		},
		{
			Key:     "skills_catalog",
			OK:      status.Catalog.SkillCount > 0,
			Message: boolMessage(status.Catalog.SkillCount > 0, "技能目录读取正常", "技能目录未读取成功"),
		},
		{
			Key:     "prompt",
			OK:      status.Config.PromptConfigured,
			Message: boolMessage(status.Config.PromptConfigured, "桥接提示词已配置", "桥接提示词为空"),
		},
		{
			Key:     "models",
			OK:      strings.TrimSpace(status.Config.Models) != "",
			Message: boolMessage(strings.TrimSpace(status.Config.Models) != "", "模型匹配规则已配置", "模型匹配规则为空"),
		},
		{
			Key:     "targets",
			OK:      strings.TrimSpace(status.Config.Targets) != "",
			Message: boolMessage(strings.TrimSpace(status.Config.Targets) != "", "注入范围已配置", "注入范围为空"),
		},
		{
			Key:     "recent_stats",
			OK:      status.Stats.RecentValid,
			Message: boolMessage(status.Stats.RecentValid, "最近事件缓存格式正常", "最近事件缓存格式异常"),
		},
	}
	sort.SliceStable(checks, func(i, j int) bool {
		return checks[i].Key < checks[j].Key
	})
	return checks
}

func boolMessage(ok bool, successMessage string, failMessage string) string {
	if ok {
		return successMessage
	}
	return failMessage
}
