package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	nervProxyScriptName       = "proxy_relay.py"
	nervProxyPidFileName      = "proxy.pid.json"
	nervProxyProcessLogName   = "proxy-process.log"
	nervProxyListenAddress    = "127.0.0.1:8080"
	nervProxyDashboardAddress = "127.0.0.1:8090"
)

var nervProxyRuntimeDirFunc = defaultNERVProxyRuntimeDir

type NERVProxyProcessStatus struct {
	Running       bool     `json:"running"`
	PID           int      `json:"pid"`
	AssetPath     string   `json:"asset_path"`
	ScriptPath    string   `json:"script_path"`
	CodexHome     string   `json:"codex_home"`
	PIDPath       string   `json:"pid_path"`
	LogPath       string   `json:"log_path"`
	ListenURL     string   `json:"listen_url"`
	DashboardURL  string   `json:"dashboard_url"`
	ListenOpen    bool     `json:"listen_open"`
	DashboardOpen bool     `json:"dashboard_open"`
	StartedAt     int64    `json:"started_at"`
	Message       string   `json:"message"`
	LogTail       string   `json:"log_tail"`
	PythonCommand string   `json:"python_command"`
	Candidates    []string `json:"candidates"`
}

type NERVProxyProcessResult struct {
	Action  string                 `json:"action"`
	Changed bool                   `json:"changed"`
	Message string                 `json:"message"`
	Status  NERVProxyProcessStatus `json:"status"`
}

type nervProxyProcessState struct {
	PID           int    `json:"pid"`
	StartedAt     int64  `json:"started_at"`
	AssetPath     string `json:"asset_path"`
	CodexHome     string `json:"codex_home"`
	PythonCommand string `json:"python_command"`
}

func NERVProxyProcessStatusFor(assetPath string) NERVProxyProcessStatus {
	runtimeDir := nervProxyRuntimeDirFunc()
	state, _ := readNERVProxyProcessState(runtimeDir)
	pythonCommand, _ := findNERVPythonCommand()
	status := NERVProxyProcessStatus{
		AssetPath:     assetPath,
		ScriptPath:    filepath.Join(assetPath, nervProxyScriptName),
		PIDPath:       filepath.Join(runtimeDir, nervProxyPidFileName),
		LogPath:       filepath.Join(runtimeDir, nervProxyProcessLogName),
		ListenURL:     "http://" + nervProxyListenAddress + "/v1",
		DashboardURL:  "http://" + nervProxyDashboardAddress + "/",
		ListenOpen:    tcpOpen(nervProxyListenAddress),
		DashboardOpen: tcpOpen(nervProxyDashboardAddress),
		LogTail:       tailNERVProxyLog(filepath.Join(runtimeDir, nervProxyProcessLogName), 4096),
		PythonCommand: pythonCommand,
	}
	_, _, candidates := ResolveNERVCodexHome("")
	status.Candidates = candidates
	if state.PID > 0 {
		status.PID = state.PID
		status.StartedAt = state.StartedAt
		status.CodexHome = state.CodexHome
		if state.AssetPath != "" {
			status.AssetPath = state.AssetPath
			status.ScriptPath = filepath.Join(state.AssetPath, nervProxyScriptName)
		}
		if state.PythonCommand != "" {
			status.PythonCommand = state.PythonCommand
		}
		status.Running = processRunning(state.PID)
	}
	status.Message = buildNERVProxyProcessMessage(status)
	return status
}

func StartNERVProxyProcess(assetPath string, codexHome string) (NERVProxyProcessResult, error) {
	status := NERVProxyProcessStatusFor(assetPath)
	result := NERVProxyProcessResult{Action: "start", Status: status}
	if status.Running {
		result.Message = "NERV 外置代理已在运行"
		return result, nil
	}
	if !fileExists(filepath.Join(assetPath, nervProxyScriptName)) {
		return result, errors.New("NERV proxy_relay.py 内置资产未找到")
	}
	pythonCommand, err := findNERVPythonCommand()
	if err != nil {
		return result, err
	}

	resolvedHome, foundHome, _ := ResolveNERVCodexHome(codexHome)
	if strings.TrimSpace(codexHome) != "" && !foundHome {
		resolvedHome = strings.TrimSpace(codexHome)
	}

	runtimeDir := nervProxyRuntimeDirFunc()
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		return result, err
	}
	logPath := filepath.Join(runtimeDir, nervProxyProcessLogName)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return result, err
	}
	defer logFile.Close()
	_, _ = fmt.Fprintf(logFile, "\n===== NERV proxy start %s =====\n", time.Now().Format(time.RFC3339))

	cmd := exec.Command(pythonCommand, "-u", filepath.Join(assetPath, nervProxyScriptName))
	cmd.Dir = assetPath
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")
	if resolvedHome != "" {
		cmd.Env = append(cmd.Env, "CODEX_HOME="+resolvedHome)
	}
	if err := cmd.Start(); err != nil {
		return result, err
	}

	state := nervProxyProcessState{
		PID:           cmd.Process.Pid,
		StartedAt:     time.Now().Unix(),
		AssetPath:     assetPath,
		CodexHome:     resolvedHome,
		PythonCommand: pythonCommand,
	}
	if err := writeNERVProxyProcessState(runtimeDir, state); err != nil {
		_ = cmd.Process.Kill()
		return result, err
	}
	_ = cmd.Process.Release()

	time.Sleep(700 * time.Millisecond)
	result.Changed = true
	result.Message = "NERV 外置代理已启动"
	result.Status = NERVProxyProcessStatusFor(assetPath)
	return result, nil
}

