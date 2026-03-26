package utils

import "strings"

// NormalizeHost canonicalizes a configured host name for matching and
// de-duplication. Host names are case-insensitive, so configuration and routing
// state should not distinguish values that differ only by case, padding, or a
// trailing ".".
func NormalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	return strings.TrimRight(host, ".")
}
