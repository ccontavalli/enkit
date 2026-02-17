package memory

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"sync"

	"github.com/ccontavalli/enkit/lib/config"
)

// Store is an in-memory config.Store that avoids serialization costs.
type Store struct {
	mu    sync.RWMutex
	items map[string]interface{}
}

// NewStore returns an in-memory Store.
func NewStore() *Store {
	return &Store{items: make(map[string]interface{})}
}

// List returns descriptors in sorted order, honoring list modifiers.
func (s *Store) List(mods ...config.ListModifier) ([]config.Descriptor, error) {
	opts := &config.ListOptions{}
	if err := config.ListModifiers(mods).Apply(opts); err != nil {
		return nil, err
	}

	s.mu.RLock()
	keys := make([]string, 0, len(s.items))
	for key := range s.items {
		keys = append(keys, key)
	}
	s.mu.RUnlock()

	sort.Strings(keys)
	descs := make([]config.Descriptor, len(keys))
	for i, key := range keys {
		descs[i] = config.Key(key)
	}

	return config.FinalizeList(s, descs, opts, 0)
}

// Marshal stores a reference to value under descriptor.
func (s *Store) Marshal(desc config.Descriptor, value interface{}) error {
	if desc == nil {
		return fmt.Errorf("API Usage Error - Store.Marshal must be passed a non-nil descriptor")
	}

	s.mu.Lock()
	s.items[desc.Key()] = value
	s.mu.Unlock()
	return nil
}

// Unmarshal copies the stored value into target.
func (s *Store) Unmarshal(desc config.Descriptor, target interface{}) (config.Descriptor, error) {
	if desc == nil {
		return nil, fmt.Errorf("API Usage Error - Store.Unmarshal must be passed a non-nil descriptor")
	}
	key := desc.Key()

	s.mu.RLock()
	value, ok := s.items[key]
	s.mu.RUnlock()
	if !ok {
		return config.Key(key), os.ErrNotExist
	}

	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Ptr || targetValue.IsNil() {
		return config.Key(key), fmt.Errorf("target must be a non-nil pointer")
	}
	storedValue := reflect.ValueOf(value)
	targetType := targetValue.Elem().Type()

	if storedValue.Kind() == reflect.Ptr && storedValue.Type().Elem().AssignableTo(targetType) {
		targetValue.Elem().Set(storedValue.Elem())
		return config.Key(key), nil
	}
	if storedValue.Type().AssignableTo(targetType) {
		targetValue.Elem().Set(storedValue)
		return config.Key(key), nil
	}
	return config.Key(key), fmt.Errorf("stored value type %s is not assignable to %s", storedValue.Type(), targetType)
}

// Delete removes the stored value.
func (s *Store) Delete(desc config.Descriptor) error {
	if desc == nil {
		return fmt.Errorf("API Usage Error - Store.Delete must be passed a non-nil descriptor")
	}
	key := desc.Key()

	s.mu.Lock()
	if _, ok := s.items[key]; !ok {
		s.mu.Unlock()
		return os.ErrNotExist
	}
	delete(s.items, key)
	s.mu.Unlock()
	return nil
}
