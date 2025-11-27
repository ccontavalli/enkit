package krequestlog

import (
	"context"
	"time"

	"github.com/ccontavalli/enkit/lib/khttp/kgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

func UnaryInterceptor(mods ...Modifier) grpc.UnaryServerInterceptor {
	opts := NewOptions(mods...)
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()
		method := info.FullMethod
		
		origin := kgrpc.ClientOrigin(ctx)

		if opts.LogStart {
			opts.Printer("GRPC START method=%s origin=%s", method, origin)
		}
		
		resp, err := handler(ctx, req)
		
		if opts.LogEnd {
			code := status.Code(err)
			opts.Printer("GRPC END method=%s origin=%s code=%s duration=%v", method, origin, code, time.Since(start))
		}
		
		return resp, err
	}
}

func StreamInterceptor(mods ...Modifier) grpc.StreamServerInterceptor {
	opts := NewOptions(mods...)
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		method := info.FullMethod
		
		origin := kgrpc.ClientOrigin(ss.Context())

		if opts.LogStart {
			opts.Printer("GRPC STREAM START method=%s origin=%s", method, origin)
		}
		
		err := handler(srv, ss)
		
		if opts.LogEnd {
			code := status.Code(err)
			opts.Printer("GRPC STREAM END method=%s origin=%s code=%s duration=%v", method, origin, code, time.Since(start))
		}
		
		return err
	}
}

