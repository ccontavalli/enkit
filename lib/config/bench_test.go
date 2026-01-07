package config_test

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ccontavalli/enkit/lib/config"
	configbbolt "github.com/ccontavalli/enkit/lib/config/bbolt"
	"github.com/ccontavalli/enkit/lib/config/directory"
	"github.com/ccontavalli/enkit/lib/config/marshal"
	"github.com/ccontavalli/enkit/lib/config/sqlite"
)

type benchConfig struct {
	Value string `json:"value"`
}

var benchRecordCounts = []int{1, 100, 1000}
var benchParallelism = []int{1, 4}

type backend struct {
	name string
	open func(tb testing.TB) (config.Store, func(), error)
}

type op struct {
	name string
	run  func(b *testing.B, backend backend, parallelism int, store config.Store, keys []string, miss func(int) string)
}

func BenchmarkConfigStore(b *testing.B) {
	recordCounts := benchIntsFromEnv(b, "ENKIT_CONFIG_BENCH_COUNTS", benchRecordCounts)
	parallelismValues := benchIntsFromEnv(b, "ENKIT_CONFIG_BENCH_PARALLELISM", benchParallelism)

	for _, backend := range benchBackends() {
		b.Run(backend.name, func(b *testing.B) {
			for _, operation := range benchOps() {
				b.Run(operation.name, func(b *testing.B) {
					for _, count := range recordCounts {
						b.Run(fmt.Sprintf("n=%d", count), func(b *testing.B) {
							for _, parallelism := range parallelismValues {
								b.Run(fmt.Sprintf("p=%d", parallelism), func(b *testing.B) {
									store, cleanup, err := backend.open(b)
									if err != nil {
										b.Fatal(err)
									}
									defer cleanup()

									keys := benchKeys(count)
									populateStore(b, store, keys)

									b.ResetTimer()
									b.SetParallelism(parallelism)
									operation.run(b, backend, parallelism, store, keys, benchMissingKey)
								})
							}
						})
					}
				})
			}
		})
	}
}

func benchOps() []op {
	return []op{
		{
			name: "List",
			run: func(b *testing.B, backend backend, parallelism int, store config.Store, keys []string, miss func(int) string) {
				for i := 0; i < b.N; i++ {
					if _, err := store.List(); err != nil {
						b.Fatal(err)
					}
				}
			},
		},
		{
			name: "Get",
			run: func(b *testing.B, backend backend, parallelism int, store config.Store, keys []string, miss func(int) string) {
				var counter uint64
				b.RunParallel(func(pb *testing.PB) {
					var value benchConfig
					for pb.Next() {
						index := int(atomic.AddUint64(&counter, 1)-1) % len(keys)
						if _, err := store.Unmarshal(keys[index], &value); err != nil {
							b.Fatal(err)
						}
					}
				})
			},
		},
		{
			name: "Store",
			run: func(b *testing.B, backend backend, parallelism int, store config.Store, keys []string, miss func(int) string) {
				if strings.HasPrefix(backend.name, "sqlite") {
					for i := 0; i < b.N; i++ {
						index := i % len(keys)
						if err := store.Marshal(config.Key(keys[index]), benchConfig{Value: "value"}); err != nil {
							b.Fatal(err)
						}
					}
					return
				}

				var counter uint64
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						index := int(atomic.AddUint64(&counter, 1)-1) % len(keys)
						if err := storeMarshalWithRetry(backend.name, store, keys[index]); err != nil {
							b.Fatal(err)
						}
					}
				})
			},
		},
		{
			name: "LookupMissing",
			run: func(b *testing.B, backend backend, parallelism int, store config.Store, keys []string, miss func(int) string) {
				var counter uint64
				b.RunParallel(func(pb *testing.PB) {
					var value benchConfig
					for pb.Next() {
						index := atomic.AddUint64(&counter, 1) - 1
						key := miss(int(index))
						if _, err := store.Unmarshal(key, &value); err == nil || !os.IsNotExist(err) {
							b.Fatalf("expected not-exist error, got %v", err)
						}
					}
				})
			},
		},
	}
}

