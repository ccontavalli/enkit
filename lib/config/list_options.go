package config

import "sort"

type ListOptions struct {
	Limit     int
	Offset    int
	StartFrom string
	Unmarshal UnmarshalSpec
}

type UnmarshalSpec interface {
	Target() interface{}
	ClearTarget()
	UnmarshalAndCall(desc Descriptor, data []byte, unmarshal func([]byte, interface{}) error) error
	Call(desc Descriptor, value interface{}) error
	NewSlice() interface{}
	SliceItem(slice interface{}, i int) interface{}
}

type UnmarshalSpecT[T any] struct {
	target *T
	fn     func(Descriptor, *T) error
}

func NewUnmarshalSpec[T any](target *T, fn func(Descriptor, *T) error) *UnmarshalSpecT[T] {
	return &UnmarshalSpecT[T]{target: target, fn: fn}
}

func (u *UnmarshalSpecT[T]) Target() interface{} {
	return u.target
}

func (u *UnmarshalSpecT[T]) ClearTarget() {
	var zero T
	*u.target = zero
}

func (u *UnmarshalSpecT[T]) UnmarshalAndCall(desc Descriptor, data []byte, unmarshal func([]byte, interface{}) error) error {
	if len(data) > 0 {
		if err := unmarshal(data, u.target); err != nil {
			return err
		}
	} else {
		u.ClearTarget()
	}
	return u.fn(desc, u.target)
}

func (u *UnmarshalSpecT[T]) Call(desc Descriptor, value interface{}) error {
	return u.fn(desc, value.(*T))
}

func (u *UnmarshalSpecT[T]) NewSlice() interface{} {
	var s []T
	return &s
}

func (u *UnmarshalSpecT[T]) SliceItem(slice interface{}, i int) interface{} {
	return &(*slice.(*[]T))[i]
}

type ListOptimized uint32

const (
	OptimizedStartFrom ListOptimized = 1 << iota
	OptimizedOffsetLimit
	OptimizedUnmarshal
)

type ListModifier func(*ListOptions) error

type ListModifiers []ListModifier

func (mods ListModifiers) Apply(opts *ListOptions) error {
	for _, mod := range mods {
		if err := mod(opts); err != nil {
			return err
		}
	}
	return nil
}

func WithListOptions(opts ListOptions) ListModifier {
	return func(target *ListOptions) error {
		target.Limit = opts.Limit
		target.Offset = opts.Offset
		target.StartFrom = opts.StartFrom
		if opts.Unmarshal != nil {
			target.Unmarshal = opts.Unmarshal
		}
		return nil
	}
}

func WithLimit(limit int) ListModifier {
	return func(opts *ListOptions) error {
		opts.Limit = limit
		return nil
	}
}

func WithOffset(offset int) ListModifier {
	return func(opts *ListOptions) error {
		opts.Offset = offset
		return nil
	}
}

func WithStartFrom(desc Descriptor) ListModifier {
	return func(opts *ListOptions) error {
		if desc == nil {
			return nil
		}
		opts.StartFrom = desc.Key()
		return nil
	}
}

func Unmarshal[T any](target *T, fn func(Descriptor, *T) error) ListModifier {
	return func(opts *ListOptions) error {
		opts.Unmarshal = NewUnmarshalSpec(target, fn)
		return nil
	}
}

func ApplyStartFrom(descs []Descriptor, start string) []Descriptor {
	if start == "" || len(descs) == 0 {
		return descs
	}
	index := sort.Search(len(descs), func(i int) bool {
		return descs[i].Key() >= start
	})
	return descs[index:]
}

func ApplyOffsetLimit(descs []Descriptor, offset int, limit int) []Descriptor {
	start := offset
	if start > len(descs) {
		start = len(descs)
	}
	end := len(descs)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	return descs[start:end]
}

// Apply applies list options, honoring the optimization bitmask.
func (opts *ListOptions) Apply(descs []Descriptor, optimized ListOptimized) []Descriptor {
	if optimized&OptimizedStartFrom == 0 {
		descs = ApplyStartFrom(descs, opts.StartFrom)
	}
	if optimized&OptimizedOffsetLimit == 0 {
		descs = ApplyOffsetLimit(descs, opts.Offset, opts.Limit)
	}
	return descs
}

// Finalize applies list options and performs unmarshal fallbacks.
func (opts *ListOptions) Finalize(store Store, descs []Descriptor, optimized ListOptimized) ([]Descriptor, error) {
	descs = opts.Apply(descs, optimized)

	if opts.Unmarshal != nil && optimized&OptimizedUnmarshal == 0 {
		for _, desc := range descs {
			opts.Unmarshal.ClearTarget()
			if _, err := store.Unmarshal(desc, opts.Unmarshal.Target()); err != nil {
				return nil, err
			}
			if err := opts.Unmarshal.Call(desc, opts.Unmarshal.Target()); err != nil {
				return nil, err
			}
		}
		return []Descriptor{}, nil
	}

	return descs, nil
}
