package controller

import (
	"os"
	"path/filepath"
	"regexp"
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
