package middleware

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/constant"
	"github.com/01121531/HUICHUAN-AI/model"
	"github.com/01121531/HUICHUAN-AI/pkg/datasetcapture"
	"github.com/01121531/HUICHUAN-AI/setting/dataset_capture_setting"
	"github.com/gin-gonic/gin"
)

const datasetCaptureWriterKey = "dataset_capture_writer"

var (
	datasetWriterMu  sync.Mutex
	datasetWriter    *managedDatasetWriter
	datasetRetired   = make(map[*managedDatasetWriter]struct{})
	datasetAlertOnce sync.Once
	datasetAlerts    *datasetcapture.AlertManager
)

type managedDatasetWriter struct {
	writer *datasetcapture.Writer

	mu        sync.Mutex
	active    int
	retired   bool
	closeOnce sync.Once
	closed    chan struct{}
}

func newManagedDatasetWriter(writer *datasetcapture.Writer) *managedDatasetWriter {
	return &managedDatasetWriter{writer: writer, closed: make(chan struct{})}
}

func (m *managedDatasetWriter) acquire() (*datasetcapture.Writer, func()) {
	if m == nil || m.writer == nil {
		return nil, func() {}
	}
	m.mu.Lock()
	if m.retired {
		m.mu.Unlock()
		return nil, func() {}
	}
	m.active++
	m.mu.Unlock()
	var once sync.Once
	return m.writer, func() {
		once.Do(m.release)
	}
}

func (m *managedDatasetWriter) release() {
	m.mu.Lock()
	if m.active > 0 {
		m.active--
	}
	shouldClose := m.retired && m.active == 0
	m.mu.Unlock()
	if shouldClose {
		m.closeAsync()
	}
}

func (m *managedDatasetWriter) retire() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.retired = true
	shouldClose := m.active == 0
	m.mu.Unlock()
	if shouldClose {
		m.closeAsync()
	}
}

func (m *managedDatasetWriter) closeAsync() {
	m.closeOnce.Do(func() {
		go func() {
			_ = m.writer.Close()
			close(m.closed)
			datasetWriterMu.Lock()
			delete(datasetRetired, m)
			datasetWriterMu.Unlock()
		}()
	})
}

type DatasetCaptureRuntimeStatus struct {
	Enabled           bool                        `json:"enabled"`
	WriterInitialized bool                        `json:"writer_initialized"`
	Node              string                      `json:"node"`
	Writer            datasetcapture.WriterStatus `json:"writer"`
	Alerts            datasetcapture.AlertStatus  `json:"alerts"`
}

type datasetResponseWriter struct {
	gin.ResponseWriter
	buffer     *datasetcapture.SegmentedBuffer
	writeErr   error
	captureErr error
}

func (w *datasetResponseWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	if n > 0 && w.captureErr == nil {
		w.captureErr = w.buffer.TryAppend(data[:n])
	}
	if err != nil && w.writeErr == nil {
		w.writeErr = err
	} else if n != len(data) && w.writeErr == nil {
		w.writeErr = io.ErrShortWrite
	}
	return n, err
}

func (w *datasetResponseWriter) WriteString(data string) (int, error) {
	n, err := w.ResponseWriter.WriteString(data)
	if n > 0 && w.captureErr == nil {
		w.captureErr = w.buffer.TryAppendString(data[:n])
	}
	if err != nil && w.writeErr == nil {
		w.writeErr = err
	} else if n != len(data) && w.writeErr == nil {
		w.writeErr = io.ErrShortWrite
	}
	return n, err
}

func DatasetCapture() gin.HandlerFunc {
	return datasetCaptureWithPolicyAndLease(acquireDatasetWriter, nil)
}

func datasetCaptureWithWriter(writerProvider func() *datasetcapture.Writer) gin.HandlerFunc {
	return datasetCaptureWithPolicy(writerProvider, func() dataset_capture_setting.Policy {
		policy := dataset_capture_setting.DefaultPolicy()
		policy.Enabled = true
		return policy
	})
}

type datasetWriterLeaseProvider func() (*datasetcapture.Writer, func())

func datasetCaptureWithLease(
	writerProvider datasetWriterLeaseProvider,
	policyProvider func() dataset_capture_setting.Policy,
) gin.HandlerFunc {
	return datasetCaptureWithPolicyAndLease(writerProvider, policyProvider)
}

