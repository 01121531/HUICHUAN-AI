package datasetcapture

import (
	"bytes"
	"io"
	"net/http"
	"testing"
)

func TestSessionUsesFinalSuccessfulAttempt(t *testing.T) {
	request := []byte(`{"model":"client-model","conversation_id":"conversation-1","messages":[{"role":"user","content":"hello"}]}`)
	session := NewSession(testCapture("/v1/chat/completions", request, nil))

	session.BeginAttempt("failed-model", "failed-route")
	session.FailAttempt()

	session.BeginAttempt("final-model", "final-route")
	upstreamRequest, err := http.NewRequest(http.MethodPost, "https://example.test/v1/chat/completions", bytes.NewReader([]byte(`{"model":"mapped-model","messages":[{"role":"user","content":"effective"}]}`)))
	if err != nil {
		t.Fatal(err)
	}
	if err := session.CaptureUpstreamRequest(upstreamRequest); err != nil {
		t.Fatal(err)
	}
	responseData := []byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	upstreamResponse := &http.Response{Body: io.NopCloser(bytes.NewReader(responseData))}
	session.WrapUpstreamResponse(upstreamResponse)
	if _, err := io.ReadAll(upstreamResponse.Body); err != nil {
		t.Fatal(err)
	}
	_ = upstreamResponse.Body.Close()
	session.SucceedAttempt()

	record, err := session.BuildRecord()
	if err != nil {
		t.Fatal(err)
	}
	if record.Model != "mapped-model" || record.Meta.SourceRoute != "final-route" || record.Meta.UserQuery != "effective" {
		t.Fatalf("unexpected final attempt: %#v", record)
	}
	expectedCapture := testCapture("/v1/chat/completions", request, responseData)
	expected, err := Normalize(expectedCapture)
	if err != nil {
		t.Fatal(err)
	}
	if record.SessionID != expected.SessionID {
		t.Fatalf("client conversation id was not preserved: got %s want %s", record.SessionID, expected.SessionID)
	}
}

func TestResponseCloseRecognizesTerminalSSE(t *testing.T) {
	request := []byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}]}`)
	session := NewSession(testCapture("/v1/chat/completions", request, nil))
	session.BeginAttempt("", "route")
	responseData := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	response := &http.Response{Body: io.NopCloser(bytes.NewReader(responseData))}
	session.WrapUpstreamResponse(response)
	buffer := make([]byte, len(responseData))
	if _, err := response.Body.Read(buffer); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	session.SucceedAttempt()
	if _, err := session.BuildRecord(); err != nil {
		t.Fatalf("terminal SSE should be complete: %v", err)
	}
}

func TestSessionRejectsFailedAttempt(t *testing.T) {
	request := []byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}]}`)
	session := NewSession(testCapture("/v1/chat/completions", request, nil))
	session.BeginAttempt("", "route")
	session.SetClientResponse([]byte(`{"choices":[]}`), true)
	session.FailAttempt()
	if _, err := session.BuildRecord(); err == nil {
		t.Fatal("failed attempt must not produce a record")
	}
}
