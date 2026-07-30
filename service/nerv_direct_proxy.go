package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	nervDirectProxyScriptName    = "direct_setup.py"
	nervDirectProxyPidFileName   = "direct-proxy.pid.json"
	nervDirectProxyLogName       = "direct-proxy.log"
	nervDirectProxyListenAddress = "127.0.0.1:8080"
)

var nervDirectProxyRuntimeDirFunc = defaultNERVDirectProxyRuntimeDir

type NERVDirectProxyStatus struct {
	Running       bool     `json:"running"`
	PID           int      `json:"pid"`
	AssetPath     string   `json:"asset_path"`
	ScriptPath    string   `json:"script_path"`
	CodexHome     string   `json:"codex_home"`
	PIDPath       string   `json:"pid_path"`
	LogPath       string   `json:"log_path"`
	ListenURL     string   `json:"listen_url"`
	ListenOpen    bool     `json:"listen_open"`
	StartedAt     int64    `json:"started_at"`
	Message       string   `json:"message"`
	LogTail       string   `json:"log_tail"`
	PythonCommand string   `json:"python_command"`
	Candidates    []string `json:"candidates"`
}

type NERVDirectProxyResult struct {
	Action  string                `json:"action"`
	Changed bool                  `json:"changed"`
	Message string                `json:"message"`
	Status  NERVDirectProxyStatus `json:"status"`
}

type nervDirectProxyState struct {
	PID           int    `json:"pid"`
	StartedAt     int64  `json:"started_at"`
	AssetPath     string `json:"asset_path"`
	CodexHome     string `json:"codex_home"`
	PythonCommand string `json:"python_command"`
}

func NERVDirectProxyStatusFor(assetPath string) NERVDirectProxyStatus {
	runtimeDir := nervDirectProxyRuntimeDirFunc()
	state, _ := readNERVDirectProxyState(runtimeDir)
	pythonCommand, _ := findNERVPythonCommand()
	status := NERVDirectProxyStatus{
		AssetPath:     assetPath,
		ScriptPath:    filepath.Join(assetPath, nervDirectProxyScriptName),
		PIDPath:       filepath.Join(runtimeDir, nervDirectProxyPidFileName),
		LogPath:       filepath.Join(runtimeDir, nervDirectProxyLogName),
		ListenURL:     "http://" + nervDirectProxyListenAddress + "/v1",
		ListenOpen:    tcpOpen(nervDirectProxyListenAddress),
		LogTail:       tailNERVProxyLog(filepath.Join(runtimeDir, nervDirectProxyLogName), 4096),
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
			status.ScriptPath = filepath.Join(state.AssetPath, nervDirectProxyScriptName)
		}
		if state.PythonCommand != "" {
			status.PythonCommand = state.PythonCommand
		}
		status.Running = processRunning(state.PID)
	}
	status.Message = buildNERVDirectProxyMessage(status)
	return status
}

func StartNERVDirectProxy(assetPath string, codexHome string) (NERVDirectProxyResult, error) {
	status := NERVDirectProxyStatusFor(assetPath)
	result := NERVDirectProxyResult{Action: "start", Status: status}
	if status.Running {
		result.Message = "NERV 直连代理已在运行"
		return result, nil
	}
	if status.ListenOpen {
		return result, errors.New("127.0.0.1:8080 已被其他代理占用，请先停止外置代理或占用进程")
	}
	if !fileExists(filepath.Join(assetPath, nervDirectProxyScriptName)) {
		return result, errors.New("NERV direct_setup.py 内置资产未找到")
	}
	pythonCommand, err := findNERVPythonCommand()
	if err != nil {
		return result, err
	}

	resolvedHome, foundHome, _ := ResolveNERVCodexHome(codexHome)
	if codexHome != "" && !foundHome {
		resolvedHome = codexHome
	}

	runtimeDir := nervDirectProxyRuntimeDirFunc()
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		return result, err
	}
	logPath := filepath.Join(runtimeDir, nervDirectProxyLogName)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return result, err
	}
	defer logFile.Close()
	_, _ = fmt.Fprintf(logFile, "\n===== NERV direct proxy start %s =====\n", time.Now().Format(time.RFC3339))

	cmd := exec.Command(pythonCommand, "-u", filepath.Join(assetPath, nervDirectProxyScriptName), "proxy")
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

	state := nervDirectProxyState{
		PID:           cmd.Process.Pid,
		StartedAt:     time.Now().Unix(),
		AssetPath:     assetPath,
		CodexHome:     resolvedHome,
		PythonCommand: pythonCommand,
	}
	if err := writeNERVDirectProxyState(runtimeDir, state); err != nil {
		_ = cmd.Process.Kill()
		return result, err
	}
	_ = cmd.Process.Release()

	time.Sleep(700 * time.Millisecond)
	result.Changed = true
	result.Message = "NERV 直连代理已启动"
	result.Status = NERVDirectProxyStatusFor(assetPath)
	return result, nil
}

func StopNERVDirectProxy(assetPath string) (NERVDirectProxyResult, error) {
	status := NERVDirectProxyStatusFor(assetPath)
	result := NERVDirectProxyResult{Action: "stop", Status: status}
	runtimeDir := nervDirectProxyRuntimeDirFunc()
	if status.PID <= 0 {
		result.Message = "NERV 直连代理未运行"
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
		result.Message = "NERV 直连代理已停止"
		time.Sleep(500 * time.Millisecond)
	}
	_ = os.Remove(filepath.Join(runtimeDir, nervDirectProxyPidFileName))
	result.Status = NERVDirectProxyStatusFor(assetPath)
	return result, nil
}

func defaultNERVDirectProxyRuntimeDir() string {
	wd, err := os.Getwd()
	if err != nil || wd == "" {
		return filepath.Join(os.TempDir(), "huichuan-nerv-direct-proxy")
	}
	return filepath.Join(wd, "logs", "nerv-direct-proxy")
}

func readNERVDirectProxyState(runtimeDir string) (nervDirectProxyState, error) {
	var state nervDirectProxyState
	data, err := os.ReadFile(filepath.Join(runtimeDir, nervDirectProxyPidFileName))
	if err != nil {
		return state, err
	}
	err = json.Unmarshal(data, &state)
	return state, err
}

func writeNERVDirectProxyState(runtimeDir string, state nervDirectProxyState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(runtimeDir, nervDirectProxyPidFileName), data, 0o644)
}

func buildNERVDirectProxyMessage(status NERVDirectProxyStatus) string {
	switch {
	case status.Running && status.ListenOpen:
		return "NERV 直连代理运行中"
	case status.Running:
		return "NERV 直连代理进程存在，但 8080 端口尚未就绪"
	case status.PID > 0:
		return "NERV 直连代理已停止，存在旧状态文件"
	case status.ListenOpen:
		return "8080 端口已被其他代理占用"
	default:
		return "NERV 直连代理未运行"
	}
}
