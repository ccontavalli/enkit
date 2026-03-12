package krequestlog

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ccontavalli/enkit/lib/kflags"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/structpb"
)

type captureFlagSet struct {
	bools        map[string]*bool
	strings      map[string]*string
	stringArrays map[string]*[]string
}

func newCaptureFlagSet() *captureFlagSet {
	return &captureFlagSet{
		bools:        map[string]*bool{},
		strings:      map[string]*string{},
		stringArrays: map[string]*[]string{},
	}
}

func (c *captureFlagSet) BoolVar(target *bool, name string, value bool, usage string) {
	*target = value
	c.bools[name] = target
}

func (c *captureFlagSet) DurationVar(target *time.Duration, name string, value time.Duration, usage string) {
}

func (c *captureFlagSet) StringVar(target *string, name string, value string, usage string) {
	*target = value
	c.strings[name] = target
}

func (c *captureFlagSet) StringArrayVar(target *[]string, name string, value []string, usage string) {
	copy := append([]string{}, value...)
	*target = copy
	c.stringArrays[name] = target
}

func (c *captureFlagSet) ByteFileVar(target *[]byte, name string, defaultFile string, usage string, mods ...kflags.ByteFileModifier) {
}

func (c *captureFlagSet) IntVar(target *int, name string, value int, usage string) {}

func TestRegisterIncludesLogPayloadsFlag(t *testing.T) {
	flags := DefaultFlags()
	set := newCaptureFlagSet()
	flags.Register(set, "")

	if _, ok := set.bools["log-payloads"]; !ok {
		t.Fatalf("log-payloads flag was not registered")
	}
	if got := *set.bools["log-payloads"]; got {
		t.Fatalf("log-payloads should default to false")
	}
}

func TestRegisterIncludesLogOmitMethodFlag(t *testing.T) {
	flags := DefaultFlags()
	set := newCaptureFlagSet()
	flags.Register(set, "")

	got, ok := set.stringArrays["log-omit-method"]
	if !ok {
		t.Fatalf("log-omit-method flag was not registered")
	}
	if len(*got) != 0 {
		t.Fatalf("log-omit-method should default to empty, got %v", *got)
	}
}

func TestFromFlagsConfiguresMethodFilter(t *testing.T) {
	flags := DefaultFlags()
	flags.LogOmitMethods = []string{"Poll"}

	opts := NewOptions(FromFlags(flags))

	if opts.LogFilter == nil {
		t.Fatalf("expected FromFlags to install a log filter")
	}
	if opts.LogFilter("/test.Service/PollStatus") {
		t.Fatalf("expected filter to omit matching method")
	}
	if !opts.LogFilter("/test.Service/GetStatus") {
		t.Fatalf("expected filter to allow non-matching method")
	}
}

func TestUnaryInterceptorLogsPayloads(t *testing.T) {
	request, err := structpb.NewStruct(map[string]interface{}{
		"message": "hello",
	})
	if err != nil {
		t.Fatalf("could not create request: %v", err)
	}
	response, err := structpb.NewStruct(map[string]interface{}{
		"message": "world",
	})
	if err != nil {
		t.Fatalf("could not create response: %v", err)
	}

	var lines []string
	interceptor := UnaryInterceptor(
		WithPrinter(func(format string, args ...interface{}) {
			lines = append(lines, fmt.Sprintf(format, args...))
		}),
		func(o *Options) {
			o.LogStart = true
			o.LogEnd = true
			o.LogPayloads = true
		},
	)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return response, nil
	}

	_, err = interceptor(context.Background(), request, &grpc.UnaryServerInfo{FullMethod: "/test.Service/Call"}, handler)
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}

	joined := strings.Join(lines, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected two log lines, got %d: %s", len(lines), joined)
	}
	if !strings.Contains(joined, `GRPC START method=/test.Service/Call`) {
		t.Fatalf("request start line missing: %s", joined)
	}
	if !strings.Contains(joined, `request={"message":"hello"}`) {
		t.Fatalf("request payload missing from logs: %s", joined)
	}
	if strings.Contains(joined, "GRPC REQUEST") || strings.Contains(joined, "GRPC RESPONSE") {
		t.Fatalf("payloads should be attached to START/END lines, got: %s", joined)
	}
	if !strings.Contains(joined, `GRPC END method=/test.Service/Call`) {
		t.Fatalf("response end line missing: %s", joined)
	}
	if !strings.Contains(joined, `response={"message":"world"}`) {
		t.Fatalf("response payload missing from logs: %s", joined)
	}
}

func TestHTTPHandlerLogsStartWithoutMissingArgs(t *testing.T) {
	var lines []string
	handler := NewHandler(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		WithPrinter(func(format string, args ...interface{}) {
			lines = append(lines, fmt.Sprintf(format, args...))
		}),
		func(o *Options) {
			o.LogStart = true
			o.LogEnd = false
		},
	)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "127.0.0.1:50082"
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if len(lines) != 1 {
		t.Fatalf("expected one log line, got %d: %v", len(lines), lines)
	}
	if got, want := lines[0], "HTTP START origin=127.0.0.1:50082 method=GET path=/"; got != want {
		t.Fatalf("unexpected start log line - got %q want %q", got, want)
	}
	if strings.Contains(lines[0], "%!s(MISSING)") {
		t.Fatalf("unexpected missing format argument in log line: %s", lines[0])
	}
}

func TestUnaryInterceptorOmitsFilteredMethods(t *testing.T) {
	var lines []string
	called := false
	interceptor := UnaryInterceptor(
		WithPrinter(func(format string, args ...interface{}) {
			lines = append(lines, fmt.Sprintf(format, args...))
		}),
		func(o *Options) {
			o.LogStart = true
			o.LogEnd = true
		},
		WithLogFilter(MethodFilter([]string{"Poll", "Heartbeat"})),
	)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		called = true
		return "ok", nil
	}

	_, err := interceptor(context.Background(), "request", &grpc.UnaryServerInfo{FullMethod: "/test.Service/PollStatus"}, handler)
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
	if !called {
		t.Fatalf("handler was not invoked")
	}
	if len(lines) != 0 {
		t.Fatalf("expected filtered method to produce no logs, got %v", lines)
	}
}

type fakeServerStream struct {
	ctx context.Context
}

func (f *fakeServerStream) SetHeader(metadata.MD) error { return nil }

func (f *fakeServerStream) SendHeader(metadata.MD) error { return nil }

func (f *fakeServerStream) SetTrailer(metadata.MD) {}

func (f *fakeServerStream) Context() context.Context { return f.ctx }

func (f *fakeServerStream) SendMsg(interface{}) error { return nil }

func (f *fakeServerStream) RecvMsg(interface{}) error { return nil }

func TestStreamInterceptorOmitsFilteredMethods(t *testing.T) {
	var lines []string
	called := false
	interceptor := StreamInterceptor(
		WithPrinter(func(format string, args ...interface{}) {
			lines = append(lines, fmt.Sprintf(format, args...))
		}),
		func(o *Options) {
			o.LogStart = true
			o.LogEnd = true
		},
		WithLogFilter(MethodFilter([]string{"Poll"})),
	)

	stream := &fakeServerStream{ctx: context.Background()}
	err := interceptor(nil, stream, &grpc.StreamServerInfo{FullMethod: "/test.Service/PollEvents"}, func(srv interface{}, ss grpc.ServerStream) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
	if !called {
		t.Fatalf("handler was not invoked")
	}
	if len(lines) != 0 {
		t.Fatalf("expected filtered method to produce no logs, got %v", lines)
	}
}
