package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

type orderedFlushRecorder struct {
	*httptest.ResponseRecorder
	events *[]string
}

func (recorder *orderedFlushRecorder) Write(data []byte) (int, error) {
	*recorder.events = append(*recorder.events, "write")
	return recorder.ResponseRecorder.Write(data)
}

func (recorder *orderedFlushRecorder) Flush() {
	*recorder.events = append(*recorder.events, "flush")
	recorder.ResponseRecorder.Flush()
}

func TestUsageLogAfterResponseFlushesBeforeSubmitting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	events := []string{}
	recorder := &orderedFlushRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		events:           &events,
	}
	originalFlush := flushDeferredUsageLogs
	flushDeferredUsageLogs = func(*gin.Context) {
		events = append(events, "submit")
	}
	t.Cleanup(func() { flushDeferredUsageLogs = originalFlush })

	router := gin.New()
	router.Use(UsageLogAfterResponse())
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/test", nil))

	assert.Equal(t, []string{"write", "flush", "submit"}, events)
}
