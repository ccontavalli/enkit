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
	"strings"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/kflags"
	"github.com/ccontavalli/enkit/lib/logger"
)

// Flags configures tracing for config stores.
type Flags struct {
	Enabled      bool
	LogRequests  bool
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
	set.BoolVar(&f.Enabled, prefix+"config-store-trace", f.Enabled, "Log one completion/error line per store operation.")
	set.BoolVar(&f.LogRequests, prefix+"config-store-trace-requests", f.LogRequests, "Log request start lines (in addition to completion/error).")
	set.BoolVar(&f.LogResponses, prefix+"config-store-trace-responses", f.LogResponses, "Log completion/error lines with response payloads when available.")
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
	LogRequests  bool
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
		o.LogRequests = flags.LogRequests
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

// WithLogRequests overrides request-start logging.
func WithLogRequests(enabled bool) Modifier {
	return func(o *Options) {
		o.LogRequests = enabled
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
		LogRequests:  opts.LogRequests,
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
	return &tracedStore{
		name:         name,
		store:        store,
		log:          t.log,
		logEnabled:   t.flags.Enabled,
		logRequests:  t.flags.LogRequests,
		logResponses: t.flags.LogResponses,
	}
}

func (t *Tracer) enabledFor(name string) bool {
	if !t.flags.Enabled && !t.flags.LogRequests && !t.flags.LogResponses {
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
	logEnabled   bool
	logRequests  bool
	logResponses bool
}

func (t *tracedStore) List() ([]config.Descriptor, error) {
	t.logStart("List", "")
	descs, err := t.store.List()
	t.logEnd("List", "", err, descs)
	return descs, err
}

func (t *tracedStore) Marshal(desc config.Descriptor, value interface{}) error {
	key := fmt.Sprint(desc)
	t.logStart("Marshal", key)
	err := t.store.Marshal(desc, value)
	t.logEnd("Marshal", key, err, nil)
	return err
}

func (t *tracedStore) Unmarshal(name string, value interface{}) (config.Descriptor, error) {
	t.logStart("Unmarshal", name)
	desc, err := t.store.Unmarshal(name, value)
	t.logEnd("Unmarshal", name, err, value)
	return desc, err
}

func (t *tracedStore) Delete(desc config.Descriptor) error {
	key := fmt.Sprint(desc)
	t.logStart("Delete", key)
	err := t.store.Delete(desc)
	t.logEnd("Delete", key, err, nil)
	return err
}

func (t *tracedStore) logStart(operation string, key string) {
	if !t.logRequests {
		return
	}
	t.logLine(operation, key, "start", nil, nil)
}

func (t *tracedStore) logEnd(operation string, key string, err error, value interface{}) {
	if !t.logEnabled && !t.logResponses && err == nil {
		return
	}
	status := "success"
	if err != nil {
		status = "error"
	}
	if err != nil {
		t.logLine(operation, key, status, nil, err)
		return
	}
	if t.logResponses && value != nil {
		t.logLine(operation, key, status, value, nil)
		return
	}
	t.logLine(operation, key, status, nil, nil)
}

func (t *tracedStore) logLine(operation string, key string, status string, value interface{}, err error) {
	msg := "store namespace=" + t.name + " operation=" + operation + " status=" + status
	if key != "" {
		msg += " key=" + key
	}
	if err != nil {
		msg += fmt.Sprintf(" error=%v", err)
	}
	if value != nil {
		msg += fmt.Sprintf(" response=%+v", value)
	}
	t.log.Infof("%s", msg)
}

func storeName(app string, namespace []string) string {
	if app == "" && len(namespace) == 0 {
		return ""
	}
	return path.Join(append([]string{app}, namespace...)...)
}
