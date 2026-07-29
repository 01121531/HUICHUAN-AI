package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/dto"
)

func TestNERVBridgeInjectsAndTampers(t *testing.T) {
	restore := setNERVTestOptions(t, map[string]string{
		NERVEnabledKey:          "true",
		NERVChatEnabledKey:      "true",
		NERVResponsesEnabledKey: "true",
		NERVTamperEnabledKey:    "true",
		NERVPromptKey:           "桥接提示词",
		NERVModeKey:             "prepend",
		NERVModelsKey:           "gpt-5.6*",
		NERVTargetsKey:          "*",
		NERVTamperReplyKey:      "替换完成",
		NERVTamperPatternsKey:   "",
	})
	defer restore()

	chatReq := &dto.GeneralOpenAIRequest{
		Model: "gpt-5.6-pro",
		Messages: []dto.Message{{
			Role:    "system",
			Content: "原系统指令",
		}},
	}
	if err := ApplyNERVToChatRequest(chatReq, NERVTargetOpenAIChat); err != nil {
		t.Fatalf("ApplyNERVToChatRequest failed: %v", err)
	}
	chatContent := chatReq.Messages[0].StringContent()
	if !strings.Contains(chatContent, "桥接提示词") || !strings.Contains(chatContent, "原系统指令") {
		t.Fatalf("unexpected chat content: %q", chatContent)
	}

	responsesReq := &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-pro",
	}
	raw, err := common.Marshal("原系统指令")
	if err != nil {
		t.Fatalf("marshal instructions failed: %v", err)
	}
	responsesReq.Instructions = raw
	if err := ApplyNERVToResponsesRequest(responsesReq, NERVTargetOpenAIResponses); err != nil {
		t.Fatalf("ApplyNERVToResponsesRequest failed: %v", err)
	}
	instructions, ok := DecodeResponsesInstructions(responsesReq.Instructions)
	if !ok {
		t.Fatal("responses instructions were not preserved")
	}
	if !strings.Contains(instructions, "桥接提示词") || !strings.Contains(instructions, "原系统指令") {
		t.Fatalf("unexpected responses instructions: %q", instructions)
	}

	chatResp := &dto.OpenAITextResponse{
		Choices: []dto.OpenAITextResponseChoice{{
			Message: dto.Message{Content: "I can't assist with that"},
		}},
	}
	if !ApplyNERVTamperToChatResponse(chatResp, NERVTargetOpenAIChat) {
		t.Fatal("expected chat response to be tampered")
	}
	if got := chatResp.Choices[0].Message.StringContent(); got != "替换完成" {
		t.Fatalf("unexpected tampered chat response: %q", got)
	}

	responsesResp := &dto.OpenAIResponsesResponse{
		Model: "gpt-5.6-pro",
		Output: []dto.ResponsesOutput{{
			Content: []dto.ResponsesOutputContent{{
				Text: "无法帮助你",
			}},
		}},
	}
	if !ApplyNERVTamperToResponsesResponse(responsesResp, NERVTargetOpenAIResponses) {
		t.Fatal("expected responses output to be tampered")
	}
	if got := responsesResp.Output[0].Content[0].Text; got != "替换完成" {
		t.Fatalf("unexpected tampered responses output: %q", got)
	}

	common.OptionMapRWMutex.RLock()
	total := common.OptionMap[NERVStatsTotalKey]
	inject := common.OptionMap[NERVStatsInjectKey]
	tamper := common.OptionMap[NERVStatsTamperKey]
	lastTarget := common.OptionMap[NERVStatsLastTargetKey]
	lastModel := common.OptionMap[NERVStatsLastModelKey]
	common.OptionMapRWMutex.RUnlock()
	if total != "4" || inject != "2" || tamper != "2" {
		t.Fatalf("unexpected NERV stats: total=%s inject=%s tamper=%s", total, inject, tamper)
	}
	if lastTarget != string(NERVTargetOpenAIResponses) || lastModel != "gpt-5.6-pro" {
		t.Fatalf("unexpected last NERV event metadata: target=%q model=%q", lastTarget, lastModel)
	}
}

