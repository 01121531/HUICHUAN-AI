package service

import "testing"

func TestParseNERVCodexVerifyChecks(t *testing.T) {
	output := `
[1/4] Checking bridge.md deployment...
  [PASS] bridge.md deployed OK (5 checks passed)
[2/4] Checking skill modules...
  [PASS] 27 skill modules deployed
[3/4] Locating Codex CLI...
  [WARN] Codex CLI binary not found in PATH
[4/4] Running smoke test...
  [PASS] Tool access OK
`
	checks := parseNERVCodexVerifyChecks(output)
	if len(checks) != 4 {
		t.Fatalf("expected 4 checks, got %d: %+v", len(checks), checks)
	}
	if !checks[0].OK || checks[2].OK || checks[2].Level != "warn" {
		t.Fatalf("unexpected parsed checks: %+v", checks)
	}
}

func TestEnrichNERVCodexVerifyResult(t *testing.T) {
	result := NERVCodexVerifyResult{
		ExitCode: 0,
		Output: `
  [PASS] bridge.md deployed OK (5 checks passed)
  [PASS] 27 skill modules deployed
  [PASS] Codex CLI found: codex
  [PASS] Tool access OK
  — zxwn test: ALL CHECKS PASSED
`,
	}
	enrichNERVCodexVerifyResult(&result)
	if !result.OK || !result.BridgeVerified || !result.SkillsVerified || !result.CodexCLIAvailable || !result.SmokeOK {
		t.Fatalf("expected successful verify result: %+v", result)
	}
}
