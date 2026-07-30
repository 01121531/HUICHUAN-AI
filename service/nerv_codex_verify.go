package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const nervCodexVerifyScriptName = "verify.py"

type NERVCodexVerifyCheck struct {
	Key     string `json:"key"`
	OK      bool   `json:"ok"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

type NERVCodexVerifyResult struct {
	OK                bool                   `json:"ok"`
	Home              string                 `json:"home"`
	Found             bool                   `json:"found"`
	ConfigPath        string                 `json:"config_path"`
	ScriptPath        string                 `json:"script_path"`
	PythonCommand     string                 `json:"python_command"`
	ExitCode          int                    `json:"exit_code"`
	TimedOut          bool                   `json:"timed_out"`
	DurationMs        int64                  `json:"duration_ms"`
	Output            string                 `json:"output"`
	Checks            []NERVCodexVerifyCheck `json:"checks"`
	Candidates        []string               `json:"candidates"`
	BridgeVerified    bool                   `json:"bridge_verified"`
	SkillsVerified    bool                   `json:"skills_verified"`
	CodexCLIAvailable bool                   `json:"codex_cli_available"`
	SmokeOK           bool                   `json:"smoke_ok"`
	Message           string                 `json:"message"`
}

func RunNERVCodexVerify(codexHome string, assetPath string, timeoutSeconds int) (NERVCodexVerifyResult, error) {
	home, found, candidates := ResolveNERVCodexHome(codexHome)
	if strings.TrimSpace(codexHome) != "" && !found {
		home = strings.TrimSpace(codexHome)
	}
	scriptPath := filepath.Join(assetPath, nervCodexVerifyScriptName)
	result := NERVCodexVerifyResult{
		Home:       home,
		Found:      found,
		ConfigPath: filepath.Join(home, "config.toml"),
		ScriptPath: scriptPath,
		Candidates: candidates,
		ExitCode:   -1,
	}
	if !fileExists(scriptPath) {
		return result, errors.New("NERV verify.py 内置资产未找到")
	}
	pythonCommand, err := findNERVPythonCommand()
	if err != nil {
		return result, err
	}
	result.PythonCommand = pythonCommand
	if timeoutSeconds <= 0 {
		timeoutSeconds = 45
	}
	if timeoutSeconds > 180 {
		timeoutSeconds = 180
	}

	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, pythonCommand, "-u", scriptPath)
	cmd.Dir = assetPath
	cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")
	if home != "" {
		cmd.Env = append(cmd.Env, "CODEX_HOME="+home)
	}
	output, runErr := cmd.CombinedOutput()
	result.DurationMs = time.Since(started).Milliseconds()
	result.Output = trimNERVVerifyOutput(string(output), 32768)
	if ctx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.Message = "verify.py 运行超时"
		return result, nil
	}
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			return result, runErr
		}
	} else {
		result.ExitCode = 0
	}

	enrichNERVCodexVerifyResult(&result)
	return result, nil
}

func enrichNERVCodexVerifyResult(result *NERVCodexVerifyResult) {
	output := result.Output
	result.Checks = parseNERVCodexVerifyChecks(output)
	result.BridgeVerified = strings.Contains(output, "bridge.md deployed OK")
	result.SkillsVerified = strings.Contains(output, "skill modules deployed") || strings.Contains(output, "skill modules (source dir)")
	result.CodexCLIAvailable = strings.Contains(output, "Codex CLI found")
	result.SmokeOK = strings.Contains(output, "Tool access OK")
	result.OK = strings.Contains(output, "ALL CHECKS PASSED") && result.ExitCode == 0 && !result.TimedOut
	if result.OK {
		result.Message = "原 verify.py 验证通过"
		return
	}
	if result.Message == "" {
		failed := 0
		for _, check := range result.Checks {
			if !check.OK {
				failed++
			}
		}
		if failed > 0 {
			result.Message = fmt.Sprintf("原 verify.py 有 %d 项未通过", failed)
		} else {
			result.Message = "原 verify.py 未返回全部通过标记"
		}
	}
}

func parseNERVCodexVerifyChecks(output string) []NERVCodexVerifyCheck {
	checks := make([]NERVCodexVerifyCheck, 0, 4)
	statusPattern := regexp.MustCompile(`\[(PASS|FAIL|WARN)\]\s*(.*)$`)
	for _, line := range strings.Split(output, "\n") {
		match := statusPattern.FindStringSubmatch(strings.TrimSpace(line))
		if len(match) < 3 {
			continue
		}
		level := strings.ToLower(match[1])
		checks = append(checks, NERVCodexVerifyCheck{
			Key:     fmt.Sprintf("verify_%d", len(checks)+1),
			OK:      level == "pass",
			Level:   level,
			Message: strings.TrimSpace(match[2]),
		})
	}
	return checks
}

func trimNERVVerifyOutput(output string, limit int) string {
	if limit <= 0 || len(output) <= limit {
		return output
	}
	return output[len(output)-limit:]
}
