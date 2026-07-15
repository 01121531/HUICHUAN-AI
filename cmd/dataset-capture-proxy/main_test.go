package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/pkg/datasetcapture"
)

func TestProxyPassesCredentialsAndCapturesStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("authorization was not forwarded")
		}
		if r.URL.Query().Get("key") != "query-secret" {
			t.Errorf("query was not forwarded")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if string(body) == "" {
			t.Error("request body was not forwarded")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hel\"},\"finish_reason\":null}]}\n\n")
		w.(http.Flusher).Flush()
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"lo\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":1}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()
	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "sample.jsonl")
	writer, err := datasetcapture.NewWriter(datasetcapture.WriterConfig{PathTemplate: output, QueueSize: 8})
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(newProxyHandler(target, writer, "test-key"))
	defer proxy.Close()

	requestBody := `{"model":"gpt-test","messages":[{"role":"user","content":"hi"}]}`
	req, err := http.NewRequest(http.MethodPost, proxy.URL+"/v1/chat/completions?key=query-secret", strings.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	responseBody, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(responseBody) == 0 || resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%q", resp.StatusCode, responseBody)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var record datasetcapture.Record
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record.Response.Content == nil || *record.Response.Content != "hello" {
		t.Fatalf("unexpected captured response: %#v", record.Response)
	}
	serialized := string(data)
	if containsAny(serialized, "Bearer secret", "query-secret", "Authorization") {
		t.Fatalf("credential leaked into dataset: %s", serialized)
	}
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
