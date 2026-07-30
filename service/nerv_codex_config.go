package service

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

const (
	NERVCodexConfigBackupSuffix = ".lab-bak"
	NERVCodexMCPBackupSuffix    = ".mcp-bak"
	nervCodexBridgeFileName     = "bridge.md"
	nervCodexSkillsDirName      = "skills"
	nervCodexMCPServerName      = "nerv_break"
	nervCodexMCPServerScript    = "mcp_server.py"
	nervCodexMCPManagedComment  = "# Managed by HUICHUAN-AI NERV MCP"
)

type NERVCodexConfigStatus struct {
	Home                 string   `json:"home"`
	ConfigPath           string   `json:"config_path"`
	Found                bool     `json:"found"`
	ConfigExists         bool     `json:"config_exists"`
	BackupExists         bool     `json:"backup_exists"`
	BridgeActive         bool     `json:"bridge_active"`
	BridgeExists         bool     `json:"bridge_exists"`
	SkillsExists         bool     `json:"skills_exists"`
	SkillCount           int      `json:"skill_count"`
	AssetPath            string   `json:"asset_path"`
	AssetExists          bool     `json:"asset_exists"`
	AssetBridgeExists    bool     `json:"asset_bridge_exists"`
	AssetSkillsExists    bool     `json:"asset_skills_exists"`
	AssetMCPServerExists bool     `json:"asset_mcp_server_exists"`
	MCPServerScriptPath  string   `json:"mcp_server_script_path"`
	MCPActive            bool     `json:"mcp_active"`
	MCPBackupExists      bool     `json:"mcp_backup_exists"`
	MCPConfigRaw         string   `json:"mcp_config_raw,omitempty"`
	Candidates           []string `json:"candidates"`
	Message              string   `json:"message"`
	ModelInstructionsRaw string   `json:"model_instructions_raw,omitempty"`
}

type NERVCodexConfigResult struct {
	Action     string                `json:"action"`
	Changed    bool                  `json:"changed"`
	BackupPath string                `json:"backup_path,omitempty"`
	Messages   []string              `json:"messages"`
	Status     NERVCodexConfigStatus `json:"status"`
}

type NERVMCPConfigOptions struct {
	Backend         string `json:"backend"`
	WSLDistro       string `json:"wsl_distro"`
	DockerContainer string `json:"docker_container"`
	SSHHost         string `json:"ssh_host"`
}

func NERVCodexConfigStatusFor(codexHome string, assetPath string) NERVCodexConfigStatus {
	home, found, candidates := ResolveNERVCodexHome(codexHome)
	status := NERVCodexConfigStatus{
		Home:       home,
		Found:      found,
		AssetPath:  assetPath,
		Candidates: candidates,
	}
	if home != "" {
		status.ConfigPath = filepath.Join(home, "config.toml")
		status.ConfigExists = fileExists(status.ConfigPath)
		status.BackupExists = fileExists(status.ConfigPath+NERVCodexConfigBackupSuffix) || fileExists(replaceConfigSuffix(status.ConfigPath, ".toml.zxwn-bak"))
		status.MCPBackupExists = fileExists(status.ConfigPath + NERVCodexMCPBackupSuffix)
		status.BridgeExists = fileExists(filepath.Join(home, nervCodexBridgeFileName))
		status.SkillsExists = dirExists(filepath.Join(home, nervCodexSkillsDirName))
		status.SkillCount = CountNERVSkillFiles(filepath.Join(home, nervCodexSkillsDirName))
		if status.ConfigExists {
			text, _ := os.ReadFile(status.ConfigPath)
			active, raw := parseNERVModelInstructionsLine(string(text))
			status.BridgeActive = active
			status.ModelInstructionsRaw = raw
			mcpActive, mcpRaw := parseNERVMCPServerBlock(string(text))
			status.MCPActive = mcpActive
			status.MCPConfigRaw = mcpRaw
		}
	}
	status.AssetExists = dirExists(assetPath)
	status.AssetBridgeExists = fileExists(filepath.Join(assetPath, nervCodexBridgeFileName))
	status.AssetSkillsExists = dirExists(filepath.Join(assetPath, nervCodexSkillsDirName))
	status.MCPServerScriptPath = filepath.Join(assetPath, nervCodexMCPServerScript)
	status.AssetMCPServerExists = fileExists(status.MCPServerScriptPath)
	status.Message = buildNERVCodexConfigMessage(status)
	return status
}

