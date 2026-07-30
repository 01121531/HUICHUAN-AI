package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func withNERVProxyRuntimeDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	previous := nervProxyRuntimeDirFunc
	nervProxyRuntimeDirFunc = func() string { return dir }
	t.Cleanup(func() {
		nervProxyRuntimeDirFunc = previous
	})
	return dir
}

func TestNERVProxyProcessStatusWithoutPID(t *testing.T) {
	runtimeDir := withNERVProxyRuntimeDir(t)
	assetPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(runtimeDir, nervProxyProcessLogName), []byte("hello nerv proxy"), 0o600); err != nil {
		t.Fatal(err)
	}

	status := NERVProxyProcessStatusFor(assetPath)
	if status.Running || status.PID != 0 {
		t.Fatalf("expected stopped status: %+v", status)
	}
	if status.PIDPath != filepath.Join(runtimeDir, nervProxyPidFileName) {
		t.Fatalf("unexpected pid path: %+v", status)
	}
	if !strings.Contains(status.LogTail, "hello nerv proxy") {
		t.Fatalf("log tail missing: %+v", status.LogTail)
	}
}

func TestReadWriteNERVProxyProcessState(t *testing.T) {
	runtimeDir := withNERVProxyRuntimeDir(t)
	state := nervProxyProcessState{
		PID:           12345,
		StartedAt:     time.Now().Unix(),
		AssetPath:     "/tmp/nerv",
		CodexHome:     "/tmp/codex",
		PythonCommand: "/usr/bin/python3",
	}
	if err := writeNERVProxyProcessState(runtimeDir, state); err != nil {
		t.Fatal(err)
	}
	got, err := readNERVProxyProcessState(runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	if got.PID != state.PID || got.AssetPath != state.AssetPath || got.CodexHome != state.CodexHome {
		t.Fatalf("state mismatch: got=%+v want=%+v", got, state)
	}
}

func TestRestoreNERVProxyAutoConfig(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.toml")
	backupPath := replaceConfigSuffix(configPath, ".toml.nerv-bak")
	if err := os.WriteFile(configPath, []byte("base_url = \"http://127.0.0.1:8080/v1\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupPath, []byte("base_url = \"http://127.0.0.1:57321/v1\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "bridge.md"), []byte("bridge"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "skills", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := RestoreNERVProxyAutoConfig(home); err != nil {
		t.Fatal(err)
	}
	config := readTestFile(t, configPath)
	if !strings.Contains(config, "57321") || strings.Contains(config, "8080") {
		t.Fatalf("config not restored: %s", config)
	}
	if fileExists(backupPath) || fileExists(filepath.Join(home, "bridge.md")) || dirExists(filepath.Join(home, "skills")) {
		t.Fatalf("proxy artifacts not removed")
	}
}