func datasetCaptureWithPolicy(
	writerProvider func() *datasetcapture.Writer,
	policyProvider func() dataset_capture_setting.Policy,
) gin.HandlerFunc {
	return datasetCaptureWithPolicyAndLease(func() (*datasetcapture.Writer, func()) {
		return writerProvider(), func() {}
	}, policyProvider)
}

func datasetCaptureWithPolicyAndLease(
	writerProvider datasetWriterLeaseProvider,
	policyProvider func() dataset_capture_setting.Policy,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		var policy dataset_capture_setting.Policy
		if policyProvider != nil {
			policy = policyProvider()
		}
		if (policyProvider == nil && !dataset_capture_setting.IsEnabled()) ||
			(policyProvider != nil && !policy.Enabled) ||
			!datasetcapture.IsSupportedPath(c.Request.URL.Path) {
			c.Next()
			return
		}

		requestBody, err := reusableRequestBody(c)
		if err != nil {
			common.SysError("dataset capture could not read request body: " + err.Error())
			c.Next()
			return
		}
		requestMetadata, err := datasetcapture.InspectRequest(c.Request.URL.Path, requestBody)
		requestedModel := requestMetadata.Model
		userID := common.GetContextKeyInt(c, constant.ContextKeyUserId)
		if userID <= 0 {
			userID = c.GetInt("id")
		}
		tokenID := c.GetInt("token_id")
		stream := requestMetadata.Stream
		allowed := false
		preserveMultimodalBase64 := policy.PreserveMultimodalBase64
		if policyProvider == nil {
			allowed, preserveMultimodalBase64 = dataset_capture_setting.RequestCaptureOptions(requestedModel, userID, tokenID, stream)
		} else {
			allowed = policyAllowsRequest(policy, requestedModel, userID, tokenID, stream)
		}
		if err != nil || !allowed {
			c.Next()
			return
		}
		writer, releaseWriter := writerProvider()
		if writer == nil {
			releaseWriter()
			c.Next()
			return
		}
		defer releaseWriter()
		responseBuffer := writer.NewResponseBuffer()
		defer func() {
			responseBuffer.Release()
		}()

		startedAt := time.Now()
		session := datasetcapture.NewSession(datasetcapture.Capture{
			RequestBody:           requestBody,
			Path:                  c.Request.URL.Path,
			RequestID:             c.GetString(common.RequestIdKey),
			UserAgent:             c.Request.UserAgent(),
			CWD:                   firstCaptureValue(c.GetHeader("X-Client-Cwd"), c.GetHeader("X-Cwd")),
			HMACKey:               datasetCaptureHMACKey(),
			CreatedAt:             startedAt,
			StripMultimodalBase64: !preserveMultimodalBase64,
		})
		writer.PrepareSession(session)
		c.Request = c.Request.WithContext(datasetcapture.WithSession(c.Request.Context(), session))
		capturingWriter := &datasetResponseWriter{ResponseWriter: c.Writer, buffer: responseBuffer}
		c.Writer = capturingWriter
		c.Set(datasetCaptureWriterKey, writer)

		c.Next()

		if c.Request.Context().Err() != nil || capturingWriter.writeErr != nil || capturingWriter.captureErr != nil || c.Writer.Status() < 200 || c.Writer.Status() >= 300 {
			if capturingWriter.captureErr != nil {
				writer.ReportCaptureDrop(capturingWriter.captureErr)
			}
			return
		}
		writer.ReportCaptureHealthy()
		userIDValue := ""
		if userID > 0 {
			userIDValue = strconv.Itoa(userID)
		}
		tokenIDValue := ""
		if tokenID := c.GetInt("token_id"); tokenID > 0 {
			tokenIDValue = strconv.Itoa(tokenID)
		} else if strings.HasPrefix(c.Request.URL.Path, "/pg/") {
			tokenIDValue = "playground"
		}
		session.UpdateMetadata(
			userIDValue,
			tokenIDValue,
			common.GetContextKeyString(c, constant.ContextKeyOriginalModel),
			c.GetString("channel_name"),
		)
		session.UpdateStorageMetadata(
			userIDValue,
			tokenIDValue,
			common.GetContextKeyString(c, constant.ContextKeyUsingGroup),
			requestedModel,
			c.GetInt("channel_id"),
		)
		session.SetClientResponseBuffer(responseBuffer, true)
		responseBuffer = nil
		if err := writer.SubmitSession(session); err != nil {
			common.SysError("dataset capture submit failed: " + err.Error())
		}
	}
}

