package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/ccontavalli/enkit/lib/config/marshal"
	"github.com/ccontavalli/enkit/lib/multierror"
)

type MultiFormat struct {
	loader     Loader
	marshaller []marshal.FileMarshaller
	keyCodec   KeyCodec
}

// OpenMulti returns a Store backed by the provided Loader.
func OpenMulti(loader Loader, marshaller ...marshal.FileMarshaller) *MultiFormat {
	return OpenMultiWithOptions(loader, marshaller)
}

// OpenMultiWithOptions returns a Store backed by the provided Loader and options.
func OpenMultiWithOptions(loader Loader, marshaller []marshal.FileMarshaller, opts ...StoreOption) *MultiFormat {
	if len(marshaller) <= 0 {
		marshaller = marshal.Known
	}
	options := applyStoreOptions(opts...)
	return &MultiFormat{loader: loader, marshaller: marshaller, keyCodec: options.keyCodec}
}

type multiWorkspace struct {
	workspace  LoaderWorkspace
	marshaller []marshal.FileMarshaller
	options    []StoreOption
}

func (m *multiWorkspace) Open(name string, namespace ...string) (Store, error) {
	loader, err := m.workspace.Open(name, namespace...)
	if err != nil {
		return nil, err
	}
	return OpenMultiWithOptions(loader, m.marshaller, m.options...), nil
}

func (m *multiWorkspace) Explore(name string, namespace ...string) (Explorer, error) {
	return m.workspace.Explore(name, namespace...)
}

func (m *multiWorkspace) Close() error {
	return m.workspace.Close()
}

// NewMulti returns a StoreWorkspace that wraps a LoaderWorkspace with a Multi store.
func NewMulti(workspace LoaderWorkspace, marshaller ...marshal.FileMarshaller) StoreWorkspace {
	return &multiWorkspace{
		workspace:  workspace,
		marshaller: marshaller,
	}
}

// List returns the list of configs the loader knows about.
//
// If a config exists in multiple formats, list will return all known formats.
// The names returned are usable to be passed directly to Unmarshal, but may
// contain an extension that was not added to begin with.
//
// For example:
//
//	mf.Marshal(Key("config"), Config{})
//	mf.Marshal(FormatKey("config", marshal.Json), Config{})
//
// will results in a "config.toml" file (default preferred format) and
// "config.json" file being created.
//
// List() will return "config.toml" and "config.json" both.
//
// Unmarshal() can be called with Unmarshal(Key("config")), which will result in
// the "config.toml" file being parsed (the preferred format). To target a
// specific format, use Unmarshal(FormatKey("config", marshal.Json)).
//
// In general, the value returned by List is guaranteed to be usable with
// Unmarshal, but may not match the value that was passed to Marshal before.
func (ss *MultiFormat) List(mods ...ListModifier) ([]Descriptor, error) {
	opts := &ListOptions{}
	if err := ListModifiers(mods).Apply(opts); err != nil {
		return nil, err
	}
	loaderOpts := *opts
	loaderOpts.Unmarshal = nil
	if opts.StartFrom != "" {
		loaderOpts.StartFrom = ss.encodeKey(opts.StartFrom)
	}
	if opts.Unmarshal != nil {
		loaderOpts.Data = func(desc Descriptor, data []byte) error {
			name := desc.Key()
			d := newMultiDescriptorFromPath(name, ss.marshaller, ss.keyCodec)
			return opts.Unmarshal.UnmarshalAndCall(d, data, d.m.Unmarshal)
		}
	} else if opts.Data != nil {
		loaderOpts.Data = func(desc Descriptor, data []byte) error {
			name := desc.Key()
			d := newMultiDescriptorFromPath(name, ss.marshaller, ss.keyCodec)
			return opts.Data(d, data)
		}
	}
	list, err := ss.loader.List(WithListOptions(loaderOpts))
	if err != nil {
		return nil, err
	}
	if loaderOpts.Data != nil && len(list) == 0 {
		return []Descriptor{}, nil
	}
	descs := make([]Descriptor, len(list))
	for i, name := range list {
		descs[i] = newMultiDescriptorFromPath(name, ss.marshaller, ss.keyCodec)
	}
	return opts.Finalize(ss, descs, OptimizedStartFrom|OptimizedOffsetLimit|OptimizedUnmarshal)
}