func benchBackends() []backend {
	return []backend{
		{
			name: "directory",
			open: func(tb testing.TB) (config.Store, func(), error) {
				tb.Helper()
				dir, err := os.MkdirTemp("", "config-bench-dir")
				if err != nil {
					return nil, nil, err
				}

				loader, err := directory.OpenDir(dir, "app", "ns")
				if err != nil {
					os.RemoveAll(dir)
					return nil, nil, err
				}

				store := config.NewSimple(loader, marshal.Json)
				cleanup := func() {
					_ = os.RemoveAll(dir)
				}
				return store, cleanup, nil
			},
		},
		{
			name: "sqlite-store",
			open: func(tb testing.TB) (config.Store, func(), error) {
				tb.Helper()
				tmp, err := os.CreateTemp("", "config-bench-sqlite-*.db")
				if err != nil {
					return nil, nil, err
				}
				path := tmp.Name()
				if err := tmp.Close(); err != nil {
					os.Remove(path)
					return nil, nil, err
				}

				db, err := sqlite.New(
					sqlite.WithPath(path),
					sqlite.WithJournalMode("WAL"),
					sqlite.WithSynchronous("NORMAL"),
					sqlite.WithBusyTimeout(5000),
					sqlite.WithMaxOpenConns(8),
					sqlite.WithMaxIdleConns(8),
				)
				if err != nil {
					os.Remove(path)
					return nil, nil, err
				}

				store, err := db.Open("app", "ns")
				if err != nil {
					db.Close()
					os.Remove(path)
					return nil, nil, err
				}

				cleanup := func() {
					_ = db.Close()
					_ = os.Remove(path)
				}
				return store, cleanup, nil
			},
		},
		{
			name: "sqlite-multi",
			open: func(tb testing.TB) (config.Store, func(), error) {
				tb.Helper()
				tmp, err := os.CreateTemp("", "config-bench-sqlite-multi-*.db")
				if err != nil {
					return nil, nil, err
				}
				path := tmp.Name()
				if err := tmp.Close(); err != nil {
					os.Remove(path)
					return nil, nil, err
				}

				db, err := sqlite.NewMulti(
					sqlite.WithPath(path),
					sqlite.WithJournalMode("WAL"),
					sqlite.WithSynchronous("NORMAL"),
					sqlite.WithBusyTimeout(5000),
					sqlite.WithMaxOpenConns(8),
					sqlite.WithMaxIdleConns(8),
				)
				if err != nil {
					os.Remove(path)
					return nil, nil, err
				}

				store, err := db.Open("app", "ns")
				if err != nil {
					db.Close()
					os.Remove(path)
					return nil, nil, err
				}

				cleanup := func() {
					_ = db.Close()
					_ = os.Remove(path)
				}
				return store, cleanup, nil
			},
		},
		{
			name: "bbolt",
			open: func(tb testing.TB) (config.Store, func(), error) {
				tb.Helper()
				tmp, err := os.CreateTemp("", "config-bench-bbolt-*.db")
				if err != nil {
					return nil, nil, err
				}
				path := tmp.Name()
				if err := tmp.Close(); err != nil {
					os.Remove(path)
					return nil, nil, err
				}

				db, err := configbbolt.New(configbbolt.WithPath(path))
				if err != nil {
					os.Remove(path)
					return nil, nil, err
				}

				store, err := db.Open("app", "ns")
				if err != nil {
					db.Close()
					os.Remove(path)
					return nil, nil, err
				}

				cleanup := func() {
					_ = db.Close()
					_ = os.Remove(path)
				}
				return store, cleanup, nil
			},
		},
	}
}

func populateStore(tb testing.TB, store config.Store, keys []string) {
	tb.Helper()
	for _, key := range keys {
		if err := store.Marshal(config.Key(key), benchConfig{Value: "value"}); err != nil {
			tb.Fatalf("populate %s: %v", key, err)
		}
	}
}

func benchKeys(count int) []string {
	keys := make([]string, count)
	for i := range keys {
		keys[i] = benchKey(i)
	}
	return keys
}

func benchKey(index int) string {
	return fmt.Sprintf("key-%d", index)
}

func benchMissingKey(index int) string {
	return fmt.Sprintf("missing-%d", index)
}

func storeMarshalWithRetry(backendName string, store config.Store, key string) error {
	err := store.Marshal(config.Key(key), benchConfig{Value: "value"})
	if err == nil || !strings.HasPrefix(backendName, "sqlite") {
		return err
	}
	if !isSQLiteBusy(err) {
		return err
	}

	for i := 0; i < 20; i++ {
		time.Sleep(5 * time.Millisecond)
		err = store.Marshal(config.Key(key), benchConfig{Value: "value"})
		if err == nil {
			return nil
		}
		if !isSQLiteBusy(err) {
			return err
		}
	}
	return err
}

func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "SQLITE_BUSY") || strings.Contains(message, "database is locked")
}

func benchIntsFromEnv(b *testing.B, name string, fallback []int) []int {
	b.Helper()
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}

	fields := strings.Split(raw, ",")
	result := make([]int, 0, len(fields))
	for _, field := range fields {
		trimmed := strings.TrimSpace(field)
		if trimmed == "" {
			continue
		}
		value, err := strconv.Atoi(trimmed)
		if err != nil {
			b.Fatalf("invalid %s value %q: %v", name, trimmed, err)
		}
		result = append(result, value)
	}
	if len(result) == 0 {
		return fallback
	}
	return result
}