func ResolveNERVCodexHome(requested string) (string, bool, []string) {
	candidates := make([]string, 0, 4)
	addCandidate := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if expanded, err := expandNERVPath(value); err == nil {
			value = expanded
		}
		if abs, err := filepath.Abs(value); err == nil {
			value = abs
		}
		for _, existing := range candidates {
			if existing == value {
				return
			}
		}
		candidates = append(candidates, value)
	}

	addCandidate(requested)
	addCandidate(os.Getenv("CODEX_HOME"))
	if homeDir, err := os.UserHomeDir(); err == nil {
		addCandidate(filepath.Join(homeDir, ".codex"))
		addCandidate(filepath.Join(homeDir, "codex"))
	}

	for _, candidate := range candidates {
		if fileExists(filepath.Join(candidate, "config.toml")) {
			return candidate, true, candidates
		}
	}
	if len(candidates) > 0 {
		return candidates[0], false, candidates
	}
	return "", false, candidates
}

func ApplyNERVCodexConfig(codexHome string, assetPath string) (NERVCodexConfigResult, error) {
	status := NERVCodexConfigStatusFor(codexHome, assetPath)
	result := NERVCodexConfigResult{Action: "apply", Status: status}
	if !status.ConfigExists {
		return result, errors.New("Codex config.toml 未找到，请先填写正确的 Codex Home")
	}
	if !status.AssetBridgeExists {
		return result, errors.New("NERV bridge.md 内置资产未找到")
	}

	backupPath := status.ConfigPath + NERVCodexConfigBackupSuffix
	result.BackupPath = backupPath
	if !fileExists(backupPath) {
		if err := copyFile(status.ConfigPath, backupPath); err != nil {
			return result, fmt.Errorf("备份 config.toml 失败：%w", err)
		}
		result.Messages = append(result.Messages, "已备份 config.toml")
	}

	originalConfig, err := os.ReadFile(status.ConfigPath)
	if err != nil {
		return result, err
	}
	updatedConfig := rewriteNERVModelInstructions(string(originalConfig))
	if err := os.WriteFile(status.ConfigPath, []byte(updatedConfig), 0o600); err != nil {
		return result, err
	}
	if err := validateNERVToml(status.ConfigPath); err != nil {
		_ = os.WriteFile(status.ConfigPath, originalConfig, 0o600)
		return result, fmt.Errorf("写入后 TOML 校验失败，已回滚：%w", err)
	}
	result.Messages = append(result.Messages, "已写入 model_instructions_file")

	if err := copyFile(filepath.Join(assetPath, nervCodexBridgeFileName), filepath.Join(status.Home, nervCodexBridgeFileName)); err != nil {
		return result, fmt.Errorf("复制 bridge.md 失败：%w", err)
	}
	result.Messages = append(result.Messages, "已复制 bridge.md")

	if status.AssetSkillsExists {
		dstSkills := filepath.Join(status.Home, nervCodexSkillsDirName)
		if err := os.RemoveAll(dstSkills); err != nil {
			return result, fmt.Errorf("清理旧 skills 失败：%w", err)
		}
		if err := copyDir(filepath.Join(assetPath, nervCodexSkillsDirName), dstSkills); err != nil {
			return result, fmt.Errorf("复制 skills 失败：%w", err)
		}
		result.Messages = append(result.Messages, "已复制 skills 目录")
	}

	result.Changed = true
	result.Status = NERVCodexConfigStatusFor(status.Home, assetPath)
	return result, nil
}

