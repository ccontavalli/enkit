package kgrpc

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

// ClientOrigin returns a string identifying the origin of a gRPC request.
//
// It includes the direct remote address from the peer and any proxy headers
// like x-forwarded-for and x-real-ip from the gRPC metadata.
func ClientOrigin(ctx context.Context) string {
	var parts []string

	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if fwd := md.Get("x-forwarded-for"); len(fwd) > 0 {
			parts = append(parts, fmt.Sprintf("x-forwarded-for: %q", strings.Join(fwd, ", ")))
		}
		if realIP := md.Get("x-real-ip"); len(realIP) > 0 {
			parts = append(parts, fmt.Sprintf("x-real-ip: %q", strings.Join(realIP, ", ")))
		}
	}

	remoteAddr := "unknown"
	if p, ok := peer.FromContext(ctx); ok {
		remoteAddr = p.Addr.String()
	}

	if len(parts) == 0 {
		return remoteAddr
	}

	return fmt.Sprintf("%s (%s)", remoteAddr, strings.Join(parts, ", "))
}
