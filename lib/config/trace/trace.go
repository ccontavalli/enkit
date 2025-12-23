// Package trace provides lightweight tracing wrappers for config stores and openers.
//
// Example:
//
//	flags := trace.DefaultFlags().Register(flagSet, "")
//	tracer := trace.New(trace.FromFlags(flags), trace.WithLogger(logger.Go))
//	store, _ := opener("familyshare", "views")
//	store = tracer.WrapStore("familyshare/views", store)
package trace

import (
	"fmt"
	"path"
	"reflect"
	"strings"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/kflags"
	"github.com/ccontavalli/enkit/lib/logger"
)

// Flags configures tracing for config stores.
type Flags struct {
	Enabled      bool
	LogResponses bool
	Include      []string
	Exclude      []string
}

// DefaultFlags returns flags with tracing disabled.
func DefaultFlags() *Flags {
	return &Flags{}
}

// Register registers tracing flags with the provided FlagSet.
func (f *Flags) Register(set kflags.FlagSet, prefix string) *Flags {
	set.BoolVar(&f.Enabled, prefix+"config-store-trace", f.Enabled, "Enable config store tracing.")
	set.BoolVar(&f.LogResponses, prefix+"config-store-trace-responses", f.LogResponses, "Log config store responses as well as lookups.")
	set.StringArrayVar(&f.Include, prefix+"config-store-trace-include", f.Include, "Trace only stores with this prefix (repeatable).")
	set.StringArrayVar(&f.Exclude, prefix+"config-store-trace-exclude", f.Exclude, "Do not trace stores with this prefix (repeatable).")
	return f
}

// Tracer wraps config stores and openers with logging.
type Tracer struct {
	flags Flags
	log   logger.Logger
}

// Options defines configuration for a Tracer.
type Options struct {
	Log          logger.Logger
	Enabled      bool
	LogResponses bool
	Include      []string
	Exclude      []string
}

// Modifier mutates Options.
type Modifier func(*Options)

// WithLogger sets the logger used by the tracer.
func WithLogger(log logger.Logger) Modifier {
	return func(o *Options) {
		o.Log = log
	}
}

// FromFlags applies a Flags struct to the tracer options.
func FromFlags(flags *Flags) Modifier {
	return func(o *Options) {
		if flags == nil {
			return
		}
		o.Enabled = flags.Enabled
		o.LogResponses = flags.LogResponses
		o.Include = append([]string{}, flags.Include...)
		o.Exclude = append([]string{}, flags.Exclude...)
	}
}

// WithEnabled overrides whether tracing is enabled.
func WithEnabled(enabled bool) Modifier {
	return func(o *Options) {
		o.Enabled = enabled
	}
}

// WithLogResponses overrides response logging.
func WithLogResponses(enabled bool) Modifier {
	return func(o *Options) {
		o.LogResponses = enabled
	}
}

// WithInclude overrides the include list.
func WithInclude(include []string) Modifier {
	return func(o *Options) {
		o.Include = append([]string{}, include...)
	}
}

// WithExclude overrides the exclude list.
func WithExclude(exclude []string) Modifier {
	return func(o *Options) {
		o.Exclude = append([]string{}, exclude...)
	}
}

// New creates a new Tracer using the provided modifiers.
func New(mods ...Modifier) *Tracer {
	opts := &Options{
		Log: logger.Go,
	}
	for _, mod := range mods {
		mod(opts)
	}
	if opts.Log == nil {
		opts.Log = logger.Go
	}
	return &Tracer{flags: Flags{
		Enabled:      opts.Enabled,
		LogResponses: opts.LogResponses,
		Include:      append([]string{}, opts.Include...),
		Exclude:      append([]string{}, opts.Exclude...),
	}, log: opts.Log}
}

// WrapOpener returns an opener that wraps any returned store with tracing.
func (t *Tracer) WrapOpener(opener config.Opener) config.Opener {
	if opener == nil {
		return nil
	}
	return func(app string, namespace ...string) (config.Store, error) {
		store, err := opener(app, namespace...)
		if err != nil {
			return nil, err
		}
		return t.WrapStore(storeName(app, namespace), store), nil
	}
}

// WrapStore returns a traced store if enabled for name.
func (t *Tracer) WrapStore(name string, store config.Store) config.Store {
	if store == nil || !t.enabledFor(name) {
		return store
	}
	return &tracedStore{name: name, store: store, log: t.log, logResponses: t.flags.LogResponses}
}

func (t *Tracer) enabledFor(name string) bool {
	if !t.flags.Enabled && !t.flags.LogResponses {
		return false
	}
	for _, exclude := range t.flags.Exclude {
		if strings.HasPrefix(name, exclude) {
			return false
		}
	}
	if len(t.flags.Include) == 0 {
		return true
	}
	for _, include := range t.flags.Include {
		if strings.HasPrefix(name, include) {
			return true
		}
	}
	return false
}

type tracedStore struct {
	name         string
	store        config.Store
	log          logger.Logger
	logResponses bool
}

func (t *tracedStore) List() ([]config.Descriptor, error) {
	t.log.Infof("config store %s: List()", t.name)
	descs, err := t.store.List()
	if err != nil {
		t.log.Infof("config store %s: List() error: %v", t.name, err)
		return nil, err
	}
	if t.logResponses {
		t.log.Infof("config store %s: List() -> %v", t.name, descs)
	}
	return descs, nil
}

func (t *tracedStore) Marshal(desc config.Descriptor, value interface{}) error {
	t.log.Infof("config store %s: Marshal(%v)", t.name, desc)
	if t.logResponses {
		t.log.Infof("config store %s: Marshal(%v) value=%s", t.name, desc, formatValue(value))
	}
	err := t.store.Marshal(desc, value)
	if err != nil {
		t.log.Infof("config store %s: Marshal(%v) error: %v", t.name, desc, err)
	}
	return err
}

func (t *tracedStore) Unmarshal(name string, value interface{}) (config.Descriptor, error) {
	t.log.Infof("config store %s: Unmarshal(%q)", t.name, name)
	desc, err := t.store.Unmarshal(name, value)
	if err != nil {
		t.log.Infof("config store %s: Unmarshal(%q) error: %v", t.name, name, err)
		return desc, err
	}
	if t.logResponses {
		t.log.Infof("config store %s: Unmarshal(%q) -> %s", t.name, name, formatValue(value))
	}
	return desc, nil
}

func (t *tracedStore) Delete(desc config.Descriptor) error {
	t.log.Infof("config store %s: Delete(%v)", t.name, desc)
	err := t.store.Delete(desc)
	if err != nil {
		t.log.Infof("config store %s: Delete(%v) error: %v", t.name, desc, err)
	}
	return err
}

func formatValue(value interface{}) string {
	if value == nil {
		return "<nil>"
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() == reflect.Ptr && !rv.IsNil() {
		return fmt.Sprintf("%+v", rv.Elem().Interface())
	}
	return fmt.Sprintf("%+v", value)
}

func storeName(app string, namespace []string) string {
	if app == "" && len(namespace) == 0 {
		return ""
	}
	return path.Join(append([]string{app}, namespace...)...)
}
