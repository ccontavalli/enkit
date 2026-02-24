// Package config provides a unified interface for configuration stores.
//
// Backends:
//   - directory: files on disk (TOML/JSON/YAML/Gob). Best when configs should be
//     human-editable or shared with external tools.
//   - bbolt: embedded KV store optimized for local, programmatic access.
//   - sqlite: embedded storage optimized for programmatic access and local queries.
//   - datastore: Google Cloud Datastore backend for remote config storage.
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
// | n/p | sqlite-store | bbolt |
// | --- | --- | --- |
// | n=1,p=1 | +465% | -92% |
// | n=1,p=4 | +637% | -91% |
// | n=100,p=1 | +18% | -93% |
// | n=100,p=4 | -28% | -92% |
// | n=1000,p=1 | -31% | -91% |
// | n=1000,p=4 | -34% | -96% |
//
// Get:
// | n/p | sqlite-store | bbolt |
// | --- | --- | --- |
// | n=1,p=1 | +787% | -34% |
// | n=1,p=4 | +1239% | -33% |
// | n=100,p=1 | +1193% | +82% |
// | n=100,p=4 | +366% | +4% |
// | n=1000,p=1 | +149% | +13% |
// | n=1000,p=4 | +197% | +28% |
//
// Store:
// | n/p | sqlite-store | bbolt |
// | --- | --- | --- |
// | n=1,p=1 | -15% | +16505% |
// | n=1,p=4 | -37% | +11555% |
// | n=100,p=1 | -29% | +12474% |
// | n=100,p=4 | -16% | +12036% |
// | n=1000,p=1 | -33% | +9582% |
// | n=1000,p=4 | -24% | +7841% |
//
// LookupMissing:
// | n/p | sqlite-store | bbolt |
// | --- | --- | --- |
// | n=1,p=1 | +1272% | +54% |
// | n=1,p=4 | +961% | +10% |
// | n=100,p=1 | +590% | +44% |
// | n=100,p=4 | +2853% | +14% |
// | n=1000,p=1 | +348% | +39% |
// | n=1000,p=4 | +310% | +43% |
package config
