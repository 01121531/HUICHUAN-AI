package controller

import (
	"path/filepath"
	"testing"
)

func TestLoadNERVToolCatalogFromBundledTools(t *testing.T) {
	toolsPath := filepath.Join("..", "nerv", "5.6-JAILBREAK-NERV", "tools", "tools.json")
	tools, err := loadNERVToolCatalog(toolsPath)
	if err != nil {
		t.Fatalf("load bundled tools catalog: %v", err)
	}
	if len(tools) != 31 {
		t.Fatalf("expected 31 bundled tools, got %d", len(tools))
	}
	if _, ok := findNERVTool(tools, "nmap_scan"); !ok {
		t.Fatal("nmap_scan not found")
	}
}

func TestRenderNERVToolCommand(t *testing.T) {
	tool := nervToolCatalogItem{
		Name:    "nmap_scan",
		Command: "nmap {flags} -p {ports} {target}",
		Params:  []string{"target", "ports", "flags"},
	}

	command, err := renderNERVToolCommand(tool, map[string]string{
		"target": "127.0.0.1",
		"ports":  "80,443",
		"flags":  "-sV",
	})
	if err != nil {
		t.Fatalf("render command: %v", err)
	}
	if command != "nmap -sV -p 80,443 127.0.0.1" {
		t.Fatalf("unexpected command: %s", command)
	}
}

func TestRenderNERVToolCommandAllowsEmptyParams(t *testing.T) {
	tool := nervToolCatalogItem{
		Name:    "nmap_scan",
		Command: "nmap {flags} -p {ports} {target}",
		Params:  []string{"target", "ports", "flags"},
	}

	command, err := renderNERVToolCommand(tool, map[string]string{
		"target": "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("render command with empty params: %v", err)
	}
	if command != "nmap  -p  127.0.0.1" {
		t.Fatalf("unexpected command: %s", command)
	}
}

func TestRenderNERVToolCommandEscapesSingleQuotes(t *testing.T) {
	tool := nervToolCatalogItem{
		Name:    "curl_fetch",
		Command: "curl -sL -D - '{url}' {flags}",
		Params:  []string{"url", "flags"},
	}

	command, err := renderNERVToolCommand(tool, map[string]string{
		"url":   "http://example.com/a'b",
		"flags": "",
	})
	if err != nil {
		t.Fatalf("render command: %v", err)
	}
	want := "curl -sL -D - 'http://example.com/a'\"'\"'b' "
	if command != want {
		t.Fatalf("unexpected escaped command: got %q want %q", command, want)
	}
}
