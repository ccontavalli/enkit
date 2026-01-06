// Package config provides a unified interface for configuration stores.
//
// Backends:
//   - directory: multi-format files on disk (TOML/JSON/YAML/Gob). Best when configs should be
//     human-editable or shared with external tools.
//   - sqlite: JSON-only storage optimized for programmatic access and local queries.
//   - sqlite multi: uses the SQLite loader with multi-format encoding for interoperability.
//   - datastore: Google Cloud Datastore backend for remote config storage.
//
// SQLite vs SQLiteMulti:
// - Use sqlite when you want a compact, JSON-only store and don’t need human editing.
// - Use sqlite multi when you must preserve multi-format compatibility.
//
// Benchmark notes:
//   - The benchmark suite exercises list/get/store/lookup across backends with varying record counts
//     and parallelism.
//   - SQLite uses a single-writer model; the store benchmark for sqlite runs single-threaded even
//     when parallelism is higher.
//   - Results vary across machines and storage layers. See BenchmarkConfigStore in bench_test.go for
//     the current suite; you can override record counts and parallelism via the environment variables
//     ENKIT_CONFIG_BENCH_COUNTS and ENKIT_CONFIG_BENCH_PARALLELISM.
//
// Quantitative summary (i7-9700, ENKIT_CONFIG_BENCH_COUNTS=100,1000, ENKIT_CONFIG_BENCH_PARALLELISM=1,4):
// - List: sqlite-store/sqlite-multi are ~50-57% faster than directory.
// - Get: sqlite-store is ~153-199% slower than directory; sqlite-multi is ~204-243% slower.
// - Store: sqlite-store is ~96-137% slower than directory; sqlite-multi is ~92-150% slower.
// - LookupMissing: sqlite-store is ~227-272% slower; sqlite-multi is ~238-276% slower.
package config
