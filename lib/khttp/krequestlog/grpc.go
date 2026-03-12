package krequestlog

import (
	"context"
	"fmt"
	"time"

	"github.com/ccontavalli/enkit/lib/khttp/kgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func UnaryInterceptor(mods ...Modifier) grpc.UnaryServerInterceptor {
	opts := NewOptions(mods...)
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()
		method := info.FullMethod

		origin := kgrpc.ClientOrigin(ctx)

		if opts.LogStart {
			if opts.LogPayloads {
				printLogLine(opts, "GRPC START method=%s origin=%s request=%s", method, origin, payloadString(req))
			} else {
				printLogLine(opts, "GRPC START method=%s origin=%s", method, origin)
			}
		}

		resp, err := handler(ctx, req)

		if opts.LogEnd {
			code := status.Code(err)
			if opts.LogPayloads {
				printLogLine(opts, "GRPC END method=%s origin=%s code=%s duration=%v response=%s", method, origin, code, time.Since(start), payloadString(resp))
			} else {
				printLogLine(opts, "GRPC END method=%s origin=%s code=%s duration=%v", method, origin, code, time.Since(start))
			}
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
			printLogLine(opts, "GRPC STREAM START method=%s origin=%s", method, origin)
		}

		err := handler(srv, ss)

		if opts.LogEnd {
			code := status.Code(err)
			printLogLine(opts, "GRPC STREAM END method=%s origin=%s code=%s duration=%v", method, origin, code, time.Since(start))
		}

		return err
	}
}

func payloadString(payload interface{}) string {
	if payload == nil {
		return "<nil>"
	}

	message, ok := payload.(proto.Message)
	if !ok {
		return fmt.Sprintf("%#v", payload)
	}

	data, err := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: true,
	}.Marshal(message)
	if err != nil {
		return fmt.Sprintf("<<payload marshal error: %v>>", err)
	}

	return string(data)
}
