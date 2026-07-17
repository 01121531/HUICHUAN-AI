package middleware

import (
	"github.com/01121531/HUICHUAN-AI/model"
	"github.com/gin-gonic/gin"
)

var flushDeferredUsageLogs = model.FlushDeferredUsageLogs

// UsageLogAfterResponse delays non-financial consume/error log persistence
// until the handler has completed writing the client response.
func UsageLogAfterResponse() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if c.Writer.Written() {
			c.Writer.Flush()
		}
		flushDeferredUsageLogs(c)
	}
}
