package typed

import "github.com/ccontavalli/enkit/lib/config"

// Store wraps a config.Store and provides typed helpers for Marshal/Unmarshal and List.
type Store[T any] struct {
	config.Store
}

// Wrap wraps a config.Store into a typed store.
func Wrap[T any](store config.Store) Store[T] {
	return Store[T]{Store: store}
}

// OpenAs opens a namespace and returns a typed wrapper.
func OpenAs[T any](ws config.StoreWorkspace, app string, namespace ...string) (Store[T], error) {
	store, err := ws.Open(app, namespace...)
	if err != nil {
		return Store[T]{}, err
	}
	return Wrap[T](store), nil
}

// List forwards to the underlying store.
func (s Store[T]) List(mods ...config.ListModifier) ([]config.Descriptor, error) {
	return s.Store.List(mods...)
}

// Marshal stores the value into the descriptor.
func (s Store[T]) Marshal(desc config.Descriptor, value T) error {
	return s.Store.Marshal(desc, value)
}

// Unmarshal reads into target.
func (s Store[T]) Unmarshal(desc config.Descriptor, target *T) (config.Descriptor, error) {
	return s.Store.Unmarshal(desc, target)
}

// Delete removes the descriptor.
func (s Store[T]) Delete(desc config.Descriptor) error {
	return s.Store.Delete(desc)
}

// Close releases underlying resources.
func (s Store[T]) Close() error {
	return s.Store.Close()
}

// Get returns the unmarshaled value by key.
func (s Store[T]) Get(desc config.Descriptor) (T, config.Descriptor, error) {
	var out T
	got, err := s.Store.Unmarshal(desc, &out)
	return out, got, err
}

// Each lists entries and invokes fn for each unmarshaled value.
func (s Store[T]) Each(fn func(config.Descriptor, *T) error, mods ...config.ListModifier) error {
	var target T
	mods = append(mods, config.Unmarshal(&target, fn))
	_, err := s.Store.List(mods...)
	return err
}

// Values lists entries and returns the unmarshaled values.
func (s Store[T]) Values(mods ...config.ListModifier) ([]T, error) {
	var out []T
	var target T
	mods = append(mods, config.Unmarshal(&target, func(_ config.Descriptor, value *T) error {
		out = append(out, *value)
		return nil
	}))
	if _, err := s.Store.List(mods...); err != nil {
		return nil, err
	}
	return out, nil
}
