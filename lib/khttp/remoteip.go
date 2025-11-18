package khttp

import (
	"fmt"
	"net/http"
	"strings"
)

// ClientOrigin returns a string identifying the origin of a request.
//
// It includes the direct remote address and any proxy headers like
// X-Forwarded-For and X-Real-IP to provide full context for debugging and logging.
func ClientOrigin(r *http.Request) string {
	var parts []string

	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts = append(parts, fmt.Sprintf("X-Forwarded-For: %q", fwd))
	}

	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		parts = append(parts, fmt.Sprintf("X-Real-IP: %q", realIP))
	}

	if len(parts) == 0 {
		return r.RemoteAddr
	}

	return fmt.Sprintf("%s (%s)", r.RemoteAddr, strings.Join(parts, ", "))
}