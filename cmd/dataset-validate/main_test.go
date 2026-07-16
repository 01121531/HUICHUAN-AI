package main

import (
	"encoding/json"
	"testing"

	"github.com/01121531/HUICHUAN-AI/pkg/datasetcapture"
)

func TestValidateLine(t *testing.T) {
	content := "answer"
	stop := "end_turn"
	createdAt := "2026-07-15T12:00:00Z"
	record := datasetcapture.Record{
		SessionID:    "0123456789abcdef",
		Model:        "gpt-test",
		SystemPrompt: "",
		Tools:        []datasetcapture.Tool{},
		Messages: []datasetcapture.Message{{
			Role:    "user",
			Content: []datasetcapture.ContentBlock{{"type": "text", "text": "question"}},
		}},
		Response: datasetcapture.Response{
			Content:    &content,
			StopReason: &stop,
			ToolUse: datasetcapture.ToolUse{
				InputAlreadyMerged: true,
				Calls:              []datasetcapture.ToolCall{},
			},
		},
		CreatedAt: &createdAt,
		Meta: datasetcapture.Meta{
			Version: "v1",
		},
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateLine(data, 1); err != nil {
		t.Fatal(err)
	}

	var object map[string]any
	_ = json.Unmarshal(data, &object)
	object["unexpected"] = true
	bad, _ := json.Marshal(object)
	if err := validateLine(bad, 1); err == nil {
		t.Fatal("validator accepted an unexpected root field")
	}
}
