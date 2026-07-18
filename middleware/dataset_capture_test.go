package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/constant"
	"github.com/01121531/HUICHUAN-AI/pkg/datasetcapture"
	"github.com/01121531/HUICHUAN-AI/setting/dataset_capture_setting"
	"github.com/gin-gonic/gin"
)

func TestDatasetCaptureMiddlewareWritesSuccessfulSample(t *testing.T) {
	gin.SetMode(gin.TestMode)
	output := filepath.Join(t.TempDir(), "sample.jsonl")
	writer, err := datasetcapture.NewWriter(datasetcapture.WriterConfig{PathTemplate: output, QueueSize: 4})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(BodyStorageCleanup())
	router.Use(datasetCaptureWithWriter(func() *datasetcapture.Writer { return writer }))
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set(common.RequestIdKey, "request-1")
		session := datasetcapture.FromContext(c.Request.Context())
		if session == nil {
			t.Fatal("capture session missing")
		}
		session.BeginAttempt("gpt-test", "test-route")
		session.SucceedAttempt()
		c.JSON(http.StatusOK, gin.H{
			"choices": []any{gin.H{"message": gin.H{"content": "answer"}, "finish_reason": "stop"}},
			"usage":   gin.H{"prompt_tokens": 2, "completion_tokens": 1},
		})
	})

	requestBody := []byte(`{"model":"gpt-test","messages":[{"role":"user","content":"question"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d", response.Code)
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
	if record.Meta.SourceRoute != "test-route" || record.Response.Content == nil || *record.Response.Content != "answer" {
		t.Fatalf("unexpected record: %#v", record)
	}
}

func TestDatasetCaptureMiddlewareSkipsFailedResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	output := filepath.Join(t.TempDir(), "sample.jsonl")
	writer, err := datasetcapture.NewWriter(datasetcapture.WriterConfig{PathTemplate: output})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(BodyStorageCleanup())
	router.Use(datasetCaptureWithWriter(func() *datasetcapture.Writer { return writer }))
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		session := datasetcapture.FromContext(c.Request.Context())
		session.BeginAttempt("gpt-test", "test-route")
		session.FailAttempt()
		c.JSON(http.StatusBadGateway, gin.H{"error": "upstream failed"})
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-test","messages":[{"role":"user","content":"question"}]}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	_ = writer.Close()
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("failed response unexpectedly created dataset: %v", err)
	}
}

func TestDatasetCaptureMiddlewareSkipsCanceledClient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	output := filepath.Join(t.TempDir(), "sample.jsonl")
	writer, err := datasetcapture.NewWriter(datasetcapture.WriterConfig{PathTemplate: output})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(BodyStorageCleanup())
	router.Use(datasetCaptureWithWriter(func() *datasetcapture.Writer { return writer }))
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		session := datasetcapture.FromContext(c.Request.Context())
		session.BeginAttempt("gpt-test", "route")
		session.SucceedAttempt()
		c.JSON(http.StatusOK, gin.H{
			"choices": []any{gin.H{"message": gin.H{"content": "answer"}, "finish_reason": "stop"}},
		})
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(
		`{"model":"gpt-test","messages":[{"role":"user","content":"question"}]}`,
	)).WithContext(ctx)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("canceled response unexpectedly created dataset: %v", err)
	}
}

func TestDatasetCaptureMiddlewareFiltersByRequestedSiteModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	output := filepath.Join(t.TempDir(), "sample.jsonl")
	writer, err := datasetcapture.NewWriter(datasetcapture.WriterConfig{PathTemplate: output, QueueSize: 4})
	if err != nil {
		t.Fatal(err)
	}
	policy := dataset_capture_setting.Policy{
		Enabled:   true,
		ModelMode: dataset_capture_setting.ModelModeSelected,
		Models:    []string{"site-model"},
	}
	capturedSessions := 0
	router := gin.New()
	router.Use(BodyStorageCleanup())
	router.Use(datasetCaptureWithPolicy(
		func() *datasetcapture.Writer { return writer },
		func() dataset_capture_setting.Policy { return policy },
	))
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		session := datasetcapture.FromContext(c.Request.Context())
		if session == nil {
			c.JSON(http.StatusOK, gin.H{"choices": []any{gin.H{"message": gin.H{"content": "not captured"}, "finish_reason": "stop"}}})
			return
		}
		capturedSessions++
		session.BeginAttempt("upstream-model", "mapped-route")
		upstreamRequest := httptest.NewRequest(
			http.MethodPost,
			"https://upstream.test/v1/chat/completions",
			bytes.NewBufferString(`{"model":"upstream-model","messages":[{"role":"user","content":"effective"}]}`),
		)
		if err := session.CaptureUpstreamRequest(upstreamRequest); err != nil {
			t.Fatal(err)
		}
		if _, err := io.ReadAll(upstreamRequest.Body); err != nil {
			t.Fatal(err)
		}
		if err := upstreamRequest.Body.Close(); err != nil {
			t.Fatal(err)
		}
		responseBody := []byte(`{"choices":[{"message":{"content":"captured"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`)
		session.SucceedAttempt()
		c.Data(http.StatusOK, "application/json", responseBody)
	})

	for _, modelName := range []string{"other-model", "site-model"} {
		body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"question"}]}`, modelName)
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("model %s status = %d", modelName, response.Code)
		}
	}
	if capturedSessions != 1 {
		t.Fatalf("capture sessions = %d, want 1", capturedSessions)
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
	if record.Model != "upstream-model" {
		t.Fatalf("record model = %q, want mapped upstream model", record.Model)
	}
}

func TestDatasetCaptureMiddlewareFiltersByUserTokenAndStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	output := filepath.Join(t.TempDir(), "sample.jsonl")
	writer, err := datasetcapture.NewWriter(datasetcapture.WriterConfig{PathTemplate: output, QueueSize: 8})
	if err != nil {
		t.Fatal(err)
	}
	policy := dataset_capture_setting.DefaultPolicy()
	policy.Enabled = true
	policy.UserMode = dataset_capture_setting.ScopeModeSelected
	policy.UserIDs = []int{7}
	policy.TokenMode = dataset_capture_setting.ScopeModeSelected
	policy.TokenIDs = []int{9}
	policy.CaptureStream = false
	captured := 0
	router := gin.New()
	router.Use(BodyStorageCleanup())
	router.Use(func(c *gin.Context) {
		c.Set(string(constant.ContextKeyUserId), 7)
		c.Set("token_id", 9)
		c.Next()
	})
	router.Use(datasetCaptureWithPolicy(
		func() *datasetcapture.Writer { return writer },
		func() dataset_capture_setting.Policy { return policy },
	))
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		session := datasetcapture.FromContext(c.Request.Context())
		if session != nil {
			captured++
			session.BeginAttempt("gpt-test", "route")
			session.SucceedAttempt()
		}
		c.JSON(http.StatusOK, gin.H{"choices": []any{gin.H{"message": gin.H{"content": "ok"}, "finish_reason": "stop"}}})
	})

	for _, stream := range []bool{true, false} {
		body := fmt.Sprintf(`{"model":"gpt-test","stream":%t,"messages":[{"role":"user","content":"hello"}]}`, stream)
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("stream=%t status=%d", stream, response.Code)
		}
	}
	if captured != 1 {
		t.Fatalf("captured=%d, want 1 non-stream request", captured)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}

