package controller

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

func TestExtractNERVTamperPatternsFromBundledDirectSetup(t *testing.T) {
	sourcePath := filepath.Join("..", "nerv", "5.6-JAILBREAK-NERV", "direct_setup.py")
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read bundled direct_setup.py: %v", err)
	}

	patterns := extractNERVTamperPatternsFromPython(string(data))
	if len(patterns) != 22 {
		t.Fatalf("expected 22 bundled tamper patterns, got %d", len(patterns))
	}

	for index, pattern := range patterns {
		if _, err := regexp.Compile(pattern); err != nil {
			t.Fatalf("pattern %d does not compile: %v", index+1, err)
		}
	}
}

func TestExtractNERVTamperPatternsFromBundledProxyRelay(t *testing.T) {
	sourcePath := filepath.Join("..", "nerv", "5.6-JAILBREAK-NERV", "proxy_relay.py")
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read bundled proxy_relay.py: %v", err)
	}

	patterns := extractNERVTamperPatternsFromPython(string(data))
	if len(patterns) != 2 {
		t.Fatalf("expected 2 bundled proxy tamper patterns, got %d", len(patterns))
	}

	for index, pattern := range patterns {
		if _, err := regexp.Compile(pattern); err != nil {
			t.Fatalf("pattern %d does not compile: %v", index+1, err)
		}
	}
}

func TestBuildNERVVerifySmokeFromSelfCheck(t *testing.T) {
	status := nervSelfCheckResponse{
		Assets: nervSelfCheckAssets{
			BasePath: "/tmp/nerv",
			RequiredFiles: []nervSelfCheckRequiredFile{
				{Path: "bridge.md", Exists: true},
				{Path: "proxy_relay.py", Exists: true},
			},
		},
		Catalog: nervSelfCheckCatalog{
			ToolCount:  31,
			SkillCount: 28,
		},
		Checks: []nervSelfCheckItem{
			{Key: "assets", OK: true},
			{Key: "required_files", OK: true},
			{Key: "tools_catalog", OK: true},
			{Key: "skills_catalog", OK: true},
			{Key: "tamper_rules", OK: true},
			{Key: "recent_stats", OK: true},
			{Key: "prompt", OK: false},
		},
	}

	smoke := buildNERVVerifySmoke(status)
	if !smoke.OK {
		t.Fatalf("expected smoke to pass: %+v", smoke)
	}
	if smoke.AssetPath != "/tmp/nerv" || smoke.ToolCount != 31 || smoke.SkillCount != 28 {
		t.Fatalf("unexpected summary: %+v", smoke)
	}
	keys := make([]string, 0, len(smoke.Checks))
	for _, check := range smoke.Checks {
		keys = append(keys, check.Key)
	}
	for _, key := range []string{"assets", "required_files", "tools_catalog", "skills_catalog", "tamper_rules", "recent_stats"} {
		if !slices.Contains(keys, key) {
			t.Fatalf("missing smoke check %q in %+v", key, keys)
		}
	}
	if slices.Contains(keys, "prompt") {
		t.Fatalf("prompt check should not block smoke: %+v", keys)
	}
}

func TestBuildNERVVerifySmokeReportsMissingRequiredFiles(t *testing.T) {
	status := nervSelfCheckResponse{
		Assets: nervSelfCheckAssets{
			RequiredFiles: []nervSelfCheckRequiredFile{
				{Path: "bridge.md", Exists: false},
			},
		},
		Checks: []nervSelfCheckItem{
			{Key: "assets", OK: true},
			{Key: "required_files", OK: false},
			{Key: "tools_catalog", OK: true},
			{Key: "skills_catalog", OK: true},
			{Key: "tamper_rules", OK: true},
			{Key: "recent_stats", OK: true},
		},
	}

	smoke := buildNERVVerifySmoke(status)
	if smoke.OK {
		t.Fatalf("expected smoke to fail: %+v", smoke)
	}
	if len(smoke.MissingRequiredFiles) != 1 || smoke.MissingRequiredFiles[0] != "bridge.md" {
		t.Fatalf("missing file not reported: %+v", smoke.MissingRequiredFiles)
	}
}
