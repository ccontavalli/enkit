package config

import (
	"fmt"
	"github.com/ccontavalli/enkit/lib/config/marshal"
)

type SimpleStore struct {
	loader     Loader
	marshaller marshal.Marshaller
}

func NewSimple(loader Loader, marshaller marshal.Marshaller) *SimpleStore {
	return &SimpleStore{loader: loader, marshaller: marshaller}
}

func (ss *SimpleStore) List() ([]Descriptor, error) {
	list, err := ss.loader.List()
	if err != nil {
		return nil, err
	}
	descs := make([]Descriptor, len(list))
	for i, name := range list {
		descs[i] = Key(name)
	}
	return descs, nil
}

func (ss *SimpleStore) Marshal(desc Descriptor, value interface{}) error {
	if desc == nil {
		return fmt.Errorf("API Usage Error - SimpleStore.Marshal must be passed a non-nil descriptor")
	}
	name := desc.Key()
	data, err := ss.marshaller.Marshal(value)
	if err != nil {
		return err
	}
	return ss.loader.Write(name, data)
}

func (ss *SimpleStore) Unmarshal(name string, value interface{}) (Descriptor, error) {
	data, err := ss.loader.Read(name)
	if err != nil {
		return Key(name), err
	}
	if len(data) <= 0 {
		return Key(name), nil
	}
	return Key(name), ss.marshaller.Unmarshal(data, value)
}

func (ss *SimpleStore) Delete(desc Descriptor) error {
	if desc == nil {
		return fmt.Errorf("API Usage Error - SimpleStore.Delete must be passed a non-nil descriptor")
	}
	name := desc.Key()
	return ss.loader.Delete(name)
}