func RemoveNERVCodexConfig(codexHome string, assetPath string) (NERVCodexConfigResult, error) {
	status := NERVCodexConfigStatusFor(codexHome, assetPath)
	result := NERVCodexConfigResult{Action: "remove", Status: status}
	if !status.ConfigExists {
		return result, errors.New("Codex config.toml 未找到，请先填写正确的 Codex Home")
	}

	backupPath := status.ConfigPath + NERVCodexConfigBackupSuffix
	if !fileExists(backupPath) {
		backupPath = replaceConfigSuffix(status.ConfigPath, ".toml.zxwn-bak")
	}
	if fileExists(backupPath) {
		if err := copyFile(backupPath, status.ConfigPath); err != nil {
			return result, fmt.Errorf("还原 config.toml 失败：%w", err)
		}
		_ = os.Remove(backupPath)
		result.BackupPath = backupPath
		result.Messages = append(result.Messages, "已从备份还原 config.toml")
	} else {
		config, err := os.ReadFile(status.ConfigPath)
		if err != nil {
			return result, err
		}
		if err := os.WriteFile(status.ConfigPath, []byte(removeNERVModelInstructions(string(config))), 0o600); err != nil {
			return result, err
		}
		if err := validateNERVToml(status.ConfigPath); err != nil {
			return result, fmt.Errorf("移除后 TOML 校验失败：%w", err)
		}
		result.Messages = append(result.Messages, "未找到备份，已移除 model_instructions_file")
	}

	for _, path := range []string{
		filepath.Join(status.Home, nervCodexBridgeFileName),
		filepath.Join(status.Home, nervCodexSkillsDirName),
	} {
		if err := os.RemoveAll(path); err != nil {
			return result, err
		}
	}
	result.Messages = append(result.Messages, "已移除 bridge.md 和 skills 目录")
	result.Changed = true
	result.Status = NERVCodexConfigStatusFor(status.Home, assetPath)
	return result, nil
}

func ApplyNERVMCPConfig(codexHome string, assetPath string, options NERVMCPConfigOptions) (NERVCodexConfigResult, error) {
	status := NERVCodexConfigStatusFor(codexHome, assetPath)
	result := NERVCodexConfigResult{Action: "apply_mcp", Status: status}
	if !status.ConfigExists {
		return result, errors.New("Codex config.toml 未找到，请先填写正确的 Codex Home")
	}
	if !status.AssetMCPServerExists {
		return result, errors.New("NERV mcp_server.py 内置资产未找到")
	}

	backupPath := status.ConfigPath + NERVCodexMCPBackupSuffix
	result.BackupPath = backupPath
	if !fileExists(backupPath) {
		if err := copyFile(status.ConfigPath, backupPath); err != nil {
			return result, fmt.Errorf("备份 MCP 配置前的 config.toml 失败：%w", err)
		}
		result.Messages = append(result.Messages, "已备份 MCP 配置前的 config.toml")
	}

	originalConfig, err := os.ReadFile(status.ConfigPath)
	if err != nil {
		return result, err
	}
	updatedConfig := rewriteNERVMCPServerBlock(string(originalConfig), assetPath, options)
	if err := os.WriteFile(status.ConfigPath, []byte(updatedConfig), 0o600); err != nil {
		return result, err
	}
	if err := validateNERVToml(status.ConfigPath); err != nil {
		_ = os.WriteFile(status.ConfigPath, originalConfig, 0o600)
		return result, fmt.Errorf("写入 MCP 后 TOML 校验失败，已回滚：%w", err)
	}

	result.Messages = append(result.Messages, "已写入 MCP 工具服务器 nerv_break")
	result.Changed = true
	result.Status = NERVCodexConfigStatusFor(status.Home, assetPath)
	return result, nil
}

