package middleware

import (
	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/gin-gonic/gin"
)

func UsageLogClientIP() gin.HandlerFunc {
	return func(c *gin.Context) {
		if common.UsageLogIPCaptureEnabled.Load() {
			c.Set(common.UsageLogClientIPKey, common.ResolveUsageLogClientIP(c.Request))
		}
		c.Next()
	}
}
