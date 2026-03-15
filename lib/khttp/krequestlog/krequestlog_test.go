package krequestlog

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ccontavalli/enkit/lib/kflags"
	gws "github.com/gorilla/websocket"
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

func TestRegisterIncludesLogOmitSubstrFlag(t *testing.T) {
	flags := DefaultFlags()
	set := newCaptureFlagSet()
	flags.Register(set, "")

	got, ok := set.stringArrays["log-omit-substr"]
	if !ok {
		t.Fatalf("log-omit-substr flag was not registered")
	}
	if len(*got) != 0 {
		t.Fatalf("log-omit-substr should default to empty, got %v", *got)
	}
}

func TestRegisterIncludesLogOmitRegexFlag(t *testing.T) {
	flags := DefaultFlags()
	set := newCaptureFlagSet()
	flags.Register(set, "")

	got, ok := set.stringArrays["log-omit-regex"]
	if !ok {
		t.Fatalf("log-omit-regex flag was not registered")
	}
	if len(*got) != 0 {
		t.Fatalf("log-omit-regex should default to empty, got %v", *got)
	}
}

func TestFromFlagsConfiguresLineFilters(t *testing.T) {
	flags := DefaultFlags()
	flags.LogOmitSubstr = []string{"Poll"}
	flags.LogOmitRegex = []string{`origin=127\.0\.0\.1:50082`}

	opts := NewOptions(FromFlags(flags))

	if opts.LogFilter == nil {
		t.Fatalf("expected FromFlags to install a log filter")
	}
	if opts.LogFilter("GRPC START method=/test.Service/PollStatus origin=10.0.0.1:1234") {
		t.Fatalf("expected substring filter to omit matching log line")
	}
	if opts.LogFilter("HTTP START origin=127.0.0.1:50082 method=GET path=/healthz") {
		t.Fatalf("expected regex filter to omit matching log line")
	}
	if !opts.LogFilter("GRPC START method=/test.Service/GetStatus origin=10.0.0.1:1234") {
		t.Fatalf("expected filter to allow non-matching log line")
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

func TestHTTPHandlerOmitsRegexFilteredLines(t *testing.T) {
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
		WithLogFilter(RegexFilter([]string{`origin=127\.0\.0\.1:50082`})),
	)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "127.0.0.1:50082"
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if len(lines) != 0 {
		t.Fatalf("expected regex filter to omit matching log line, got %v", lines)
	}
}

func TestHTTPHandlerLogsCopiedResponseSize(t *testing.T) {
	var lines []string
	handler := NewHandler(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n, err := io.Copy(w, strings.NewReader("hello"))
			if err != nil {
				t.Fatalf("copy failed: %v", err)
			}
			if n != 5 {
				t.Fatalf("unexpected copy length %d", n)
			}
		}),
		WithPrinter(func(format string, args ...interface{}) {
			lines = append(lines, fmt.Sprintf(format, args...))
		}),
		func(o *Options) {
			o.LogStart = false
			o.LogEnd = true
		},
	)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "127.0.0.1:50082"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if got := recorder.Body.String(); got != "hello" {
		t.Fatalf("unexpected response body %q", got)
	}
	if len(lines) != 1 {
		t.Fatalf("expected one log line, got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "HTTP END origin=127.0.0.1:50082 method=GET path=/ status=200 size=5") {
		t.Fatalf("unexpected end log line %q", lines[0])
	}
}

func TestUnaryInterceptorOmitsFilteredLines(t *testing.T) {
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
		WithLogFilter(SubstringFilter([]string{"Poll", "Heartbeat"})),
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
		t.Fatalf("expected filtered line to produce no logs, got %v", lines)
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

func TestStreamInterceptorOmitsFilteredLines(t *testing.T) {
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
		WithLogFilter(SubstringFilter([]string{"Poll"})),
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
		t.Fatalf("expected filtered line to produce no logs, got %v", lines)
	}
}

func TestHTTPHandlerPreservesWebsocketUpgrade(t *testing.T) {
	upgrader := gws.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	handler := NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		mt, payload, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("read failed: %v", err)
			return
		}
		if err := conn.WriteMessage(mt, payload); err != nil {
			t.Errorf("write failed: %v", err)
		}
	}))

	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := strings.Replace(server.URL, "http://", "ws://", 1)
	conn, _, err := gws.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(gws.TextMessage, []byte("hello")); err != nil {
		t.Fatalf("client write failed: %v", err)
	}
	mt, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("client read failed: %v", err)
	}
	if mt != gws.TextMessage {
		t.Fatalf("unexpected message type %d", mt)
	}
	if string(payload) != "hello" {
		t.Fatalf("unexpected payload %q", payload)
	}
}