func RemoveNERVMCPConfig(codexHome string, assetPath string) (NERVCodexConfigResult, error) {
	status := NERVCodexConfigStatusFor(codexHome, assetPath)
	result := NERVCodexConfigResult{Action: "remove_mcp", Status: status}
	if !status.ConfigExists {
		return result, errors.New("Codex config.toml 未找到，请先填写正确的 Codex Home")
	}

	originalConfig, err := os.ReadFile(status.ConfigPath)
	if err != nil {
		return result, err
	}
	updatedConfig := removeNERVMCPServerBlock(string(originalConfig))
	if updatedConfig != string(originalConfig) {
		if err := os.WriteFile(status.ConfigPath, []byte(updatedConfig), 0o600); err != nil {
			return result, err
		}
		if err := validateNERVToml(status.ConfigPath); err != nil {
			_ = os.WriteFile(status.ConfigPath, originalConfig, 0o600)
			return result, fmt.Errorf("移除 MCP 后 TOML 校验失败，已回滚：%w", err)
		}
		result.Changed = true
		result.Messages = append(result.Messages, "已移除 MCP 工具服务器 nerv_break")
	} else {
		result.Messages = append(result.Messages, "未找到 MCP 工具服务器 nerv_break")
	}
	result.Status = NERVCodexConfigStatusFor(status.Home, assetPath)
	return result, nil
}

func CountNERVSkillFiles(skillsPath string) int {
	count := 0
	_ = filepath.WalkDir(skillsPath, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		if d.Name() == "SKILL.md" {
			count++
		}
		return nil
	})
	return count
}

func rewriteNERVModelInstructions(config string) string {
	lines := strings.SplitAfter(config, "\n")
	if len(lines) == 0 {
		return `model_instructions_file = "./bridge.md"` + "\n"
	}
	hasActive := false
	for _, line := range lines {
		if isActiveNERVModelInstructionsLine(line) {
			hasActive = true
			break
		}
	}
	replaced := false
	inserted := false
	out := make([]string, 0, len(lines)+1)
	for _, line := range lines {
		if isActiveNERVModelInstructionsLine(line) {
			out = append(out, `model_instructions_file = "./bridge.md"`+"\n")
			replaced = true
			continue
		}
		out = append(out, line)
		if !hasActive && !inserted && strings.HasPrefix(strings.TrimSpace(line), "model =") && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			out = append(out, `model_instructions_file = "./bridge.md"`+"\n")
			inserted = true
		}
	}
	if !replaced && !inserted {
		if len(out) > 0 && out[len(out)-1] != "" && !strings.HasSuffix(out[len(out)-1], "\n") {
			out[len(out)-1] += "\n"
		}
		out = append(out, `model_instructions_file = "./bridge.md"`+"\n")
	}
	return strings.Join(out, "")
}

