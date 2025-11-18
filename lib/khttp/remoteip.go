package khttp

import (
	"net"
	"net/http"
	"strings"
)

// RemoteIP returns the remote client IP address from a request.
//
// It gives precedence to the X-Forwarded-For header to work correctly
// behind proxies.
func RemoteIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		// X-Forwarded-For can be a comma-separated list of IPs.
		// The first one is the original client.
		if parts := strings.Split(fwd, ","); len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}

	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return realIP
	}

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
