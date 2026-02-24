package config

import (
	"fmt"
	"strings"

	"github.com/ccontavalli/enkit/lib/config/marshal"
)

type SimpleStore struct {
	loader     Loader
	marshaller marshal.FileMarshaller
	keyCodec   KeyCodec
}

// OpenSimple returns a Store backed by the provided Loader.
func OpenSimple(loader Loader, marshaller marshal.FileMarshaller) *SimpleStore {
	return OpenSimpleWithOptions(loader, marshaller)
}

// OpenSimpleWithOptions returns a Store backed by the provided Loader and options.
func OpenSimpleWithOptions(loader Loader, marshaller marshal.FileMarshaller, opts ...StoreOption) *SimpleStore {
	options := applyStoreOptions(opts...)
	return &SimpleStore{
		loader:     loader,
		marshaller: marshaller,
		keyCodec:   options.keyCodec,
	}
}

type simpleWorkspace struct {
	workspace  LoaderWorkspace
	marshaller marshal.FileMarshaller
	options    []StoreOption
}

func (s *simpleWorkspace) Open(name string, namespace ...string) (Store, error) {
	loader, err := s.workspace.Open(name, namespace...)
	if err != nil {
		return nil, err
	}
	return OpenSimpleWithOptions(loader, s.marshaller, s.options...), nil
}

func (s *simpleWorkspace) Explore(name string, namespace ...string) (Explorer, error) {
	return s.workspace.Explore(name, namespace...)
}

func (s *simpleWorkspace) Close() error {
	return s.workspace.Close()
}

// NewSimple returns a StoreWorkspace that wraps a LoaderWorkspace with a Simple store.
func NewSimple(workspace LoaderWorkspace, marshaller marshal.FileMarshaller, opts ...StoreOption) StoreWorkspace {
	return &simpleWorkspace{
		workspace:  workspace,
		marshaller: marshaller,
		options:    opts,
	}
}

func (ss *SimpleStore) List(mods ...ListModifier) ([]Descriptor, error) {
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
			key := strings.TrimSuffix(name, "."+ss.marshaller.Extension())
			key = ss.decodeKey(key)
			return opts.Unmarshal.UnmarshalAndCall(Key(key), data, ss.marshaller.Unmarshal)
		}
	} else if opts.Data != nil {
		loaderOpts.Data = func(desc Descriptor, data []byte) error {
			name := desc.Key()
			key := strings.TrimSuffix(name, "."+ss.marshaller.Extension())
			key = ss.decodeKey(key)
			return opts.Data(Key(key), data)
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
		key := strings.TrimSuffix(name, "."+ss.marshaller.Extension())
		key = ss.decodeKey(key)
		descs[i] = Key(key)
	}
	return opts.Finalize(ss, descs, OptimizedStartFrom|OptimizedOffsetLimit|OptimizedUnmarshal)
}

func (ss *SimpleStore) Marshal(desc Descriptor, value interface{}) error {
	if desc == nil {
		return fmt.Errorf("API Usage Error - SimpleStore.Marshal must be passed a non-nil descriptor")
	}
	name := ss.pathForKey(desc.Key())
	data, err := ss.marshaller.Marshal(value)
	if err != nil {
		return err
	}
	return ss.loader.Write(name, data)
}

func (ss *SimpleStore) Unmarshal(desc Descriptor, value interface{}) (Descriptor, error) {
	if desc == nil {
		return nil, fmt.Errorf("API Usage Error - SimpleStore.Unmarshal must be passed a non-nil descriptor")
	}
	key := desc.Key()
	path := ss.pathForKey(key)
	data, err := ss.loader.Read(path)
	if err != nil {
		return Key(key), err
	}
	if len(data) <= 0 {
		return Key(key), nil
	}
	return Key(key), ss.marshaller.Unmarshal(data, value)
}

func (ss *SimpleStore) Delete(desc Descriptor) error {
	if desc == nil {
		return fmt.Errorf("API Usage Error - SimpleStore.Delete must be passed a non-nil descriptor")
	}
	name := ss.pathForKey(desc.Key())
	return ss.loader.Delete(name)
}

func (ss *SimpleStore) Close() error {
	return ss.loader.Close()
}

func (ss *SimpleStore) pathForKey(key string) string {
	encoded := ss.encodeKey(key)
	return encoded + "." + ss.marshaller.Extension()
}

func (ss *SimpleStore) encodeKey(name string) string {
	return ss.keyCodec.Encode(name)
}

func (ss *SimpleStore) decodeKey(name string) string {
	return ss.keyCodec.Decode(name)
}
