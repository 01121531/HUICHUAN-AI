package controller

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestHandleNERVMCPInitialize(t *testing.T) {
	response := handleNERVMCPRequest(nervMCPRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
	})

	if response.Error != nil {
		t.Fatalf("unexpected error: %#v", response.Error)
	}
	result, ok := response.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %#v", response.Result)
	}
	if result["protocolVersion"] != nervMCPProtocolVersion {
		t.Fatalf("unexpected protocol version: %#v", result["protocolVersion"])
	}
}

func TestBuildNERVMCPToolsListFromBundledCatalog(t *testing.T) {
	t.Setenv("NERV_ASSET_PATH", filepath.Join("..", "nerv", "5.6-JAILBREAK-NERV"))

	result, err := buildNERVMCPToolsList()
	if err != nil {
		t.Fatalf("build tools list: %v", err)
	}
	tools, ok := result["tools"].([]map[string]any)
	if !ok {
		t.Fatalf("unexpected tools type: %#v", result["tools"])
	}
	if len(tools) != 31 {
		t.Fatalf("expected 31 tools, got %d", len(tools))
	}
	for _, tool := range tools {
		if tool["name"] == "nmap_scan" {
			if _, ok := tool["inputSchema"].(map[string]any); !ok {
				t.Fatalf("nmap_scan input schema missing: %#v", tool)
			}
			return
		}
	}
	t.Fatal("nmap_scan not found in MCP tools list")
}

func TestNERVMCPUnknownMethod(t *testing.T) {
	response := handleNERVMCPRequest(nervMCPRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`2`),
		Method:  "unknown/method",
	})

	if response.Error == nil || response.Error.Code != -32601 {
		t.Fatalf("expected method not found error, got %#v", response.Error)
	}
}

func TestCallNERVMCPUnknownTool(t *testing.T) {
	t.Setenv("NERV_ASSET_PATH", filepath.Join("..", "nerv", "5.6-JAILBREAK-NERV"))

	_, err := callNERVMCPTool(json.RawMessage(`{"name":"missing_tool","arguments":{}}`))
	if err == nil {
		t.Fatal("expected missing tool error")
	}
}
