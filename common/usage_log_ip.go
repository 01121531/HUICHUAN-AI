package common

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
)

const UsageLogClientIPKey = "usage_log_client_ip"

// DefaultUsageLogTrustedProxyCIDRs covers the most common deployment shape:
// a local Nginx/Caddy/system proxy forwards traffic to HUICHUAN over loopback.
// Operators can add more proxy/load-balancer networks through TrustedProxyCIDRs.
const DefaultUsageLogTrustedProxyCIDRs = "127.0.0.0/8,::1/128"

var UsageLogIPCaptureEnabled atomic.Bool

var trustedProxyPrefixes atomic.Value

func init() {
	trustedProxyPrefixes.Store(defaultUsageLogTrustedProxyPrefixes())
}

// SetUsageLogTrustedProxyCIDRs atomically publishes the trusted proxy list.
// Invalid entries are ignored and returned to the caller for validation. The
// loopback defaults are always included so local reverse proxies do not cause
// usage logs to record 127.0.0.1 instead of the real forwarded client address.
func SetUsageLogTrustedProxyCIDRs(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ';'
	})
	prefixes := defaultUsageLogTrustedProxyPrefixes()
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

func defaultUsageLogTrustedProxyPrefixes() []netip.Prefix {
	defaults := strings.Split(DefaultUsageLogTrustedProxyCIDRs, ",")
	prefixes := make([]netip.Prefix, 0, len(defaults))
	for _, raw := range defaults {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err == nil {
			prefixes = append(prefixes, prefix.Masked())
		}
	}
	return prefixes
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
	host := strings.Trim(strings.TrimSpace(raw), `"`)
	if host == "" || strings.EqualFold(host, "unknown") {
		return netip.Addr{}, false
	}
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

func forwardedHeaderForChain(raw string) []netip.Addr {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	chain := make([]netip.Addr, 0)
	for _, element := range strings.Split(raw, ",") {
		for _, pair := range strings.Split(element, ";") {
			key, value, ok := strings.Cut(strings.TrimSpace(pair), "=")
			if !ok || !strings.EqualFold(strings.TrimSpace(key), "for") {
				continue
			}
			if address, valid := parseForwardedIP(value); valid {
				chain = append(chain, address)
			}
		}
	}
	return chain
}

func firstUntrustedForwardedIP(chain []netip.Addr) (netip.Addr, bool) {
	if len(chain) == 0 {
		return netip.Addr{}, false
	}
	for index := len(chain) - 1; index >= 0; index-- {
		if !isUsageLogTrustedProxy(chain[index]) {
			return chain[index], true
		}
	}
	return chain[0], true
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
	if address, valid := firstUntrustedForwardedIP(chain); valid {
		return address.String()
	}

	if address, valid := firstUntrustedForwardedIP(forwardedHeaderForChain(request.Header.Get("Forwarded"))); valid {
		return address.String()
	}

	for _, headerName := range []string{"X-Real-IP", "CF-Connecting-IP", "True-Client-IP", "X-Client-IP"} {
		if address, valid := parseForwardedIP(request.Header.Get(headerName)); valid {
			return address.String()
		}
	}
	return remote.String()
}
