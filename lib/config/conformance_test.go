package config_test

import (
	"fmt"
	"math/rand"
	"os"
	"sort"
	"testing"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/config/bbolt"
	"github.com/ccontavalli/enkit/lib/config/directory"
	"github.com/ccontavalli/enkit/lib/config/marshal"
	"github.com/ccontavalli/enkit/lib/config/memory"
	"github.com/ccontavalli/enkit/lib/config/sqlite"
	"github.com/stretchr/testify/assert"
)

type conformanceConfig struct {
	Value string `json:"value"`
}

type storeFactory struct {
	name string
	open func(t *testing.T) (config.Store, func())
}

func TestStoreConformance(t *testing.T) {
	factories := []storeFactory{
		{
			name: "simple-json",
			open: func(t *testing.T) (config.Store, func()) {
				t.Helper()
				dir := t.TempDir()
				loader, err := directory.OpenDir(dir)
				if err != nil {
					t.Fatalf("open dir: %v", err)
				}
				return config.NewSimple(loader, marshal.Json), func() {}
			},
		},
		{
			name: "multi-json",
			open: func(t *testing.T) (config.Store, func()) {
				t.Helper()
				dir := t.TempDir()
				loader, err := directory.OpenDir(dir)
				if err != nil {
					t.Fatalf("open dir: %v", err)
				}
				return config.NewMulti(loader, marshal.Json), func() {}
			},
		},
		{
			name: "sqlite",
			open: func(t *testing.T) (config.Store, func()) {
				t.Helper()
				tmp, err := os.CreateTemp("", "config-conformance-sqlite-*.db")
				if err != nil {
					t.Fatalf("temp db: %v", err)
				}
				path := tmp.Name()
				if err := tmp.Close(); err != nil {
					_ = os.Remove(path)
					t.Fatalf("close db: %v", err)
				}
				db, err := sqlite.New(sqlite.WithPath(path))
				if err != nil {
					_ = os.Remove(path)
					t.Fatalf("open sqlite: %v", err)
				}
				store, err := db.Open("app", "ns")
				if err != nil {
					_ = db.Close()
					_ = os.Remove(path)
					t.Fatalf("open store: %v", err)
				}
				cleanup := func() {
					_ = db.Close()
					_ = os.Remove(path)
				}
				return store, cleanup
			},
		},
		{
			name: "bbolt",
			open: func(t *testing.T) (config.Store, func()) {
				t.Helper()
				tmp, err := os.CreateTemp("", "config-conformance-bbolt-*.db")
				if err != nil {
					t.Fatalf("temp db: %v", err)
				}
				path := tmp.Name()
				if err := tmp.Close(); err != nil {
					_ = os.Remove(path)
					t.Fatalf("close db: %v", err)
				}
				db, err := bbolt.New(bbolt.WithPath(path))
				if err != nil {
					_ = os.Remove(path)
					t.Fatalf("open bbolt: %v", err)
				}
				store, err := db.Open("app", "ns")
				if err != nil {
					_ = db.Close()
					_ = os.Remove(path)
					t.Fatalf("open store: %v", err)
				}
				cleanup := func() {
					_ = db.Close()
					_ = os.Remove(path)
				}
				return store, cleanup
			},
		},
		{
			name: "memory",
			open: func(t *testing.T) (config.Store, func()) {
				t.Helper()
				return memory.NewStore(), func() {}
			},
		},
		{
			name: "memory-loader-json",
			open: func(t *testing.T) (config.Store, func()) {
				t.Helper()
				loader := memory.New()
				return config.NewSimple(loader, marshal.Json), func() {}
			},
		},
	}

	for _, factory := range factories {
		factory := factory
		t.Run(factory.name, func(t *testing.T) {
			store, cleanup := factory.open(t)
			defer cleanup()

			keys := make([]string, 1000)
			for i := range keys {
				keys[i] = fmt.Sprintf("k%04d", i)
			}
			seeded := make([]string, len(keys))
			copy(seeded, keys)
			rng := rand.New(rand.NewSource(1337))
			rng.Shuffle(len(seeded), func(i, j int) {
				seeded[i], seeded[j] = seeded[j], seeded[i]
			})
			for _, key := range seeded {
				assert.NoError(t, store.Marshal(config.Key(key), conformanceConfig{Value: key}), "marshal %q", key)
			}

			descs, err := store.List()
			assert.NoError(t, err)
			got := make([]string, len(descs))
			for i, desc := range descs {
				got[i] = desc.Key()
			}
			want := append([]string(nil), keys...)
			sort.Strings(want)
			assert.Equal(t, want, got)

			var loaded conformanceConfig
			_, err = store.Unmarshal(config.Key(keys[0]), &loaded)
			assert.NoError(t, err)
			assert.Equal(t, keys[0], loaded.Value)

			assert.NoError(t, store.Delete(config.Key(keys[0])))
			_, err = store.Unmarshal(config.Key(keys[0]), &loaded)
			assert.True(t, err != nil && os.IsNotExist(err), "expected not exist, got %v", err)
			err = store.Delete(config.Key(keys[0]))
			assert.True(t, err != nil && os.IsNotExist(err), "expected not exist delete, got %v", err)
			remaining := want[1:]

			var seen []string
			target := &conformanceConfig{}
			list, err := store.List(config.Unmarshal(target, func(desc config.Descriptor, value *conformanceConfig) error {
				seen = append(seen, desc.Key()+"="+value.Value)
				return nil
			}))
			assert.NoError(t, err)
			assert.Len(t, list, 0)
			assert.Len(t, seen, len(remaining))

			assertOffsetLimit(t, store, remaining, 0, 0)
			assertOffsetLimit(t, store, remaining, 0, 10)
			assertOffsetLimit(t, store, remaining, len(remaining)-1, 1)
			assertOffsetLimit(t, store, remaining, len(remaining), 1)
			assertOffsetLimit(t, store, remaining, len(remaining)+10, 5)
			assertOffsetLimit(t, store, remaining, 10, 0)
			assertOffsetLimit(t, store, remaining, 10, 5)
			assertOffsetLimit(t, store, remaining, 10, len(remaining))

			assertStartFrom(t, store, remaining, remaining[100], 0, 0)
			assertStartFrom(t, store, remaining, remaining[100], 5, 10)
			assertStartFrom(t, store, remaining, remaining[len(remaining)-1], 0, 5)

			assertOffsetLimitUnmarshal(t, store, remaining, 0, 10)
			assertOffsetLimitUnmarshal(t, store, remaining, len(remaining)-5, 10)
			assertOffsetLimitUnmarshal(t, store, remaining, len(remaining), 5)
			assertOffsetLimitUnmarshal(t, store, remaining, len(remaining)+5, 5)

			assertStartFromUnmarshal(t, store, remaining, remaining[200], 3, 7)
			assertStartFromUnmarshal(t, store, remaining, remaining[len(remaining)-1], 0, 5)
		})
	}
}

