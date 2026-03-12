package krequestlog

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ccontavalli/enkit/lib/kflags"
	"github.com/ccontavalli/enkit/lib/logger"
)

type Flags struct {
	LogStart      bool
	LogEnd        bool
	LogPayloads   bool
	LogOmitSubstr []string
	LogOmitRegex  []string
	LogFormat     string
}

func DefaultFlags() *Flags {
	return &Flags{
		LogStart:      false,
		LogEnd:        true,
		LogPayloads:   false,
		LogOmitSubstr: nil,
		LogOmitRegex:  nil,
		LogFormat:     "text",
	}
}

func (f *Flags) Register(set kflags.FlagSet, prefix string) *Flags {
	set.BoolVar(&f.LogStart, prefix+"log-start", f.LogStart, "Log request start")
	set.BoolVar(&f.LogEnd, prefix+"log-end", f.LogEnd, "Log request end")
	set.BoolVar(&f.LogPayloads, prefix+"log-payloads", f.LogPayloads, "Log gRPC request and response payloads for unary requests")
	set.StringArrayVar(&f.LogOmitSubstr, prefix+"log-omit-substr", f.LogOmitSubstr, "Omit request log lines containing any of the provided substrings")
	set.StringArrayVar(&f.LogOmitRegex, prefix+"log-omit-regex", f.LogOmitRegex, "Omit request log lines matching any of the provided regular expressions")
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
type LogFilter func(line string) bool

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
		if filter == nil {
			return
		}
		if o.LogFilter == nil {
			o.LogFilter = filter
			return
		}
		previous := o.LogFilter
		o.LogFilter = func(line string) bool {
			return previous(line) && filter(line)
		}
	}
}

func SubstringFilter(omitted []string) LogFilter {
	patterns := append([]string{}, omitted...)
	return func(line string) bool {
		for _, omitted := range patterns {
			if omitted != "" && strings.Contains(line, omitted) {
				return false
			}
		}
		return true
	}
}

func RegexFilter(patterns []string) LogFilter {
	filter, _ := compileRegexFilter(patterns)
	return filter
}

func FromFlags(flags *Flags) Modifier {
	return func(o *Options) {
		o.LogStart = flags.LogStart
		o.LogEnd = flags.LogEnd
		o.LogPayloads = flags.LogPayloads
		o.LogFormat = flags.LogFormat
		if len(flags.LogOmitSubstr) > 0 {
			WithLogFilter(SubstringFilter(flags.LogOmitSubstr))(o)
		}
		if len(flags.LogOmitRegex) > 0 {
			filter, invalid := compileRegexFilter(flags.LogOmitRegex)
			for _, pattern := range invalid {
				o.Log.Warnf("krequestlog: ignoring invalid log-omit-regex %s", pattern)
			}
			WithLogFilter(filter)(o)
		}
	}
}

func compileRegexFilter(patterns []string) (LogFilter, []string) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	var invalid []string
	for _, pattern := range patterns {
		if pattern == "" {
			continue
		}

		re, err := regexp.Compile(pattern)
		if err != nil {
			invalid = append(invalid, fmt.Sprintf("pattern %q: %v", pattern, err))
			continue
		}
		compiled = append(compiled, re)
	}

	return func(line string) bool {
		for _, re := range compiled {
			if re.MatchString(line) {
				return false
			}
		}
		return true
	}, invalid
}

func printLogLine(opts *Options, format string, args ...interface{}) {
	line := fmt.Sprintf(format, args...)
	if opts.LogFilter != nil && !opts.LogFilter(line) {
		return
	}
	opts.Printer("%s", line)
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
