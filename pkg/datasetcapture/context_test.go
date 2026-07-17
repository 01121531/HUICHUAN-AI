package datasetcapture

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionUsesFinalSuccessfulAttempt(t *testing.T) {
	request := []byte(`{"model":"client-model","conversation_id":"conversation-1","messages":[{"role":"user","content":"hello"}]}`)
	session := NewSession(testCapture("/v1/chat/completions", request, nil))
	prepareTestRequestCapture(session, 1<<20, 4<<20)

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
	if _, err := io.ReadAll(upstreamRequest.Body); err != nil {
		t.Fatal(err)
	}
	if err := upstreamRequest.Body.Close(); err != nil {
		t.Fatal(err)
	}
	responseData := []byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	session.SetClientResponse(responseData, true)
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

func TestSessionAcceptsCompleteClientSSE(t *testing.T) {
	request := []byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}]}`)
	session := NewSession(testCapture("/v1/chat/completions", request, nil))
	session.BeginAttempt("", "route")
	responseData := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	session.SetClientResponse(responseData, true)
	session.SucceedAttempt()
	if _, err := session.BuildRecord(); err != nil {
		t.Fatalf("terminal SSE should be complete: %v", err)
	}
}

func TestSessionNormalizesEffectiveRequestAndClientResponseProtocols(t *testing.T) {
	clientRequest := []byte(`{"model":"client-model","messages":[{"role":"user","content":"client input"}]}`)
	session := NewSession(testCapture("/v1/chat/completions", clientRequest, nil))
	prepareTestRequestCapture(session, 1<<20, 4<<20)
	session.BeginAttempt("claude-effective", "anthropic-route")
	upstreamRequest, err := http.NewRequest(http.MethodPost, "https://example.test/v1/messages", bytes.NewReader([]byte(`{"model":"claude-effective","system":"effective system","messages":[{"role":"user","content":"effective input"}]}`)))
	if err != nil {
		t.Fatal(err)
	}
	if err := session.CaptureUpstreamRequest(upstreamRequest); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(upstreamRequest.Body); err != nil {
		t.Fatal(err)
	}
	if err := upstreamRequest.Body.Close(); err != nil {
		t.Fatal(err)
	}
	session.SetClientResponse([]byte(`{"choices":[{"message":{"content":"client-visible result"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`), true)
	session.SucceedAttempt()

	record, err := session.BuildRecord()
	if err != nil {
		t.Fatal(err)
	}
	if record.Model != "claude-effective" || record.SystemPrompt != "effective system" || record.Meta.UserQuery != "effective input" {
		t.Fatalf("effective request semantics were not preserved: %#v", record)
	}
	if record.Response.Content == nil || *record.Response.Content != "client-visible result" {
		t.Fatalf("client response protocol was not normalized: %#v", record.Response)
	}
}

type countingReadCloser struct {
	reader io.Reader
	reads  int
}

func (r *countingReadCloser) Read(p []byte) (int, error) {
	r.reads++
	return r.reader.Read(p)
}

func (r *countingReadCloser) Close() error { return nil }

func TestCaptureUpstreamRequestDoesNotPreReadBody(t *testing.T) {
	original := []byte(`{"model":"gpt-test","messages":[]}`)
	session := NewSession(testCapture("/v1/chat/completions", original, nil))
	prepareTestRequestCapture(session, 1<<20, 4<<20)
	session.BeginAttempt("", "route")
	req, err := http.NewRequest(http.MethodPost, "https://example.test/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	source := &countingReadCloser{reader: bytes.NewReader(original)}
	req.Body = source
	req.ContentLength = int64(len(original))
	if err := session.CaptureUpstreamRequest(req); err != nil {
		t.Fatal(err)
	}
	if source.reads != 0 {
		t.Fatalf("CaptureUpstreamRequest pre-read the body %d times", source.reads)
	}
	transmitted, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := req.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(transmitted, original) {
		t.Fatalf("upstream body changed by capture: got %q want %q", transmitted, original)
	}
}

func TestRequestCaptureLimitDoesNotChangeUpstreamRead(t *testing.T) {
	original := []byte(`{"model":"gpt-test","messages":[]}`)
	session := NewSession(testCapture("/v1/chat/completions", original, nil))
	pool := prepareTestRequestCapture(session, 4, 4)
	session.BeginAttempt("", "route")
	req, err := http.NewRequest(http.MethodPost, "https://example.test/v1/chat/completions", bytes.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	if err := session.CaptureUpstreamRequest(req); err != nil {
		t.Fatal(err)
	}
	transmitted, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("capture limit changed upstream read result: %v", err)
	}
	_ = req.Body.Close()
	if !bytes.Equal(transmitted, original) {
		t.Fatalf("capture limit changed upstream bytes: got %q want %q", transmitted, original)
	}
	if err := session.reserveRetainedMemory(pool, 64); !errors.Is(err, ErrSampleTooLarge) && !errors.Is(err, ErrInFlightLimitReached) {
		t.Fatalf("reservation error=%v, want captured request capacity error", err)
	}
	session.SetClientResponse([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`), true)
	session.SucceedAttempt()
	if _, err := session.BuildRecord(); !errors.Is(err, ErrSampleTooLarge) && !errors.Is(err, ErrInFlightLimitReached) {
		t.Fatalf("BuildRecord error=%v, want capture capacity error", err)
	}
}

func prepareTestRequestCapture(session *Session, maxSampleBytes, maxInFlightBytes int64) *BufferPool {
	pool := NewBufferPool(4, maxInFlightBytes)
	session.SetRequestBufferFactory(func() *SegmentedBuffer {
		return pool.NewBuffer(maxSampleBytes)
	})
	return pool
}

func TestSessionReservationCountsReferencedRequestMemory(t *testing.T) {
	request := []byte("12345678")
	pool := NewBufferPool(4, 10)
	session := NewSession(Capture{RequestBody: request})
	response := pool.NewBuffer(16)
	if err := response.TryAppendString("ok"); err != nil {
		t.Fatal(err)
	}
	session.SetClientResponseBuffer(response, true)
	if err := session.reserveRetainedMemory(pool, 16); !errors.Is(err, ErrInFlightLimitReached) {
		t.Fatalf("reservation error=%v, want ErrInFlightLimitReached", err)
	}
	session.Release()
	if pool.InFlightBytes() != 0 {
		t.Fatalf("reservation failure leaked %d in-flight bytes", pool.InFlightBytes())
	}
	if pool.droppedInFlightLimit.Load() != 1 {
		t.Fatalf("in-flight drop count=%d, want 1", pool.droppedInFlightLimit.Load())
	}
}

func TestSessionReservationEnforcesCombinedSampleLimit(t *testing.T) {
	pool := NewBufferPool(4, 64)
	session := NewSession(Capture{RequestBody: []byte("12345678")})
	response := pool.NewBuffer(16)
	if err := response.TryAppendString("ok"); err != nil {
		t.Fatal(err)
	}
	session.SetClientResponseBuffer(response, true)
	if err := session.reserveRetainedMemory(pool, 9); !errors.Is(err, ErrSampleTooLarge) {
		t.Fatalf("reservation error=%v, want ErrSampleTooLarge", err)
	}
	session.Release()
	if pool.InFlightBytes() != 0 {
		t.Fatalf("sample-limit failure leaked %d in-flight bytes", pool.InFlightBytes())
	}
}

func TestSessionReservationReleasesSuccessfulReservation(t *testing.T) {
	pool := NewBufferPool(4, 64)
	session := NewSession(Capture{RequestBody: []byte("12345678")})
	response := pool.NewBuffer(16)
	if err := response.TryAppendString("ok"); err != nil {
		t.Fatal(err)
	}
	session.SetClientResponseBuffer(response, true)
	if err := session.reserveRetainedMemory(pool, 16); err != nil {
		t.Fatal(err)
	}
	if got := pool.InFlightBytes(); got != 12 {
		t.Fatalf("in-flight bytes=%d, want 12", got)
	}
	session.Release()
	if pool.InFlightBytes() != 0 {
		t.Fatalf("successful reservation leaked %d in-flight bytes", pool.InFlightBytes())
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

func TestMaterializeClientResponseSpoolsAndReleasesSegments(t *testing.T) {
	pool := NewBufferPool(4, 16)
	buffer := pool.NewBuffer(16)
	if err := buffer.TryAppendString("response"); err != nil {
		t.Fatal(err)
	}
	data, err := materializeClientResponse(buffer, 1)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "response" {
		t.Fatalf("materialized response=%q", data)
	}
	if pool.InFlightBytes() != 0 {
		t.Fatalf("spooled buffer retained %d bytes", pool.InFlightBytes())
	}
}

func TestMaterializeClientResponseClassifiesSpoolFailure(t *testing.T) {
	invalidTempDir := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(invalidTempDir, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", invalidTempDir)
	t.Setenv("TMP", invalidTempDir)
	t.Setenv("TEMP", invalidTempDir)
	pool := NewBufferPool(4, 16)
	buffer := pool.NewBuffer(16)
	defer buffer.Release()
	if err := buffer.TryAppendString("response"); err != nil {
		t.Fatal(err)
	}
	_, err := materializeClientResponse(buffer, 1)
	if !errors.Is(err, ErrSpoolWriteFailed) {
		t.Fatalf("spool error=%v", err)
	}
}