func (ss *MultiFormat) Marshal(desc Descriptor, value interface{}) error {
	name, marshaller, err := ss.parseDesc(desc)
	if err != nil {
		return err
	}
	if marshaller == nil {
		marshaller = ss.marshaller[0]
		name = ss.pathForKey(name, marshaller)
	}

	data, err := marshaller.Marshal(value)
	if err != nil {
		return err
	}
	return ss.loader.Write(name, data)
}

func (ss *MultiFormat) parseDesc(desc Descriptor) (string, marshal.FileMarshaller, error) {
	var name string
	var marshaller marshal.FileMarshaller
	switch t := desc.(type) {
	case Key:
		name = string(t)
	case *multiDescriptor:
		name = ss.pathForKey(t.k, t.m)
		marshaller = t.m
	default:
		return "", nil, fmt.Errorf("API Usage Error - MultiFormat.Marshal passed an unknown descriptor type - %#v", desc)
	}

	return name, marshaller, nil
}

func (ss *MultiFormat) Delete(desc Descriptor) error {
	name, marshaller, err := ss.parseDesc(desc)
	if err != nil {
		return err
	}

	if marshaller != nil {
		return ss.loader.Delete(name)
	}

	nonexisting := 0
	var errors []error
	for _, marshaller := range ss.marshaller {
		fullname := ss.pathForKey(name, marshaller)
		err := ss.loader.Delete(fullname)
		if err == nil {
			continue
		}

		if os.IsNotExist(err) {
			nonexisting += 1
			continue
		}

		errors = append(errors, fmt.Errorf("could not delete %s: %w", fullname, err))
	}

	if nonexisting == len(ss.marshaller) {
		return os.ErrNotExist
	}
	return multierror.New(errors)
}

func (ss *MultiFormat) Close() error {
	return ss.loader.Close()
}

func (ss *MultiFormat) encodeKey(name string) string {
	return ss.keyCodec.Encode(name)
}

func (ss *MultiFormat) decodeKey(name string) string {
	return ss.keyCodec.Decode(name)
}

func (ss *MultiFormat) pathForKey(key string, m marshal.FileMarshaller) string {
	encoded := ss.encodeKey(key)
	if m == nil {
		return encoded
	}
	return encoded + "." + m.Extension()
}

// FormatKey returns a descriptor that targets a specific format for a key.
func FormatKey(key string, m marshal.FileMarshaller) Descriptor {
	return &multiDescriptor{m: m, k: key}
}

type multiDescriptor struct {
	m marshal.FileMarshaller
	k string
}

func (d *multiDescriptor) Key() string {
	return d.k
}

func (ss *MultiFormat) Unmarshal(desc Descriptor, value interface{}) (Descriptor, error) {
	if desc == nil {
		return nil, fmt.Errorf("API Usage Error - MultiFormat.Unmarshal must be passed a non-nil descriptor")
	}
	load := func(m marshal.FileMarshaller, path string) (Descriptor, error) {
		data, err := ss.loader.Read(path)
		if err != nil {
			return nil, err
		}
		key := ss.decodeKey(strings.TrimSuffix(path, "."+m.Extension()))
		descriptor := &multiDescriptor{m: m, k: key}
		if len(data) <= 0 {
			return descriptor, nil
		}
		return descriptor, m.Unmarshal(data, value)
	}

	switch t := desc.(type) {
	case Key:
		key := string(t)
		var err error
		var result Descriptor
		for _, m := range ss.marshaller {
			path := ss.pathForKey(key, m)
			result, err = load(m, path)
			if err == nil {
				return result, nil
			}
		}
		return result, err
	case *multiDescriptor:
		path := ss.pathForKey(t.k, t.m)
		return load(t.m, path)
	default:
		return nil, fmt.Errorf("API Usage Error - MultiFormat.Unmarshal passed an unknown descriptor type - %#v", desc)
	}
}

func newMultiDescriptorFromPath(path string, marshaller []marshal.FileMarshaller, codec KeyCodec) *multiDescriptor {
	m := marshal.FileMarshallers(marshaller).ByFilePathExtension(path)
	key := path
	if m != nil {
		key = strings.TrimSuffix(path, "."+m.Extension())
	}
	key = codec.Decode(key)
	return &multiDescriptor{m: m, k: key}
}
