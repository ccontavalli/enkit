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

func NewSimple(loader Loader, marshaller marshal.FileMarshaller) *SimpleStore {
	return NewSimpleWithOptions(loader, marshaller)
}

func NewSimpleWithOptions(loader Loader, marshaller marshal.FileMarshaller, opts ...StoreOption) *SimpleStore {
	options := applyStoreOptions(opts...)
	return &SimpleStore{
		loader:     loader,
		marshaller: marshaller,
		keyCodec:   options.keyCodec,
	}
}

func (ss *SimpleStore) List() ([]Descriptor, error) {
	list, err := ss.loader.List()
	if err != nil {
		return nil, err
	}
	descs := make([]Descriptor, len(list))
	for i, name := range list {
		key := strings.TrimSuffix(name, "."+ss.marshaller.Extension())
		key = ss.decodeKey(key)
		descs[i] = Key(key)
	}
	return descs, nil
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
