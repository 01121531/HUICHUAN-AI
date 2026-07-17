package common

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
)

const UsageLogClientIPKey = "usage_log_client_ip"

var UsageLogIPCaptureEnabled atomic.Bool

var trustedProxyPrefixes atomic.Value

func init() {
	trustedProxyPrefixes.Store([]netip.Prefix{})
}

// SetUsageLogTrustedProxyCIDRs atomically publishes the trusted proxy list.
// Invalid entries are ignored and returned to the caller for validation.
func SetUsageLogTrustedProxyCIDRs(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ';'
	})
	prefixes := make([]netip.Prefix, 0, len(parts))
	invalid := make([]string, 0)
	for _, raw := range parts {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			if address, addressErr := netip.ParseAddr(raw); addressErr == nil {
				bits := 128
				if address.Is4() {
					bits = 32
				}
				prefix = netip.PrefixFrom(address.Unmap(), bits)
			} else {
				invalid = append(invalid, raw)
				continue
			}
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	if len(invalid) == 0 {
		trustedProxyPrefixes.Store(prefixes)
	}
	return invalid
}

func isUsageLogTrustedProxy(address netip.Addr) bool {
	for _, prefix := range trustedProxyPrefixes.Load().([]netip.Prefix) {
		if prefix.Contains(address.Unmap()) {
			return true
		}
	}
	return false
}

func parseRemoteIP(remoteAddress string) (netip.Addr, bool) {
	host := strings.TrimSpace(remoteAddress)
	if splitHost, _, err := net.SplitHostPort(host); err == nil {
		host = splitHost
	}
	host = strings.Trim(host, "[]")
	if zone := strings.LastIndex(host, "%"); zone >= 0 {
		host = host[:zone]
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return address.Unmap(), true
}

func parseForwardedIP(raw string) (netip.Addr, bool) {
	address, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil {
		return netip.Addr{}, false
	}
	return address.Unmap(), true
}

// ResolveUsageLogClientIP honors forwarded headers only when the immediate
// network peer is configured as trusted. It walks X-Forwarded-For from right
// to left and returns the first untrusted hop.
func ResolveUsageLogClientIP(request *http.Request) string {
	if request == nil {
		return ""
	}
	remote, ok := parseRemoteIP(request.RemoteAddr)
	if !ok {
		return ""
	}
	if !isUsageLogTrustedProxy(remote) {
		return remote.String()
	}

	chain := make([]netip.Addr, 0)
	for _, raw := range strings.Split(request.Header.Get("X-Forwarded-For"), ",") {
		if address, valid := parseForwardedIP(raw); valid {
			chain = append(chain, address)
		}
	}
	if len(chain) == 0 {
		if address, valid := parseForwardedIP(request.Header.Get("X-Real-IP")); valid {
			return address.String()
		}
		return remote.String()
	}
	for index := len(chain) - 1; index >= 0; index-- {
		if !isUsageLogTrustedProxy(chain[index]) {
			return chain[index].String()
		}
	}
	return chain[0].String()
}
