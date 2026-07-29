package controller

import (
	"strconv"
	"strings"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/service"
	"github.com/gin-gonic/gin"
)

type nervProxyLogsResponse struct {
	Events []service.NERVProxyEvent `json:"events"`
	Stats  service.NERVProxyStats   `json:"stats"`
	Limit  int                      `json:"limit"`
	Target string                   `json:"target,omitempty"`
}

type nervProxyStatsResponse struct {
	Stats service.NERVProxyStats `json:"stats"`
}

func GetNERVProxyLogs(c *gin.Context) {
	limit := parseNERVProxyLimit(c.Query("limit"))
	target := strings.TrimSpace(c.Query("target"))
	tampered, hasTamperedFilter := parseNERVProxyBool(c.Query("tampered"))
	var tamperedFilter *bool
	if hasTamperedFilter {
		tamperedFilter = &tampered
	}

	events, stats := service.NERVProxySnapshot(limit, target, tamperedFilter)
	common.ApiSuccess(c, nervProxyLogsResponse{
		Events: events,
		Stats:  stats,
		Limit:  limit,
		Target: target,
	})
}

func GetNERVProxyStats(c *gin.Context) {
	_, stats := service.NERVProxySnapshot(0, "", nil)
	common.ApiSuccess(c, nervProxyStatsResponse{Stats: stats})
}

func ClearNERVProxyLogs(c *gin.Context) {
	if err := service.ClearNERVProxyLogs(); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func parseNERVProxyLimit(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return 20
	}
	if value > 100 {
		return 100
	}
	return value
}

func parseNERVProxyBool(raw string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1", "yes", "y":
		return true, true
	case "false", "0", "no", "n":
		return false, true
	default:
		return false, false
	}
}
