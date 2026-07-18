package common

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveUsageLogClientIP(t *testing.T) {
	t.Cleanup(func() { SetUsageLogTrustedProxyCIDRs("") })

	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "203.0.113.9:1234"
	request.Header.Set("X-Forwarded-For", "198.51.100.7")
	assert.Equal(t, "203.0.113.9", ResolveUsageLogClientIP(request))

	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("X-Forwarded-For", "198.51.100.8")
	assert.Equal(t, "198.51.100.8", ResolveUsageLogClientIP(request))

	require.Empty(t, SetUsageLogTrustedProxyCIDRs("10.0.0.0/8, 192.168.0.0/16"))
	request.RemoteAddr = "10.0.0.2:1234"
	request.Header.Set("X-Forwarded-For", "198.51.100.7, 192.168.1.2")
	assert.Equal(t, "198.51.100.7", ResolveUsageLogClientIP(request))

	request.Header.Set("X-Forwarded-For", "not-an-ip")
	request.Header.Set("X-Real-IP", "2001:db8::7")
	assert.Equal(t, "2001:db8::7", ResolveUsageLogClientIP(request))
}

func TestResolveUsageLogClientIPSupportsForwardedHeader(t *testing.T) {
	t.Cleanup(func() { SetUsageLogTrustedProxyCIDRs("") })

	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "[::1]:1234"
	request.Header.Set("Forwarded", `for=198.51.100.9;proto=https, for=127.0.0.1`)
	assert.Equal(t, "198.51.100.9", ResolveUsageLogClientIP(request))
}

func TestResolveUsageLogClientIPSupportsCommonCDNHeaders(t *testing.T) {
	t.Cleanup(func() { SetUsageLogTrustedProxyCIDRs("") })

	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("CF-Connecting-IP", "198.51.100.10")
	assert.Equal(t, "198.51.100.10", ResolveUsageLogClientIP(request))
}

func TestSetUsageLogTrustedProxyCIDRsRejectsInvalidValues(t *testing.T) {
	t.Cleanup(func() { SetUsageLogTrustedProxyCIDRs("") })
	assert.Equal(t, []string{"bad-cidr"}, SetUsageLogTrustedProxyCIDRs("127.0.0.1;bad-cidr"))
}
