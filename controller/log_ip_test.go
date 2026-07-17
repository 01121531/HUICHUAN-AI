package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/01121531/HUICHUAN-AI/model"
	"github.com/gin-gonic/gin"
)

func TestUsageLogIPAccessNeverMasksValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET", "/api/log/", nil)
	logs := []*model.Log{
		{Ip: "192.168.31.102"},
		{Ip: "2001:db8:abcd:1234:5678:90ab:cdef:1"},
		{Ip: "invalid-history"},
	}

	auditUsageLogIPAccess(context, logs, "all")
	if logs[0].Ip != "192.168.31.102" {
		t.Fatalf("IPv4 was unexpectedly masked: %q", logs[0].Ip)
	}
	if logs[1].Ip != "2001:db8:abcd:1234:5678:90ab:cdef:1" {
		t.Fatalf("IPv6 was unexpectedly masked: %q", logs[1].Ip)
	}
	if logs[2].Ip != "invalid-history" {
		t.Fatalf("historical IP value was unexpectedly changed: %q", logs[2].Ip)
	}
}

func TestUsageLogIPAccessIgnoresLegacyMaskedParameter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET", "/api/log/self?ip_visibility=masked", nil)
	logs := []*model.Log{{Ip: "203.0.113.9"}}

	auditUsageLogIPAccess(context, logs, "self")
	if logs[0].Ip != "203.0.113.9" {
		t.Fatalf("legacy masked parameter changed the IP: %q", logs[0].Ip)
	}
}
