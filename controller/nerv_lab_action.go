package controller

import (
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/gin-gonic/gin"
)

type nervLabActionRequest struct {
	Action         string `json:"action"`
	Backend        string `json:"backend"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type nervLabActionResponse struct {
	Action      string `json:"action"`
	Backend     string `json:"backend"`
	Command     string `json:"command"`
	ExitCode    int    `json:"exit_code"`
	Stdout      string `json:"stdout"`
	Stderr      string `json:"stderr"`
	TimedOut    bool   `json:"timed_out"`
	DurationMs  int64  `json:"duration_ms"`
	OutputBytes int    `json:"output_bytes"`
	Message     string `json:"message"`
}

func RunNERVLabAction(c *gin.Context) {
	var request nervLabActionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}

	action := strings.ToLower(strings.TrimSpace(request.Action))
	backend := normalizeNERVToolBackend(request.Backend)
	if backend == "" {
		backend = readNERVConfiguredBackend()
	}
	if backend == "auto" {
		backend = detectNERVToolBackend()
	}
	timeout := time.Duration(normalizeNERVLabActionTimeout(request.TimeoutSeconds)) * time.Second

	var (
		result nervLabActionResponse
		err    error
	)
	switch action {
	case "tools-check":
		result, err = runNERVToolsCheckAction(backend, timeout)
	case "tools-install":
		result = runNERVToolsInstallAction(backend, timeout)
	case "kali-wsl":
		result = buildNERVGuideAction(action, backend, buildNERVKaliWSLGuide())
	case "kali-ssh":
		result = buildNERVGuideAction(action, backend, buildNERVKaliSSHGuide())
	case "ssh-test":
		result = runNERVSSHTestAction(timeout)
	default:
		common.ApiErrorMsg(c, "未知的 NERV 原脚本动作")
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func normalizeNERVLabActionTimeout(timeoutSeconds int) int {
	if timeoutSeconds <= 0 {
		return 60
	}
	if timeoutSeconds > 300 {
		return 300
	}
	return timeoutSeconds
}

func runNERVToolsCheckAction(backend string, timeout time.Duration) (nervLabActionResponse, error) {
	catalog, err := loadNERVToolCatalogFromAsset()
	if err != nil {
		return nervLabActionResponse{}, err
	}
	binaries := uniqueNERVCheckableBinaries(catalog.Tools)
	command := buildNERVToolsCheckCommand(binaries, backend)
	runResult := executeNERVToolCommand(command, backend, timeout)
	runResult.Command = command
	return nervLabActionFromToolResult("tools-check", backend, "工具检查完成", runResult), nil
}

func runNERVToolsInstallAction(backend string, timeout time.Duration) nervLabActionResponse {
	command := buildNERVToolsInstallCommand(backend)
	runResult := executeNERVToolCommand(command, backend, timeout)
	runResult.Command = command
	return nervLabActionFromToolResult("tools-install", backend, "基础 Python 工具安装命令已执行", runResult)
}

func runNERVSSHTestAction(timeout time.Duration) nervLabActionResponse {
	command := "echo OK"
	runResult := executeNERVToolCommand(command, "ssh", timeout)
	runResult.Command = command
	return nervLabActionFromToolResult("ssh-test", "ssh", "远程主机连通性测试完成", runResult)
}

func nervLabActionFromToolResult(action string, backend string, message string, result nervToolRunResponse) nervLabActionResponse {
	return nervLabActionResponse{
		Action:      action,
		Backend:     backend,
		Command:     result.Command,
		ExitCode:    result.ExitCode,
		Stdout:      result.Stdout,
		Stderr:      result.Stderr,
		TimedOut:    result.TimedOut,
		DurationMs:  result.DurationMs,
		OutputBytes: result.OutputBytes,
		Message:     message,
	}
}

func buildNERVGuideAction(action string, backend string, stdout string) nervLabActionResponse {
	return nervLabActionResponse{
		Action:      action,
		Backend:     backend,
		ExitCode:    0,
		Stdout:      stdout,
		OutputBytes: len(stdout),
		Message:     "已生成原脚本向导命令",
	}
}

func uniqueNERVCheckableBinaries(tools []nervToolCatalogItem) []string {
	seen := map[string]bool{}
	binaries := make([]string, 0, len(tools))
	for _, tool := range tools {
		binary := strings.TrimSpace(tool.Binary)
		if binary == "" || strings.Contains(binary, "{") || seen[binary] {
			continue
		}
		seen[binary] = true
		binaries = append(binaries, binary)
	}
	sort.Strings(binaries)
	return binaries
}

func buildNERVToolsCheckCommand(binaries []string, backend string) string {
	if len(binaries) == 0 {
		return "echo 没有可检查的工具"
	}
	if runtime.GOOS == "windows" && backend == "local" {
		quoted := make([]string, 0, len(binaries))
		for _, binary := range binaries {
			quoted = append(quoted, "'"+strings.ReplaceAll(binary, "'", "''")+"'")
		}
		return fmt.Sprintf("$items=@(%s); foreach($item in $items){ if(Get-Command $item -ErrorAction SilentlyContinue){ \"[OK] $item\" } else { \"[--] $item\" } }", strings.Join(quoted, ","))
	}

	quoted := make([]string, 0, len(binaries))
	for _, binary := range binaries {
		quoted = append(quoted, strconv.Quote(binary))
	}
	return fmt.Sprintf("for c in %s; do if command -v \"$c\" >/dev/null 2>&1; then echo \"[OK] $c\"; else echo \"[--] $c\"; fi; done", strings.Join(quoted, " "))
}

func buildNERVToolsInstallCommand(backend string) string {
	if runtime.GOOS == "windows" && backend == "local" {
		return "python -m pip install --user sqlmap pwntools"
	}
	return "python3 -m pip install --user sqlmap pwntools || python -m pip install --user sqlmap pwntools"
}

func buildNERVKaliWSLGuide() string {
	return strings.Join([]string{
		"请在 Windows 管理员终端执行：",
		"wsl --install -d kali-linux",
		"",
		"安装完成后进入 Kali 执行：",
		"sudo apt update",
		"sudo apt install -y kali-linux-headless nmap sqlmap hydra metasploit-framework john hashcat",
		"sudo apt install -y radare2 binwalk exiftool foremost tcpdump netcat-openbsd",
		"",
		"然后回到 Codex 配置里使用：",
		"python mcp_server.py --wsl --wsl-distro kali-linux",
	}, "\n")
}

func buildNERVKaliSSHGuide() string {
	host := readNERVOptionString("nerv_setting.ssh_host", "root@192.168.1.100")
	return strings.Join([]string{
		"远程 Kali 主机准备步骤：",
		fmt.Sprintf("ssh -o StrictHostKeyChecking=no %s \"echo OK\"", host),
		fmt.Sprintf("python mcp_server.py --kali %s", host),
		"",
		"如需修改远程地址，请到 NERV 连接设置里的工具后端区域填写。",
	}, "\n")
}
