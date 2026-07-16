package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/01121531/HUICHUAN-AI/common"
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
		responseBody := []byte(`{"choices":[{"message":{"content":"captured"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`)
		upstreamResponse := &http.Response{Body: io.NopCloser(bytes.NewReader(responseBody))}
		session.WrapUpstreamResponse(upstreamResponse)
		if _, err := io.ReadAll(upstreamResponse.Body); err != nil {
			t.Fatal(err)
		}
		if err := upstreamResponse.Body.Close(); err != nil {
			t.Fatal(err)
		}
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
