// Package otesting provides helpers to assume authenticated users in tests
// and local development without real auth cookies.
//
// Example:
//
//	creds := otesting.AssumedCredentials("ccontavalli@gmail.com")
//	grpcs := grpc.NewServer(
//		grpc.ChainUnaryInterceptor(
//			otesting.AssumeAuthUnaryInterceptor(creds),
//		),
//		grpc.ChainStreamInterceptor(
//			otesting.AssumeAuthStreamInterceptor(creds),
//		),
//	)
package otesting

import (
	"context"
	"strings"

	"github.com/ccontavalli/enkit/lib/oauth"
	"github.com/ccontavalli/enkit/lib/oauth/ogrpc"
	"google.golang.org/grpc"
)

// AssumedCredentials returns credentials for a specific user, used for local testing.
func AssumedCredentials(user string) *oauth.CredentialsCookie {
	trimmed := strings.TrimSpace(strings.ToLower(user))
	username, organization := trimmed, "local"
	if parts := strings.SplitN(trimmed, "@", 2); len(parts) == 2 {
		username = parts[0]
		organization = parts[1]
	}
	return &oauth.CredentialsCookie{
		Identity: oauth.Identity{
			Username:     username,
			Organization: organization,
		},
	}
}

// AssumeAuthUnaryInterceptor injects credentials for unary RPCs.
func AssumeAuthUnaryInterceptor(creds *oauth.CredentialsCookie) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		return handler(oauth.SetCredentials(ctx, creds), req)
	}
}

// AssumeAuthStreamInterceptor injects credentials for streaming RPCs.
func AssumeAuthStreamInterceptor(creds *oauth.CredentialsCookie) grpc.StreamServerInterceptor {
	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := oauth.SetCredentials(stream.Context(), creds)
		return handler(srv, ogrpc.SetContextStream(stream, ctx))
	}
}