func policyAllowsRequest(policy dataset_capture_setting.Policy, model string, userID, tokenID int, stream bool) bool {
	if stream && !policy.CaptureStream {
		return false
	}
	if policy.ModelMode == dataset_capture_setting.ModelModeAll {
		// Continue with user and token scope checks.
	} else {
		allowed := false
		for _, candidate := range policy.Models {
			if strings.TrimSpace(candidate) == model {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}
	if policy.UserMode == dataset_capture_setting.ScopeModeSelected && !containsCaptureID(policy.UserIDs, userID) {
		return false
	}
	if policy.TokenMode == dataset_capture_setting.ScopeModeSelected && !containsCaptureID(policy.TokenIDs, tokenID) {
		return false
	}
	return true
}

func containsCaptureID(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func CloseDatasetCapture() {
	timeoutSeconds := common.GetEnvOrDefault("DATASET_CAPTURE_SHUTDOWN_TIMEOUT_SECONDS", 30)
	if timeoutSeconds < 1 {
		timeoutSeconds = 1
	}
	if !closeDatasetCaptureWithin(time.Duration(timeoutSeconds) * time.Second) {
		common.SysError("dataset capture shutdown timed out; unfinished snapshots may be dropped")
	}
}

func closeDatasetCaptureWithin(timeout time.Duration) bool {
	datasetWriterMu.Lock()
	writers := make([]*managedDatasetWriter, 0, len(datasetRetired)+1)
	if datasetWriter != nil {
		writers = append(writers, datasetWriter)
		datasetWriter = nil
	}
	for writer := range datasetRetired {
		writers = append(writers, writer)
	}
	datasetWriterMu.Unlock()
	for _, writer := range writers {
		writer.retire()
	}
	if len(writers) == 0 {
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for _, writer := range writers {
		select {
		case <-writer.closed:
		case <-timer.C:
			return false
		}
	}
	return true
}

func ReloadDatasetCapture() {
	configureDatasetCaptureAlerts()
	var replacement *managedDatasetWriter
	if dataset_capture_setting.IsEnabled() {
		writer := newDatasetWriter()
		if writer == nil {
			return
		}
		replacement = newManagedDatasetWriter(writer)
	}
	datasetWriterMu.Lock()
	previous := datasetWriter
	datasetWriter = replacement
	if previous != nil {
		datasetRetired[previous] = struct{}{}
	}
	datasetWriterMu.Unlock()
	if previous != nil {
		previous.retire()
	}
}

func GetDatasetCaptureRuntimeStatus() DatasetCaptureRuntimeStatus {
	configureDatasetCaptureAlerts()
	datasetWriterMu.Lock()
	managed := datasetWriter
	status := DatasetCaptureRuntimeStatus{
		Enabled: dataset_capture_setting.IsEnabled(), WriterInitialized: managed != nil,
		Node: DatasetCaptureNode(), Alerts: datasetCaptureAlertManager().Status(),
	}
	if managed != nil {
		status.Writer = managed.writer.Status()
	}
	datasetWriterMu.Unlock()
	return status
}

func SendDatasetCaptureTestAlert() bool {
	configureDatasetCaptureAlerts()
	return datasetCaptureAlertManager().SendTest()
}

func acquireDatasetWriter() (*datasetcapture.Writer, func()) {
	if !dataset_capture_setting.IsEnabled() {
		return nil, func() {}
	}
	datasetWriterMu.Lock()
	if datasetWriter != nil {
		writer, release := datasetWriter.acquire()
		datasetWriterMu.Unlock()
		return writer, release
	}
	datasetWriterMu.Unlock()

	created := newDatasetWriter()
	if created == nil {
		return nil, func() {}
	}
	candidate := newManagedDatasetWriter(created)
	datasetWriterMu.Lock()
	if datasetWriter == nil {
		datasetWriter = candidate
	} else {
		datasetRetired[candidate] = struct{}{}
		candidate.retire()
	}
	writer, release := datasetWriter.acquire()
	datasetWriterMu.Unlock()
	return writer, release
}

func newDatasetWriter() *datasetcapture.Writer {
	path := DatasetCapturePathTemplate()
	node := DatasetCaptureNode()
	performance := dataset_capture_setting.Get().Performance
	configureDatasetCaptureAlerts()
	writer, err := datasetcapture.NewWriter(datasetcapture.WriterConfig{
		PathTemplate:        path,
		Node:                node,
		QueueSize:           performance.QueueSize,
		Workers:             performance.Workers,
		IndexQueueSize:      performance.IndexQueueSize,
		IndexBatchSize:      performance.IndexBatchSize,
		IndexFlushInterval:  time.Duration(performance.IndexFlushIntervalMS) * time.Millisecond,
		SegmentSize:         performance.BufferSegmentKB << 10,
		MaxSampleBytes:      int64(performance.MaxSampleMB) << 20,
		MaxInFlightBytes:    int64(performance.MaxInFlightMB) << 20,
		SpoolThresholdBytes: int64(performance.SpoolThresholdMB) << 20,
		MaxDiskBytes:        int64(performance.MaxDiskGB) << 30,
		MinFreeDiskBytes:    int64(performance.MinFreeDiskGB) << 30,
		Partitioned:         true,
		OnError: func(err error) {
			common.SysError("dataset capture writer: " + err.Error())
		},
		OnEvent: datasetCaptureAlertManager().Notify,
		OnWrittenBatch: func(results []datasetcapture.WriteResult) error {
			indices := make([]model.DatasetCaptureIndex, 0, len(results))
			for _, result := range results {
				indices = append(indices, model.NewDatasetCaptureIndex(result))
			}
			return model.UpsertDatasetCaptureIndices(indices)
		},
	})
	if err != nil {
		common.SysError("dataset capture initialization failed: " + err.Error())
		return nil
	}
	common.SysLog(fmt.Sprintf("dataset capture enabled: path=%s queue_size=%d workers=%d max_disk_gb=%d", path, performance.QueueSize, performance.Workers, performance.MaxDiskGB))
	return writer
}

func datasetCaptureAlertManager() *datasetcapture.AlertManager {
	datasetAlertOnce.Do(func() {
		datasetAlerts = datasetcapture.NewAlertManager(func(notification datasetcapture.AlertNotification) error {
			return common.SendEmail(notification.Subject, strings.Join(notification.Recipients, ";"), notification.HTML)
		})
	})
	return datasetAlerts
}

func configureDatasetCaptureAlerts() {
	policy := dataset_capture_setting.Get()
	datasetCaptureAlertManager().Update(datasetcapture.AlertConfig{
		Enabled: policy.Alerts.Enabled, Recipients: policy.Alerts.Recipients, Types: policy.Alerts.Types,
		Silence:         time.Duration(policy.Alerts.SilenceMinutes) * time.Minute,
		AlertAfterDrops: int64(policy.Alerts.AlertAfterDrops), SendRecovery: policy.Alerts.SendRecovery,
		Node: DatasetCaptureNode(), Version: common.Version,
	})
}

func DatasetCapturePathTemplate() string {
	path := strings.TrimSpace(os.Getenv("DATASET_CAPTURE_PATH"))
	if path == "" {
		path = filepath.Join(*common.LogDir, "datasets", "sample-{date}-{node}.jsonl")
	}
	return path
}

func DatasetCaptureNode() string {
	return datasetcapture.NormalizeNode(firstCaptureValue(os.Getenv("NODE_NAME"), os.Getenv("HOSTNAME"), "node"))
}

func datasetCaptureHMACKey() string {
	if key := strings.TrimSpace(os.Getenv("DATASET_CAPTURE_HMAC_KEY")); key != "" {
		return key
	}
	return common.CryptoSecret
}

func reusableRequestBody(c *gin.Context) ([]byte, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	c.Request.Body = io.NopCloser(storage)
	// memoryStorage keeps its immutable byte slice after Close, while diskStorage
	// already returns an owned allocation. The capture Session can retain either
	// without another full request-body copy.
	return body, nil
}

func firstCaptureValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
