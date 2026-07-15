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

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/datasetcapture"
	"github.com/QuantumNous/new-api/setting/dataset_capture_setting"
	"github.com/gin-gonic/gin"
)

const datasetCaptureWriterKey = "dataset_capture_writer"

var (
	datasetWriterMu sync.Mutex
	datasetWriter   *datasetcapture.Writer
)

type datasetResponseWriter struct {
	gin.ResponseWriter
	file     *os.File
	writeErr error
}

func (w *datasetResponseWriter) Write(data []byte) (int, error) {
	if w.writeErr == nil {
		_, w.writeErr = w.file.Write(data)
	}
	n, err := w.ResponseWriter.Write(data)
	if err != nil && w.writeErr == nil {
		w.writeErr = err
	}
	return n, err
}

func (w *datasetResponseWriter) WriteString(data string) (int, error) {
	if w.writeErr == nil {
		_, w.writeErr = io.WriteString(w.file, data)
	}
	n, err := w.ResponseWriter.WriteString(data)
	if err != nil && w.writeErr == nil {
		w.writeErr = err
	}
	return n, err
}

func DatasetCapture() gin.HandlerFunc {
	return datasetCaptureWithPolicy(getDatasetWriter, dataset_capture_setting.Get)
}

func datasetCaptureWithWriter(writerProvider func() *datasetcapture.Writer) gin.HandlerFunc {
	return datasetCaptureWithPolicy(writerProvider, func() dataset_capture_setting.Policy {
		return dataset_capture_setting.Policy{Enabled: true, ModelMode: dataset_capture_setting.ModelModeAll}
	})
}

func datasetCaptureWithPolicy(
	writerProvider func() *datasetcapture.Writer,
	policyProvider func() dataset_capture_setting.Policy,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		policy := policyProvider()
		if !policy.Enabled || !datasetcapture.IsSupportedPath(c.Request.URL.Path) {
			c.Next()
			return
		}

		requestBody, err := reusableRequestBody(c)
		if err != nil {
			common.SysError("dataset capture could not read request body: " + err.Error())
			c.Next()
			return
		}
		requestedModel, err := datasetcapture.RequestedModel(c.Request.URL.Path, requestBody)
		if err != nil || !policyAllowsModel(policy, requestedModel) {
			c.Next()
			return
		}
		writer := writerProvider()
		if writer == nil {
			c.Next()
			return
		}
		responseFile, err := os.CreateTemp("", "new-api-dataset-response-*.spool")
		if err != nil {
			common.SysError("dataset capture could not create response spool: " + err.Error())
			c.Next()
			return
		}
		_ = responseFile.Chmod(0o600)
		defer func() {
			_ = responseFile.Close()
			_ = os.Remove(responseFile.Name())
		}()

		startedAt := time.Now()
		session := datasetcapture.NewSession(datasetcapture.Capture{
			RequestBody: requestBody,
			Path:        c.Request.URL.Path,
			RequestID:   c.GetString(common.RequestIdKey),
			UserAgent:   c.Request.UserAgent(),
			CWD:         firstCaptureValue(c.GetHeader("X-Client-Cwd"), c.GetHeader("X-Cwd")),
			HMACKey:     datasetCaptureHMACKey(),
			CreatedAt:   startedAt,
		})
		c.Request = c.Request.WithContext(datasetcapture.WithSession(c.Request.Context(), session))
		capturingWriter := &datasetResponseWriter{ResponseWriter: c.Writer, file: responseFile}
		c.Writer = capturingWriter
		c.Set(datasetCaptureWriterKey, writer)

		c.Next()

		if capturingWriter.writeErr != nil || c.Writer.Status() < 200 || c.Writer.Status() >= 300 {
			return
		}
		if _, err := responseFile.Seek(0, io.SeekStart); err != nil {
			common.SysError("dataset capture could not rewind response spool: " + err.Error())
			return
		}
		responseBody, err := io.ReadAll(responseFile)
		if err != nil {
			common.SysError("dataset capture could not read response spool: " + err.Error())
			return
		}
		userID := common.GetContextKeyInt(c, constant.ContextKeyUserId)
		if userID <= 0 {
			userID = c.GetInt("id")
		}
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
		session.SetClientResponse(responseBody, true)
		record, err := session.BuildRecord()
		if err != nil {
			if err != datasetcapture.ErrIncompleteCapture && err != datasetcapture.ErrUnsupportedProtocol {
				common.SysError("dataset capture skipped invalid sample: " + err.Error())
			}
			return
		}
		if err := writer.Submit(record); err != nil {
			common.SysError("dataset capture submit failed: " + err.Error())
		}
	}
}

func policyAllowsModel(policy dataset_capture_setting.Policy, model string) bool {
	if policy.ModelMode == dataset_capture_setting.ModelModeAll {
		return true
	}
	for _, allowed := range policy.Models {
		if strings.TrimSpace(allowed) == model {
			return true
		}
	}
	return false
}

func CloseDatasetCapture() {
	datasetWriterMu.Lock()
	defer datasetWriterMu.Unlock()
	if datasetWriter != nil {
		_ = datasetWriter.Close()
		datasetWriter = nil
	}
}

func getDatasetWriter() *datasetcapture.Writer {
	if !dataset_capture_setting.IsEnabled() {
		return nil
	}
	datasetWriterMu.Lock()
	defer datasetWriterMu.Unlock()
	if datasetWriter != nil {
		return datasetWriter
	}
	path := DatasetCapturePathTemplate()
	node := DatasetCaptureNode()
	queueSize := common.GetEnvOrDefault("DATASET_CAPTURE_QUEUE_SIZE", 128)
	maxDiskGB := common.GetEnvOrDefault("DATASET_CAPTURE_MAX_DISK_GB", 10)
	writer, err := datasetcapture.NewWriter(datasetcapture.WriterConfig{
		PathTemplate: path,
		Node:         node,
		QueueSize:    queueSize,
		MaxDiskBytes: int64(maxDiskGB) << 30,
		Partitioned:  true,
		OnError: func(err error) {
			common.SysError("dataset capture writer: " + err.Error())
		},
		OnWritten: func(result datasetcapture.WriteResult) error {
			return model.UpsertDatasetCaptureIndex(model.NewDatasetCaptureIndex(result))
		},
	})
	if err != nil {
		common.SysError("dataset capture initialization failed: " + err.Error())
		return nil
	}
	datasetWriter = writer
	common.SysLog(fmt.Sprintf("dataset capture enabled: path=%s queue_size=%d max_disk_gb=%d", path, queueSize, maxDiskGB))
	return datasetWriter
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
	return append([]byte(nil), body...), nil
}

func firstCaptureValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