func assertOffsetLimit(t *testing.T, store config.Store, all []string, offset int, limit int) {
	t.Helper()
	descs, err := store.List(config.WithOffset(offset), config.WithLimit(limit))
	assert.NoError(t, err)
	got := make([]string, len(descs))
	for i, desc := range descs {
		got[i] = desc.Key()
	}
	want := sliceOffsetLimit(all, offset, limit)
	assert.Equal(t, want, got)
}

func assertStartFrom(t *testing.T, store config.Store, all []string, start string, offset int, limit int) {
	t.Helper()
	descs, err := store.List(config.WithStartFrom(config.Key(start)), config.WithOffset(offset), config.WithLimit(limit))
	assert.NoError(t, err)
	got := make([]string, len(descs))
	for i, desc := range descs {
		got[i] = desc.Key()
	}
	want := sliceOffsetLimit(sliceStartFrom(all, start), offset, limit)
	assert.Equal(t, want, got)
}

func assertOffsetLimitUnmarshal(t *testing.T, store config.Store, all []string, offset int, limit int) {
	t.Helper()
	var seen []string
	target := &conformanceConfig{}
	list, err := store.List(
		config.WithOffset(offset),
		config.WithLimit(limit),
		config.Unmarshal(target, func(desc config.Descriptor, value *conformanceConfig) error {
			seen = append(seen, desc.Key())
			return nil
		}),
	)
	assert.NoError(t, err)
	assert.Len(t, list, 0)
	want := sliceOffsetLimit(all, offset, limit)
	assert.Equal(t, want, append([]string{}, seen...))
}

func assertStartFromUnmarshal(t *testing.T, store config.Store, all []string, start string, offset int, limit int) {
	t.Helper()
	var seen []string
	target := &conformanceConfig{}
	list, err := store.List(
		config.WithStartFrom(config.Key(start)),
		config.WithOffset(offset),
		config.WithLimit(limit),
		config.Unmarshal(target, func(desc config.Descriptor, value *conformanceConfig) error {
			seen = append(seen, desc.Key())
			return nil
		}),
	)
	assert.NoError(t, err)
	assert.Len(t, list, 0)
	want := sliceOffsetLimit(sliceStartFrom(all, start), offset, limit)
	assert.Equal(t, want, append([]string{}, seen...))
}

func sliceOffsetLimit(all []string, offset int, limit int) []string {
	if offset < 0 {
		offset = 0
	}
	if offset > len(all) {
		offset = len(all)
	}
	end := len(all)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return append([]string{}, all[offset:end]...)
}

func sliceStartFrom(all []string, start string) []string {
	index := sort.SearchStrings(all, start)
	return append([]string{}, all[index:]...)
}
