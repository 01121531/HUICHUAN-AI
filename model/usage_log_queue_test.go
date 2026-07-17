package model

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestDeferredUsageLogIsSubmittedOnlyAfterFlush(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	before := GetUsageLogQueueStatus().Submitted

	deferUsageLog(context, usageLogJob{})
	assert.Equal(t, before, GetUsageLogQueueStatus().Submitted)

	FlushDeferredUsageLogs(context)
	assert.Eventually(t, func() bool {
		return GetUsageLogQueueStatus().Submitted == before+1
	}, time.Second, time.Millisecond)
}

func BenchmarkCloneUsageLogMap(b *testing.B) {
	source := map[string]interface{}{
		"model_ratio": 1.0,
		"admin_info": map[string]interface{}{
			"use_channel": []int{1, 2},
		},
		"request_conversion": []string{"OpenAI", "Claude"},
	}
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		_ = cloneUsageLogMap(source)
	}
}
