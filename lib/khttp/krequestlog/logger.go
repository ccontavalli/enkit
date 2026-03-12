package krequestlog

import (
	"strings"

	"github.com/ccontavalli/enkit/lib/kflags"
	"github.com/ccontavalli/enkit/lib/logger"
)

type Flags struct {
	LogStart       bool
	LogEnd         bool
	LogPayloads    bool
	LogOmitMethods []string
	LogFormat      string
}

func DefaultFlags() *Flags {
	return &Flags{
		LogStart:       false,
		LogEnd:         true,
		LogPayloads:    false,
		LogOmitMethods: nil,
		LogFormat:      "text",
	}
}

func (f *Flags) Register(set kflags.FlagSet, prefix string) *Flags {
	set.BoolVar(&f.LogStart, prefix+"log-start", f.LogStart, "Log request start")
	set.BoolVar(&f.LogEnd, prefix+"log-end", f.LogEnd, "Log request end")
	set.BoolVar(&f.LogPayloads, prefix+"log-payloads", f.LogPayloads, "Log gRPC request and response payloads for unary requests")
	set.StringArrayVar(&f.LogOmitMethods, prefix+"log-omit-method", f.LogOmitMethods, "Omit gRPC request logs for methods containing any of the provided substrings")
	set.StringVar(&f.LogFormat, prefix+"log-format", f.LogFormat, "Log format (text, json, apache)")
	return f
}

type Options struct {
	Log         logger.Logger
	LogStart    bool
	LogEnd      bool
	LogPayloads bool
	LogFilter   LogFilter
	LogFormat   string
	Printer     func(format string, args ...interface{})
}

type Modifier func(*Options)
type LogFilter func(method string) bool

func WithLogger(log logger.Logger) Modifier {
	return func(o *Options) {
		o.Log = log
		if o.Printer == nil {
			o.Printer = log.Infof
		}
	}
}

func WithPrinter(printer func(format string, args ...interface{})) Modifier {
	return func(o *Options) {
		o.Printer = printer
	}
}

func WithLogFilter(filter LogFilter) Modifier {
	return func(o *Options) {
		o.LogFilter = filter
	}
}

func MethodFilter(omitted []string) LogFilter {
	patterns := append([]string{}, omitted...)
	return func(method string) bool {
		for _, omitted := range patterns {
			if omitted != "" && strings.Contains(method, omitted) {
				return false
			}
		}
		return true
	}
}

func FromFlags(flags *Flags) Modifier {
	return func(o *Options) {
		o.LogStart = flags.LogStart
		o.LogEnd = flags.LogEnd
		o.LogPayloads = flags.LogPayloads
		o.LogFormat = flags.LogFormat
		if len(flags.LogOmitMethods) > 0 {
			WithLogFilter(MethodFilter(flags.LogOmitMethods))(o)
		}
	}
}

func NewOptions(mods ...Modifier) *Options {
	o := &Options{
		Log:       logger.Go,
		LogEnd:    true,
		LogFormat: "text",
	}
	for _, m := range mods {
		m(o)
	}
	if o.Printer == nil {
		o.Printer = o.Log.Infof
	}
	return o
}
