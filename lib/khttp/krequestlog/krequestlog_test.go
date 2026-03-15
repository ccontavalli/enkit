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
	"github.com/stretchr/testify/assert"
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

	_, ok := set.bools["log-payloads"]
	if assert.True(t, ok, "log-payloads flag was not registered") {
		assert.False(t, *set.bools["log-payloads"], "log-payloads should default to false")
	}
}

func TestRegisterIncludesLogOmitSubstrFlag(t *testing.T) {
	flags := DefaultFlags()
	set := newCaptureFlagSet()
	flags.Register(set, "")

	got, ok := set.stringArrays["log-omit-substr"]
	if assert.True(t, ok, "log-omit-substr flag was not registered") {
		assert.Empty(t, *got, "log-omit-substr should default to empty")
	}
}

func TestRegisterIncludesLogOmitRegexFlag(t *testing.T) {
	flags := DefaultFlags()
	set := newCaptureFlagSet()
	flags.Register(set, "")

	got, ok := set.stringArrays["log-omit-regex"]
	if assert.True(t, ok, "log-omit-regex flag was not registered") {
		assert.Empty(t, *got, "log-omit-regex should default to empty")
	}
}

func TestFromFlagsConfiguresLineFilters(t *testing.T) {
	flags := DefaultFlags()
	flags.LogOmitSubstr = []string{"Poll"}
	flags.LogOmitRegex = []string{`origin=127\.0\.0\.1:50082`}

	opts := NewOptions(FromFlags(flags))

	if assert.NotNil(t, opts.LogFilter, "expected FromFlags to install a log filter") {
		assert.False(t, opts.LogFilter("GRPC START method=/test.Service/PollStatus origin=10.0.0.1:1234"), "expected substring filter to omit matching log line")
		assert.False(t, opts.LogFilter("HTTP START origin=127.0.0.1:50082 method=GET path=/healthz"), "expected regex filter to omit matching log line")
		assert.True(t, opts.LogFilter("GRPC START method=/test.Service/GetStatus origin=10.0.0.1:1234"), "expected filter to allow non-matching log line")
	}
}

func TestUnaryInterceptorLogsPayloads(t *testing.T) {
	request, err := structpb.NewStruct(map[string]interface{}{
		"message": "hello",
	})
	if !assert.NoError(t, err, "could not create request") {
		return
	}
	response, err := structpb.NewStruct(map[string]interface{}{
		"message": "world",
	})
	if !assert.NoError(t, err, "could not create response") {
		return
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
	assert.NoError(t, err, "interceptor returned error")

	joined := strings.Join(lines, "\n")
	assert.Len(t, lines, 2, "expected two log lines")
	assert.Contains(t, joined, `GRPC START method=/test.Service/Call`, "request start line missing")
	assert.Contains(t, joined, `request={"message":"hello"}`, "request payload missing from logs")
	assert.NotContains(t, joined, "GRPC REQUEST", "payloads should be attached to START/END lines")
	assert.NotContains(t, joined, "GRPC RESPONSE", "payloads should be attached to START/END lines")
	assert.Contains(t, joined, `GRPC END method=/test.Service/Call`, "response end line missing")
	assert.Contains(t, joined, `response={"message":"world"}`, "response payload missing from logs")
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

	if assert.Len(t, lines, 1, "expected one log line") {
		assert.Equal(t, "HTTP START origin=127.0.0.1:50082 method=GET path=/", lines[0], "unexpected start log line")
		assert.NotContains(t, lines[0], "%!s(MISSING)", "unexpected missing format argument in log line")
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

	assert.Empty(t, lines, "expected regex filter to omit matching log line")
}

func TestHTTPHandlerLogsCopiedResponseSize(t *testing.T) {
	var lines []string
	handler := NewHandler(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n, err := io.Copy(w, strings.NewReader("hello"))
			assert.NoError(t, err, "copy failed")
			assert.EqualValues(t, 5, n, "unexpected copy length")
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

	assert.Equal(t, "hello", recorder.Body.String(), "unexpected response body")
	if assert.Len(t, lines, 1, "expected one log line") {
		assert.Contains(t, lines[0], "HTTP END origin=127.0.0.1:50082 method=GET path=/ status=200 size=5", "unexpected end log line")
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
	assert.NoError(t, err, "interceptor returned error")
	assert.True(t, called, "handler was not invoked")
	assert.Empty(t, lines, "expected filtered line to produce no logs")
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
	assert.NoError(t, err, "interceptor returned error")
	assert.True(t, called, "handler was not invoked")
	assert.Empty(t, lines, "expected filtered line to produce no logs")
}

func TestHTTPHandlerPreservesWebsocketUpgrade(t *testing.T) {
	upgrader := gws.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	handler := NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if !assert.NoError(t, err, "upgrade failed") {
			return
		}
		defer conn.Close()

		mt, payload, err := conn.ReadMessage()
		if !assert.NoError(t, err, "read failed") {
			return
		}
		assert.NoError(t, conn.WriteMessage(mt, payload), "write failed")
	}))

	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := strings.Replace(server.URL, "http://", "ws://", 1)
	conn, _, err := gws.DefaultDialer.Dial(wsURL, nil)
	if !assert.NoError(t, err, "dial failed") {
		return
	}
	defer conn.Close()

	assert.NoError(t, conn.WriteMessage(gws.TextMessage, []byte("hello")), "client write failed")
	mt, payload, err := conn.ReadMessage()
	assert.NoError(t, err, "client read failed")
	assert.Equal(t, gws.TextMessage, mt, "unexpected message type")
	assert.Equal(t, "hello", string(payload), "unexpected payload")
}
