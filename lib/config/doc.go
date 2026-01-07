// Package config provides a unified interface for configuration stores.
//
// Backends:
//   - directory: multi-format files on disk (TOML/JSON/YAML/Gob). Best when configs should be
//     human-editable or shared with external tools.
//   - bbolt: JSON-only embedded KV store optimized for local, programmatic access.
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
// Quantitative summary (i7-9700, benchtime=50ms, counts=1/100/1000, parallelism=1/4).
// n = number of records pre-populated in the store. p = benchmark parallelism setting.
// Each cell shows % delta vs directory (negative is faster).
//
// List:
// | n/p | sqlite-store | sqlite-multi | bbolt |
// | --- | --- | --- | --- |
// | n=1,p=1 | +465% | +199% | -92% |
// | n=1,p=4 | +637% | +498% | -91% |
// | n=100,p=1 | +18% | -17% | -93% |
// | n=100,p=4 | -28% | -34% | -92% |
// | n=1000,p=1 | -31% | -59% | -91% |
// | n=1000,p=4 | -34% | -37% | -96% |
//
// Get:
// | n/p | sqlite-store | sqlite-multi | bbolt |
// | --- | --- | --- | --- |
// | n=1,p=1 | +787% | +893% | -34% |
// | n=1,p=4 | +1239% | +5217% | -33% |
// | n=100,p=1 | +1193% | +705% | +82% |
// | n=100,p=4 | +366% | +2058% | +4% |
// | n=1000,p=1 | +149% | +1797% | +13% |
// | n=1000,p=4 | +197% | +1308% | +28% |
//
// Store:
// | n/p | sqlite-store | sqlite-multi | bbolt |
// | --- | --- | --- | --- |
// | n=1,p=1 | -15% | +12% | +16505% |
// | n=1,p=4 | -37% | -22% | +11555% |
// | n=100,p=1 | -29% | -7% | +12474% |
// | n=100,p=4 | -16% | -22% | +12036% |
// | n=1000,p=1 | -33% | +65% | +9582% |
// | n=1000,p=4 | -24% | -50% | +7841% |
//
// LookupMissing:
// | n/p | sqlite-store | sqlite-multi | bbolt |
// | --- | --- | --- | --- |
// | n=1,p=1 | +1272% | +1312% | +54% |
// | n=1,p=4 | +961% | +1402% | +10% |
// | n=100,p=1 | +590% | +1111% | +44% |
// | n=100,p=4 | +2853% | +1184% | +14% |
// | n=1000,p=1 | +348% | +899% | +39% |
// | n=1000,p=4 | +310% | +1104% | +43% |
package config