type datasetResponseOrderProbe struct {
	gin.ResponseWriter
	buffer      *datasetcapture.SegmentedBuffer
	observedLen int64
	flushLen    int64
}

func (w *datasetResponseOrderProbe) Write(data []byte) (int, error) {
	w.observedLen = w.buffer.Len()
	return w.ResponseWriter.Write(data)
}

func (w *datasetResponseOrderProbe) Flush() {
	w.flushLen = w.buffer.Len()
	w.ResponseWriter.Flush()
}

func TestDatasetResponseWriterDeliversBeforeCapture(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	pool := datasetcapture.NewBufferPool(4, 16)
	buffer := pool.NewBuffer(16)
	probe := &datasetResponseOrderProbe{ResponseWriter: context.Writer, buffer: buffer}
	writer := &datasetResponseWriter{ResponseWriter: probe, buffer: buffer}

	n, err := writer.Write([]byte("answer"))
	if err != nil {
		t.Fatal(err)
	}
	if n != len("answer") || recorder.Body.String() != "answer" {
		t.Fatalf("client received n=%d body=%q", n, recorder.Body.String())
	}
	if probe.observedLen != 0 {
		t.Fatalf("capture buffer contained %d bytes before client write", probe.observedLen)
	}
	if buffer.Len() != int64(len("answer")) {
		t.Fatalf("capture buffer len=%d", buffer.Len())
	}
	buffer.Release()
}

func TestDatasetResponseWriterFlushesAfterCaptureAppend(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	buffer := datasetcapture.NewBufferPool(4, 16).NewBuffer(16)
	probe := &datasetResponseOrderProbe{ResponseWriter: context.Writer, buffer: buffer}
	writer := &datasetResponseWriter{ResponseWriter: probe, buffer: buffer}

	if _, err := writer.Write([]byte("answer")); err != nil {
		t.Fatal(err)
	}
	writer.Flush()
	if probe.flushLen != int64(len("answer")) {
		t.Fatalf("flush observed buffer len=%d, want %d", probe.flushLen, len("answer"))
	}
	if recorder.Body.String() != "answer" {
		t.Fatalf("client body=%q", recorder.Body.String())
	}
	buffer.Release()
}

func TestDatasetResponseWriterCaptureLimitDoesNotFailClientWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	buffer := datasetcapture.NewBufferPool(4, 4).NewBuffer(1)
	writer := &datasetResponseWriter{ResponseWriter: context.Writer, buffer: buffer}

	n, err := writer.Write([]byte("ok"))
	if err != nil || n != 2 {
		t.Fatalf("client write n=%d err=%v", n, err)
	}
	if recorder.Body.String() != "ok" {
		t.Fatalf("client body=%q", recorder.Body.String())
	}
	if !errors.Is(writer.captureErr, datasetcapture.ErrSampleTooLarge) {
		t.Fatalf("capture error=%v", writer.captureErr)
	}
	buffer.Release()
}

func TestManagedDatasetWriterRetiresAfterActiveLease(t *testing.T) {
	writer, err := datasetcapture.NewWriter(datasetcapture.WriterConfig{
		PathTemplate: filepath.Join(t.TempDir(), "sample.jsonl"),
		QueueSize:    4,
	})
	if err != nil {
		t.Fatal(err)
	}
	managed := newManagedDatasetWriter(writer)
	acquired, release := managed.acquire()
	if acquired != writer {
		t.Fatal("managed writer lease did not return the active writer")
	}
	managed.retire()
	select {
	case <-managed.closed:
		t.Fatal("writer closed while an active request still held a lease")
	case <-time.After(20 * time.Millisecond):
	}
	release()
	select {
	case <-managed.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("retired writer did not close after its final lease was released")
	}
}
