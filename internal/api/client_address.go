package api

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

const (
	cloudflareClientHeader = "CF-Connecting-IP"
	forwardedForHeader     = "X-Forwarded-For"
	realIPHeader           = "X-Real-IP"
)

var cloudflareRanges = mustParsePrefixes(
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22", "141.101.64.0/18",
	"108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20", "197.234.240.0/22", "198.41.128.0/17",
	"162.158.0.0/15", "104.16.0.0/13", "104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
	"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32", "2405:8100::/32",
	"2a06:98c0::/29", "2c0f:f248::/32",
)

func mustParsePrefixes(values ...string) []netip.Prefix {
	prefixes := make([]netip.Prefix, len(values))
	for i, value := range values {
		prefixes[i] = netip.MustParsePrefix(value)
	}
	return prefixes
}

func fromCloudflare(address string) bool {
	parsed, err := netip.ParseAddr(address)
	if err != nil {
		return false
	}
	parsed = parsed.Unmap()
	for _, prefix := range cloudflareRanges {
		if prefix.Contains(parsed) {
			return true
		}
	}
	return false
}

func lastForwardedHop(header string) string {
	hops := strings.Split(header, ",")
	return strings.TrimSpace(hops[len(hops)-1])
}

func remoteHost(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

func edgeAddress(r *http.Request) string {
	if forwarded := lastForwardedHop(r.Header.Get(forwardedForHeader)); forwarded != "" {
		return forwarded
	}
	if real := strings.TrimSpace(r.Header.Get(realIPHeader)); real != "" {
		return real
	}
	return remoteHost(r.RemoteAddr)
}

func clientAddress(r *http.Request, trustCloudflareHeader bool) string {
	edge := edgeAddress(r)
	visitor := strings.TrimSpace(r.Header.Get(cloudflareClientHeader))
	if visitor != "" && (trustCloudflareHeader || fromCloudflare(edge)) {
		return visitor
	}
	return edge
}

func (s *Server) visitor(r *http.Request) string {
	return clientAddress(r, s.trustCloudflareHeader)
}
