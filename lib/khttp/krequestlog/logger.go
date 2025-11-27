package krequestlog

import (
	"github.com/ccontavalli/enkit/lib/kflags"
	"github.com/ccontavalli/enkit/lib/logger"
)

type Flags struct {
	LogStart  bool
	LogEnd    bool
	LogFormat string
}

func DefaultFlags() *Flags {
	return &Flags{
		LogStart:  false,
		LogEnd:    true,
		LogFormat: "text",
	}
}

func (f *Flags) Register(set kflags.FlagSet, prefix string) *Flags {
	set.BoolVar(&f.LogStart, prefix+"log-start", f.LogStart, "Log request start")
	set.BoolVar(&f.LogEnd, prefix+"log-end", f.LogEnd, "Log request end")
	set.StringVar(&f.LogFormat, prefix+"log-format", f.LogFormat, "Log format (text, json, apache)")
	return f
}

type Options struct {
	Log       logger.Logger
	LogStart  bool
	LogEnd    bool
	LogFormat string
	Printer   func(format string, args ...interface{})
}

type Modifier func(*Options)

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

func FromFlags(flags *Flags) Modifier {
	return func(o *Options) {
		o.LogStart = flags.LogStart
		o.LogEnd = flags.LogEnd
		o.LogFormat = flags.LogFormat
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