func removeNERVModelInstructions(config string) string {
	lines := strings.SplitAfter(config, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if isActiveNERVModelInstructionsLine(line) {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "")
}

func parseNERVModelInstructionsLine(config string) (bool, string) {
	for _, line := range strings.Split(config, "\n") {
		if isActiveNERVModelInstructionsLine(line) {
			return true, strings.TrimSpace(line)
		}
	}
	return false, ""
}

func rewriteNERVMCPServerBlock(config string, assetPath string, options NERVMCPConfigOptions) string {
	cleaned := strings.TrimRight(removeNERVMCPServerBlock(config), "\n")
	if cleaned != "" {
		cleaned += "\n\n"
	}
	return cleaned + buildNERVMCPServerBlock(assetPath, options)
}

func removeNERVMCPServerBlock(config string) string {
	lines := strings.SplitAfter(config, "\n")
	out := make([]string, 0, len(lines))
	skipping := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == nervCodexMCPManagedComment {
			continue
		}
		if isNERVMCPServerTableLine(line) {
			skipping = true
			continue
		}
		if skipping {
			if isActiveTOMLHeader(trimmed) {
				skipping = false
			} else {
				continue
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "")
}

func parseNERVMCPServerBlock(config string) (bool, string) {
	lines := strings.SplitAfter(config, "\n")
	block := make([]string, 0)
	capturing := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if isNERVMCPServerTableLine(line) {
			capturing = true
			block = append(block, line)
			continue
		}
		if capturing {
			if isActiveTOMLHeader(trimmed) {
				break
			}
			block = append(block, line)
		}
	}
	if len(block) == 0 {
		return false, ""
	}
	return true, strings.TrimSpace(strings.Join(block, ""))
}

func buildNERVMCPServerBlock(assetPath string, options NERVMCPConfigOptions) string {
	options = normalizeNERVMCPConfigOptions(options)
	scriptPath := filepath.ToSlash(filepath.Join(assetPath, nervCodexMCPServerScript))
	args := []string{scriptPath}
	if options.WSLDistro != "" {
		args = append(args, "--wsl-distro", options.WSLDistro)
	}
	switch options.Backend {
	case "auto":
		args = append(args, "--auto")
	case "wsl":
		args = append(args, "--wsl")
	case "docker":
		args = append(args, "--docker", options.DockerContainer)
	case "ssh":
		args = append(args, "--kali", options.SSHHost)
	}

	quotedArgs := make([]string, 0, len(args))
	for _, arg := range args {
		quotedArgs = append(quotedArgs, strconv.Quote(arg))
	}

	return strings.Join([]string{
		nervCodexMCPManagedComment,
		fmt.Sprintf("[mcp_servers.%s]", nervCodexMCPServerName),
		`command = "python"`,
		"args = [" + strings.Join(quotedArgs, ", ") + "]",
		"startup_timeout_sec = 30",
		"",
	}, "\n")
}

func normalizeNERVMCPConfigOptions(options NERVMCPConfigOptions) NERVMCPConfigOptions {
	backend := strings.ToLower(strings.TrimSpace(options.Backend))
	switch backend {
	case "auto", "local", "wsl", "docker", "ssh":
	default:
		backend = "auto"
	}
	options.Backend = backend
	options.WSLDistro = strings.TrimSpace(options.WSLDistro)
	options.DockerContainer = strings.TrimSpace(options.DockerContainer)
	options.SSHHost = strings.TrimSpace(options.SSHHost)
	if options.WSLDistro == "" {
		options.WSLDistro = "kali-linux"
	}
	if options.DockerContainer == "" {
		options.DockerContainer = "kali-tools"
	}
	if options.SSHHost == "" {
		options.SSHHost = "root@192.168.1.100"
	}
	return options
}

func isNERVMCPServerTableLine(line string) bool {
	return strings.TrimSpace(line) == fmt.Sprintf("[mcp_servers.%s]", nervCodexMCPServerName)
}

func isActiveTOMLHeader(trimmed string) bool {
	return strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "#")
}

func isActiveNERVModelInstructionsLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "model_instructions_file") && !strings.HasPrefix(trimmed, "#")
}

func validateNERVToml(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var decoded map[string]any
	return toml.Unmarshal(data, &decoded)
}

func copyFile(src string, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(src); err == nil {
		mode = info.Mode().Perm()
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func copyDir(src string, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func expandNERVPath(path string) (string, error) {
	if path == "~" {
		return os.UserHomeDir()
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return path, err
		}
		return filepath.Join(home, path[2:]), nil
	}
	return os.ExpandEnv(path), nil
}

func replaceConfigSuffix(path string, suffix string) string {
	ext := filepath.Ext(path)
	return strings.TrimSuffix(path, ext) + suffix
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func buildNERVCodexConfigMessage(status NERVCodexConfigStatus) string {
	switch {
	case !status.Found:
		return "未找到 Codex config.toml"
	case !status.AssetBridgeExists:
		return "NERV bridge.md 内置资产未找到"
	case status.BridgeActive && status.BridgeExists && status.SkillCount > 0:
		return "NERV Codex 配置已部署"
	case status.BridgeActive:
		return "配置项已写入，但 bridge.md 或 skills 不完整"
	default:
		return "NERV Codex 配置未部署"
	}
}
