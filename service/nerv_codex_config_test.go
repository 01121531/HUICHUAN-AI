package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyAndRemoveNERVCodexConfig(t *testing.T) {
	home, assetPath := newNERVCodexConfigFixture(t, `model = "gpt-5"
`)

	status := NERVCodexConfigStatusFor(home, assetPath)
	if !status.Found || status.BridgeActive {
		t.Fatalf("unexpected initial status: %+v", status)
	}

	result, err := ApplyNERVCodexConfig(home, assetPath)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || !result.Status.BridgeActive || result.Status.SkillCount != 1 {
		t.Fatalf("unexpected apply result: %+v", result)
	}
	config := readTestFile(t, filepath.Join(home, "config.toml"))
	if !strings.Contains(config, `model_instructions_file = "./bridge.md"`) {
		t.Fatalf("config missing model_instructions_file: %s", config)
	}
	if !strings.Contains(readTestFile(t, filepath.Join(home, "bridge.md")), "NERV bridge") {
		t.Fatalf("bridge.md not copied")
	}
	if !fileExists(filepath.Join(home, "config.toml"+NERVCodexConfigBackupSuffix)) {
		t.Fatalf("backup not created")
	}

	removeResult, err := RemoveNERVCodexConfig(home, assetPath)
	if err != nil {
		t.Fatal(err)
	}
	if !removeResult.Changed || removeResult.Status.BridgeActive || removeResult.Status.BridgeExists || removeResult.Status.SkillsExists {
		t.Fatalf("unexpected remove result: %+v", removeResult)
	}
	restored := readTestFile(t, filepath.Join(home, "config.toml"))
	if strings.Contains(restored, "model_instructions_file") || !strings.Contains(restored, `model = "gpt-5"`) {
		t.Fatalf("config not restored: %s", restored)
	}
}

func TestApplyNERVCodexConfigReplacesExistingInstruction(t *testing.T) {
	home, assetPath := newNERVCodexConfigFixture(t, `model = "gpt-5"
model_instructions_file = "./old.md"
`)

	_, err := ApplyNERVCodexConfig(home, assetPath)
	if err != nil {
		t.Fatal(err)
	}
	config := readTestFile(t, filepath.Join(home, "config.toml"))
	if strings.Contains(config, "old.md") {
		t.Fatalf("old instruction not replaced: %s", config)
	}
	if count := strings.Count(config, "model_instructions_file"); count != 1 {
		t.Fatalf("expected exactly one instruction line, got %d: %s", count, config)
	}
}

func TestRemoveNERVCodexConfigWithoutBackupRemovesInstructionLine(t *testing.T) {
	home, assetPath := newNERVCodexConfigFixture(t, `model = "gpt-5"
model_instructions_file = "./bridge.md"
`)
	if err := os.WriteFile(filepath.Join(home, "bridge.md"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "skills", "x"), 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := RemoveNERVCodexConfig(home, assetPath)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Status.BridgeActive || result.Status.BridgeExists || result.Status.SkillsExists {
		t.Fatalf("unexpected result: %+v", result)
	}
	config := readTestFile(t, filepath.Join(home, "config.toml"))
	if strings.Contains(config, "model_instructions_file") {
		t.Fatalf("instruction line not removed: %s", config)
	}
}

func TestApplyAndRemoveNERVMCPConfig(t *testing.T) {
	home, assetPath := newNERVCodexConfigFixture(t, `model = "gpt-5"
model_instructions_file = "./bridge.md"

[mcp_servers.other]
command = "node"
args = ["server.js"]
`)

	result, err := ApplyNERVMCPConfig(home, assetPath, NERVMCPConfigOptions{
		Backend:   "ssh",
		SSHHost:   "root@kali",
		WSLDistro: "kali-linux",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || !result.Status.MCPActive || !result.Status.MCPBackupExists {
		t.Fatalf("unexpected apply result: %+v", result)
	}
	config := readTestFile(t, filepath.Join(home, "config.toml"))
	for _, want := range []string{
		"[mcp_servers.nerv_break]",
		`command = "python"`,
		`"--kali", "root@kali"`,
		`model_instructions_file = "./bridge.md"`,
		"[mcp_servers.other]",
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("config missing %q: %s", want, config)
		}
	}

	removeResult, err := RemoveNERVMCPConfig(home, assetPath)
	if err != nil {
		t.Fatal(err)
	}
	if !removeResult.Changed || removeResult.Status.MCPActive {
		t.Fatalf("unexpected remove result: %+v", removeResult)
	}
	removed := readTestFile(t, filepath.Join(home, "config.toml"))
	if strings.Contains(removed, "nerv_break") || strings.Contains(removed, nervCodexMCPManagedComment) {
		t.Fatalf("mcp block not removed: %s", removed)
	}
	for _, want := range []string{
		`model_instructions_file = "./bridge.md"`,
		"[mcp_servers.other]",
	} {
		if !strings.Contains(removed, want) {
			t.Fatalf("config should preserve %q: %s", want, removed)
		}
	}
}

func TestApplyNERVMCPConfigReplacesExistingBlock(t *testing.T) {
	home, assetPath := newNERVCodexConfigFixture(t, `model = "gpt-5"

[mcp_servers.nerv_break]
command = "python"
args = ["old.py"]
startup_timeout_sec = 5

[mcp_servers.keep]
command = "node"
`)

	_, err := ApplyNERVMCPConfig(home, assetPath, NERVMCPConfigOptions{Backend: "docker", DockerContainer: "kali"})
	if err != nil {
		t.Fatal(err)
	}
	config := readTestFile(t, filepath.Join(home, "config.toml"))
	if strings.Contains(config, "old.py") {
		t.Fatalf("old mcp block not replaced: %s", config)
	}
	if count := strings.Count(config, "[mcp_servers.nerv_break]"); count != 1 {
		t.Fatalf("expected exactly one mcp block, got %d: %s", count, config)
	}
	if !strings.Contains(config, `--docker", "kali`) || !strings.Contains(config, "[mcp_servers.keep]") {
		t.Fatalf("new mcp block or preserved block missing: %s", config)
	}
}

func newNERVCodexConfigFixture(t *testing.T, config string) (string, string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "codex")
	assetPath := filepath.Join(root, "asset")
	if err := os.MkdirAll(filepath.Join(assetPath, "skills", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetPath, "bridge.md"), []byte("NERV bridge"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetPath, "skills", "demo", "SKILL.md"), []byte("skill"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetPath, "mcp_server.py"), []byte("print('mcp')"), 0o600); err != nil {
		t.Fatal(err)
	}
	return home, assetPath
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