func StopNERVProxyProcess(assetPath string, restoreConfig bool) (NERVProxyProcessResult, error) {
	status := NERVProxyProcessStatusFor(assetPath)
	result := NERVProxyProcessResult{Action: "stop", Status: status}
	runtimeDir := nervProxyRuntimeDirFunc()
	if status.PID <= 0 {
		result.Message = "NERV 外置代理未运行"
		return result, nil
	}
	if status.Running {
		process, err := os.FindProcess(status.PID)
		if err != nil {
			return result, err
		}
		if err := process.Kill(); err != nil {
			return result, err
		}
		result.Changed = true
		result.Message = "NERV 外置代理已停止"
		time.Sleep(500 * time.Millisecond)
	}
	_ = os.Remove(filepath.Join(runtimeDir, nervProxyPidFileName))
	if restoreConfig && status.CodexHome != "" {
		_ = RestoreNERVProxyAutoConfig(status.CodexHome)
	}
	result.Status = NERVProxyProcessStatusFor(assetPath)
	return result, nil
}

func RestoreNERVProxyAutoConfig(codexHome string) error {
	codexHome = strings.TrimSpace(codexHome)
	if codexHome == "" {
		return nil
	}
	configPath := filepath.Join(codexHome, "config.toml")
	backupPath := replaceConfigSuffix(configPath, ".toml.nerv-bak")
	if fileExists(backupPath) {
		if err := copyFile(backupPath, configPath); err != nil {
			return err
		}
		_ = os.Remove(backupPath)
	}
	_ = os.Remove(filepath.Join(codexHome, nervCodexBridgeFileName))
	_ = os.RemoveAll(filepath.Join(codexHome, nervCodexSkillsDirName))
	return nil
}

func defaultNERVProxyRuntimeDir() string {
	wd, err := os.Getwd()
	if err != nil || wd == "" {
		return filepath.Join(os.TempDir(), "huichuan-nerv-proxy")
	}
	return filepath.Join(wd, "logs", "nerv-proxy")
}

func readNERVProxyProcessState(runtimeDir string) (nervProxyProcessState, error) {
	var state nervProxyProcessState
	data, err := os.ReadFile(filepath.Join(runtimeDir, nervProxyPidFileName))
	if err != nil {
		return state, err
	}
	err = json.Unmarshal(data, &state)
	return state, err
}

func writeNERVProxyProcessState(runtimeDir string, state nervProxyProcessState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(runtimeDir, nervProxyPidFileName), data, 0o644)
}

func findNERVPythonCommand() (string, error) {
	for _, candidate := range []string{"python3", "python"} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	return "", errors.New("未找到 python3/python，无法启动 NERV 外置代理")
}

func processRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func tcpOpen(address string) bool {
	conn, err := net.DialTimeout("tcp", address, 150*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func tailNERVProxyLog(path string, limit int64) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ""
	}
	offset := info.Size() - limit
	if offset < 0 {
		offset = 0
	}
	if _, err := file.Seek(offset, 0); err != nil {
		return ""
	}
	buf := make([]byte, info.Size()-offset)
	n, _ := file.Read(buf)
	return string(buf[:n])
}

func buildNERVProxyProcessMessage(status NERVProxyProcessStatus) string {
	switch {
	case status.Running && status.ListenOpen:
		return "NERV 外置代理运行中"
	case status.Running:
		return "NERV 外置代理进程存在，但端口尚未就绪"
	case status.PID > 0:
		return "NERV 外置代理已停止，存在旧状态文件"
	default:
		return "NERV 外置代理未运行"
	}
}
