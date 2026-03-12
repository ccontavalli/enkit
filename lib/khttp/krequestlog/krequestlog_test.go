package krequestlog

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ccontavalli/enkit/lib/kflags"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"
)

type captureFlagSet struct {
	bools   map[string]*bool
	strings map[string]*string
}

func newCaptureFlagSet() *captureFlagSet {
	return &captureFlagSet{
		bools:   map[string]*bool{},
		strings: map[string]*string{},
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