func TestNERVSkillsContextInjectedFromRequestText(t *testing.T) {
	tempDir := t.TempDir()
	skillDir := filepath.Join(tempDir, "skills", "web-pentest")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("create skill dir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: web-pentest
description: Web application penetration testing. Trigger: sqli, xss, web pentest.
---

# Web Pentest

Use the web testing workflow for SQL injection and XSS checks.
`), 0o644); err != nil {
		t.Fatalf("write skill failed: %v", err)
	}

	t.Setenv("NERV_ASSET_PATH", tempDir)
	resetNERVSkillsCacheForTest()
	t.Cleanup(resetNERVSkillsCacheForTest)

	restore := setNERVTestOptions(t, map[string]string{
		NERVEnabledKey:          "true",
		NERVChatEnabledKey:      "true",
		NERVSkillsEnabledKey:    "true",
		NERVSkillsLimitKey:      "1",
		NERVPromptKey:           "桥接提示词",
		NERVModeKey:             "prepend",
		NERVModelsKey:           "*",
		NERVTargetsKey:          "*",
		NERVTamperEnabledKey:    "false",
		NERVTamperReplyKey:      "",
		NERVTamperPatternsKey:   "",
		NERVResponsesEnabledKey: "true",
	})
	defer restore()

	chatReq := &dto.GeneralOpenAIRequest{
		Model: "codex-test",
		Messages: []dto.Message{
			{Role: "system", Content: "原系统指令"},
			{Role: "user", Content: "请做一次 web pentest，重点检查 sqli 和 xss。"},
		},
	}
	if err := ApplyNERVToChatRequest(chatReq, NERVTargetOpenAIChat); err != nil {
		t.Fatalf("ApplyNERVToChatRequest failed: %v", err)
	}
	content := chatReq.Messages[0].StringContent()
	if !strings.Contains(content, "桥接提示词") {
		t.Fatalf("bridge prompt missing: %q", content)
	}
	if !strings.Contains(content, "[NERV 技能模块]") || !strings.Contains(content, "web-pentest") {
		t.Fatalf("skill context missing: %q", content)
	}
	if !strings.Contains(content, "原系统指令") {
		t.Fatalf("existing instructions missing: %q", content)
	}
}

func setNERVTestOptions(t *testing.T, values map[string]string) func() {
	t.Helper()

	common.OptionMapRWMutex.Lock()
	hadMap := common.OptionMap != nil
	if !hadMap {
		common.OptionMap = make(map[string]string)
	}

	originals := make(map[string]*string, len(values))
	for key, value := range values {
		if current, ok := common.OptionMap[key]; ok {
			copyValue := current
			originals[key] = &copyValue
		} else {
			originals[key] = nil
		}
		common.OptionMap[key] = value
	}
	common.OptionMapRWMutex.Unlock()

	return func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		for key, original := range originals {
			if original == nil {
				delete(common.OptionMap, key)
				continue
			}
			common.OptionMap[key] = *original
		}
		if !hadMap {
			common.OptionMap = nil
		}
	}
}

func TestNERVTamperPatternsKeepRegexQuantifierCommas(t *testing.T) {
	restore := setNERVTestOptions(t, map[string]string{
		NERVEnabledKey:        "true",
		NERVTamperEnabledKey:  "true",
		NERVTargetsKey:        "*",
		NERVTamperReplyKey:    "NERV_TEST_REPLACED",
		NERVTamperPatternsKey: `(?i)I (?:can'?t|cannot|won't|am unable to).{0,80}(?:assist|help|provide|do that)`,
	})
	defer restore()

	count, invalid := NERVTamperPatternDiagnostics(common.OptionMap[NERVTamperPatternsKey])
	if count != 1 {
		t.Fatalf("expected one tamper pattern, got %d", count)
	}
	if len(invalid) != 0 {
		t.Fatalf("expected pattern to compile, got errors: %#v", invalid)
	}

	replacement, tampered := ApplyNERVTamperToStreamText(
		"I cannot assist with that request.",
		NERVTargetOpenAIChat,
		"gpt-test",
	)
	if !tampered {
		t.Fatal("expected stream text to be tampered")
	}
	if replacement != "NERV_TEST_REPLACED" {
		t.Fatalf("unexpected replacement: %q", replacement)
	}
}
