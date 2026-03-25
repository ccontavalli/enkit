// Package cryptstore wraps config.Loader backends with key and value encryption.
//
// For structured values, compose the returned loader with config.OpenSimple,
// config.OpenMulti, config.NewSimple, or config.NewMulti.
//
// For CLI integration, use DefaultFlags/Register plus FromFlags to build the
// key codec and value encoder from command-line configuration.
//
// Key codec constraints:
//
//   - The default behavior, when no key codec is configured, is to keep
//     plaintext keys and apply config.DefaultKeyCodec() escaping.
//   - Custom key codecs must be deterministic, reversible, and stable across
//     process restarts. cryptstore re-encodes the plaintext key for every
//     Read, Write, and Delete operation.
//   - Randomized key encoders are invalid. If Encode("name") can return a
//     different value on each call, later reads and deletes will look up a
//     different backend key than the one originally written.
//   - Key codecs do not need to preserve lexicographic ordering. The generic
//     list path decodes backend keys, sorts them in plaintext order, and
//     applies list semantics locally.
//   - Codecs that implement ListOptimizingKeyCodec may take over listing, but
//     they must still return plaintext keys and preserve the visible list
//     semantics of the wrapped loader.
package cryptstore
