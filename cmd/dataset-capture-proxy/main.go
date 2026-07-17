package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/01121531/HUICHUAN-AI/pkg/datasetcapture"
)

type proxyHandler struct {
	proxy   *httputil.ReverseProxy
	writer  *datasetcapture.Writer
	target  *url.URL
	hmacKey string
}

type proxyResponseWriter struct {
	http.ResponseWriter
	status     int
	buffer     *datasetcapture.SegmentedBuffer
	writeErr   error
	captureErr error
}

func (w *proxyResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *proxyResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(data)
	if n > 0 && w.captureErr == nil {
		w.captureErr = w.buffer.TryAppend(data[:n])
	}
	if err != nil {
		w.writeErr = err
	} else if n != len(data) {
		w.writeErr = io.ErrShortWrite
	}
	return n, err
}

func (w *proxyResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func newProxyHandler(target *url.URL, writer *datasetcapture.Writer, hmacKey string) *proxyHandler {
	handler := &proxyHandler{writer: writer, target: target, hmacKey: hmacKey}
	handler.proxy = &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.URL.Path = joinURLPath(target.Path, req.URL.Path)
			req.Host = target.Host
			req.Header.Del("Accept-Encoding")
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			log.Printf("proxy request failed: %v", err)
			http.Error(w, "upstream request failed", http.StatusBadGateway)
		},
	}
	return handler
}

func (h *proxyHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path == "/__capture/health" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !datasetcapture.IsSupportedPath(req.URL.Path) || req.Method != http.MethodPost {
		h.proxy.ServeHTTP(w, req)
		return
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(body))
	createdAt := time.Now()
	session := datasetcapture.NewSession(datasetcapture.Capture{
		RequestBody: body,
		Path:        req.URL.Path,
		RequestID:   firstValue(req.Header.Get("X-Request-Id"), req.Header.Get("X-Oneapi-Request-Id")),
		UserID:      req.Header.Get("X-Capture-User-Id"),
		UserAgent:   req.UserAgent(),
		CWD:         firstValue(req.Header.Get("X-Client-Cwd"), req.Header.Get("X-Cwd")),
		HMACKey:     h.hmacKey,
		CreatedAt:   createdAt,
		Route:       h.target.Host,
	})
	h.writer.PrepareSession(session)
	session.BeginAttempt("", h.target.Host)
	req = req.WithContext(datasetcapture.WithSession(req.Context(), session))

	responseBuffer := h.writer.NewResponseBuffer()
	defer func() {
		responseBuffer.Release()
	}()
	captureWriter := &proxyResponseWriter{ResponseWriter: w, buffer: responseBuffer}
	h.proxy.ServeHTTP(captureWriter, req)
	if captureWriter.writeErr != nil || captureWriter.captureErr != nil || captureWriter.status < 200 || captureWriter.status >= 300 {
		session.FailAttempt()
		return
	}
	session.SetClientResponseBuffer(responseBuffer, true)
	responseBuffer = nil
	session.SucceedAttempt()
	if err := h.writer.SubmitSession(session); err != nil {
		log.Printf("capture submit failed: %v", err)
	}
}

func main() {
	targetValue := strings.TrimSpace(os.Getenv("CAPTURE_PROXY_UPSTREAM"))
	if targetValue == "" {
		log.Fatal("CAPTURE_PROXY_UPSTREAM is required")
	}
	target, err := url.Parse(targetValue)
	if err != nil || target.Scheme == "" || target.Host == "" {
		log.Fatal("CAPTURE_PROXY_UPSTREAM must be an absolute HTTP(S) URL")
	}
	listen := firstValue(os.Getenv("CAPTURE_PROXY_LISTEN"), ":8080")
	output := firstValue(os.Getenv("DATASET_CAPTURE_PATH"), "./logs/datasets/sample-{date}-{node}.jsonl")
	node := firstValue(os.Getenv("NODE_NAME"), os.Getenv("HOSTNAME"), "capture-proxy")
	queueSize := envInt("DATASET_CAPTURE_QUEUE_SIZE", 128)
	workers := envInt("DATASET_CAPTURE_WORKERS", 2)
	segmentSizeKB := envInt("DATASET_CAPTURE_BUFFER_SEGMENT_KB", 64)
	maxSampleMB := envInt("DATASET_CAPTURE_MAX_SAMPLE_MB", 100)
	maxInFlightMB := envInt("DATASET_CAPTURE_MAX_INFLIGHT_MB", 512)
	spoolThresholdMB := envInt("DATASET_CAPTURE_SPOOL_THRESHOLD_MB", 2)
	maxDiskGB := envInt("DATASET_CAPTURE_MAX_DISK_GB", 10)
	minFreeDiskGB := envInt("DATASET_CAPTURE_MIN_FREE_DISK_GB", 2)
	hmacKey := os.Getenv("DATASET_CAPTURE_HMAC_KEY")
	if hmacKey == "" {
		log.Fatal("DATASET_CAPTURE_HMAC_KEY is required for the standalone proxy")
	}
	writer, err := datasetcapture.NewWriter(datasetcapture.WriterConfig{
		PathTemplate:        output,
		Node:                node,
		QueueSize:           queueSize,
		Workers:             workers,
		SegmentSize:         segmentSizeKB << 10,
		MaxSampleBytes:      int64(maxSampleMB) << 20,
		MaxInFlightBytes:    int64(maxInFlightMB) << 20,
		SpoolThresholdBytes: int64(spoolThresholdMB) << 20,
		MaxDiskBytes:        int64(maxDiskGB) << 30,
		MinFreeDiskBytes:    int64(minFreeDiskGB) << 30,
		Partitioned:         true,
		OnError: func(err error) {
			log.Printf("dataset writer: %v", err)
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer writer.Close()

	server := &http.Server{
		Addr:              listen,
		Handler:           newProxyHandler(target, writer, hmacKey),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	go func() {
		log.Printf("dataset capture proxy listening on %s -> %s", listen, target.Redacted())
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("capture proxy failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("capture proxy shutdown: %v", err)
	}
}

func joinURLPath(basePath, requestPath string) string {
	joined := path.Join("/", basePath, requestPath)
	if strings.HasSuffix(requestPath, "/") && !strings.HasSuffix(joined, "/") {
		joined += "/"
	}
	return joined
}

func firstValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		log.Printf("invalid %s=%q, using %d", name, value, fallback)
		return fallback
	}
	return parsed
}
