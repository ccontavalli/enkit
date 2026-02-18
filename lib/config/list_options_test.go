package config

import (
	"strconv"
	"testing"
)

type listTestStore struct{}

func (s listTestStore) List(mods ...ListModifier) ([]Descriptor, error) { return nil, nil }
func (s listTestStore) Marshal(Descriptor, interface{}) error           { return nil }
func (s listTestStore) Unmarshal(desc Descriptor, value interface{}) (Descriptor, error) {
	return desc, nil
}
func (s listTestStore) Delete(Descriptor) error { return nil }
func (s listTestStore) Close() error            { return nil }

func makeDescs(n int) []Descriptor {
	out := make([]Descriptor, n)
	for i := 0; i < n; i++ {
		out[i] = Key("k" + strconv.Itoa(i))
	}
	return out
}

func TestFinalizeListOffsetLimit(t *testing.T) {
	opts := &ListOptions{}
	if err := ListModifiers([]ListModifier{
		WithOffset(2),
		WithLimit(3),
	}).Apply(opts); err != nil {
		t.Fatalf("apply options: %v", err)
	}

	descs := makeDescs(10)
	got, err := opts.Finalize(listTestStore{}, descs, 0)
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	want := []Descriptor{Key("k2"), Key("k3"), Key("k4")}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i].Key() != want[i].Key() {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestFinalizeListStartFrom(t *testing.T) {
	opts := &ListOptions{}
	if err := ListModifiers([]ListModifier{
		WithStartFrom(Key("k3")),
		WithOffset(1),
		WithLimit(2),
	}).Apply(opts); err != nil {
		t.Fatalf("apply options: %v", err)
	}

	descs := makeDescs(8)
	got, err := opts.Finalize(listTestStore{}, descs, 0)
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	want := []Descriptor{Key("k4"), Key("k5")}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i].Key() != want[i].Key() {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestFinalizeListUnmarshalFallback(t *testing.T) {
	var seen []string
	target := &struct{}{}
	opts := &ListOptions{}
	if err := ListModifiers([]ListModifier{
		WithLimit(2),
		Unmarshal(target, func(desc Descriptor, _ *struct{}) error {
			seen = append(seen, desc.Key())
			return nil
		}),
	}).Apply(opts); err != nil {
		t.Fatalf("apply options: %v", err)
	}
	descs := makeDescs(4)
	got, err := opts.Finalize(listTestStore{}, descs, 0)
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty descs, got %v", got)
	}
	if len(seen) != 2 || seen[0] != "k0" || seen[1] != "k1" {
		t.Fatalf("unexpected callbacks %v", seen)
	}
}
