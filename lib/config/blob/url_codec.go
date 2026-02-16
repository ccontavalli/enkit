package blob

import (
	"context"
	"fmt"
	"math/rand"
	"net/url"
	"sort"
	"time"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/config/marshal"
	"github.com/ccontavalli/enkit/lib/kflags"
	"github.com/ccontavalli/enkit/lib/token"
)

// URL codecs control how blob keys and transfer parameters are represented in URLs.
//
// Common usage patterns:
//
// 1) Default encrypted key + params in query:
//    rng := rand.New(rand.NewSource(seed))
//    codec, err := NewTokenCodec(WithTokenRand(rng))
//
// 2) Encode everything in the path (no query parameters):
//    rng := rand.New(rand.NewSource(seed))
//    path, err := NewTokenPathCodec(rng)
//    codec, err := NewTokenCodec(WithTokenPathCodec(path))
//
// 3) Reuse a specific key (so tokens are stable across restarts):
//    rng := rand.New(rand.NewSource(seed))
//    enc, err := BuildTokenEncoder(rng, WithEncoderSetters(token.UseSymmetricKey(keyBytes)))
//    codec, err := NewTokenCodec(WithTokenEncoder(enc))
//
// 4) Configure from flags (key + optional validity):
//    flags := DefaultTokenCodecFlags()
//    flags.Register(flagSet, "blob-")
//    // After flag parsing:
//    rng := rand.New(rand.NewSource(seed))
//    codec, err := NewTokenCodec(WithTokenRand(rng), WithTokenFlags(flags))
//
// 5) Stable encryption (fixed nonce; deterministic output):
//    rng := rand.New(rand.NewSource(seed))
//    enc, err := NewSymmetricTokenEncoder(rng, WithEncoderSetters(token.UseFixedNonce(nil)))
//    codec, err := NewTokenCodec(WithTokenEncoder(enc))
//
// 6) Build key/params codecs directly from modifiers:
//    rng := rand.New(rand.NewSource(seed))
//    keyCodec, err := NewTokenKeyCodec(rng, WithEncoderValidity(time.Hour))
//    paramsCodec, err := NewTokenParamsCodec(rng, "token", WithEncoderValidity(time.Hour))
//
// URLCodec encodes and decodes URL paths and query parameters.
//
// Decode should ignore parameters it does not understand and leave them as-is.
type URLCodec interface {
	Encode(key string, params url.Values) (encodedKey string, encodedParams url.Values, err error)
	Decode(encodedKey string, encodedParams url.Values) (key string, params url.Values, err error)
}

// PlainCodec encodes keys as paths and passes parameters through unchanged.
type PlainCodec struct{}

func (PlainCodec) Encode(key string, params url.Values) (string, url.Values, error) {
	encodedKey := config.EncodeKey(key)
	if params == nil {
		params = url.Values{}
	}
	return encodedKey, params, nil
}

func (PlainCodec) Decode(encodedKey string, values url.Values) (string, url.Values, error) {
	key := config.DecodeKey(encodedKey)
	if values == nil {
		values = url.Values{}
	}
	return key, values, nil
}

type tokenParamEntry struct {
	Key    string   `json:"key"`
	Values []string `json:"values,omitempty"`
}

type tokenPathPayload struct {
	Key    string            `json:"key"`
	Params []tokenParamEntry `json:"params,omitempty"`
}

// TokenParamsCodec signs or encrypts parameters into a single token query field.
type TokenParamsCodec struct {
	enc       *token.TypeEncoder
	paramName string
}

// NewTokenParamsCodec creates a token-based codec using encoder modifiers.
func NewTokenParamsCodec(rng *rand.Rand, paramName string, mods ...TokenEncoderOption) (*TokenParamsCodec, error) {
	opts := tokenEncoderOptions{}
	TokenEncoderOptions(mods).Apply(&opts)
	enc := opts.encoder
	if enc == nil {
		if rng == nil {
			return nil, fmt.Errorf("token RNG must be provided")
		}
		var err error
		enc, err = BuildTokenEncoder(
			rng,
			WithEncoderValidity(opts.validity),
			WithEncoderSetters(opts.setters...),
		)
		if err != nil {
			return nil, err
		}
	}
	if paramName == "" {
		paramName = "token"
	}
	typeSetters := append([]token.TypeEncoderSetter{
		token.WithMarshaller(marshal.Json),
	}, opts.typeSetters...)
	return &TokenParamsCodec{
		enc:       token.NewTypeEncoder(enc, typeSetters...),
		paramName: paramName,
	}, nil
}

func (c *TokenParamsCodec) Encode(key string, params url.Values) (string, url.Values, error) {
	raw, err := c.enc.Encode(encodeTokenParams(params))
	if err != nil {
		return "", nil, err
	}
	values := url.Values{}
	values.Set(c.paramName, string(raw))
	return key, values, nil
}

func (c *TokenParamsCodec) Decode(key string, encodedParams url.Values) (string, url.Values, error) {
	values := url.Values{}
	for k, v := range encodedParams {
		values[k] = append([]string(nil), v...)
	}
	tokenValue := values.Get(c.paramName)
	if tokenValue == "" {
		return key, values, nil
	}
	var payload []tokenParamEntry
	if _, err := c.enc.Decode(context.Background(), []byte(tokenValue), &payload); err != nil {
		return "", nil, err
	}
	delete(values, c.paramName)
	decoded := decodeTokenParams(payload)
	for k, v := range decoded {
		for _, value := range v {
			values.Add(k, value)
		}
	}
	return key, values, nil
}

// TokenKeyCodec signs or encrypts keys into a token-safe string.
type TokenKeyCodec struct {
	enc token.BinaryEncoder
}

// NewTokenKeyCodec creates a key codec using encoder modifiers.
func NewTokenKeyCodec(rng *rand.Rand, mods ...TokenEncoderOption) (*TokenKeyCodec, error) {
	opts := tokenEncoderOptions{}
	TokenEncoderOptions(mods).Apply(&opts)
	enc := opts.encoder
	if enc == nil {
		if rng == nil {
			return nil, fmt.Errorf("token RNG must be provided")
		}
		var err error
		enc, err = BuildTokenEncoder(
			rng,
			WithEncoderValidity(opts.validity),
			WithEncoderSetters(opts.setters...),
		)
		if err != nil {
			return nil, err
		}
	}
	return &TokenKeyCodec{enc: enc}, nil
}
func (c *TokenKeyCodec) Encode(key string, params url.Values) (string, url.Values, error) {
	raw, err := c.enc.Encode([]byte(key))
	if err != nil {
		return "", nil, err
	}
	return string(raw), params, nil
}

func (c *TokenKeyCodec) Decode(encodedKey string, params url.Values) (string, url.Values, error) {
	_, raw, err := c.enc.Decode(context.Background(), []byte(encodedKey))
	if err != nil {
		return "", nil, err
	}
	return string(raw), params, nil
}

// TokenPathCodec encodes both key and params into the path token.
type TokenPathCodec struct {
	enc *token.TypeEncoder
}

// NewTokenPathCodec creates a path-only codec using encoder modifiers.
func NewTokenPathCodec(rng *rand.Rand, mods ...TokenEncoderOption) (*TokenPathCodec, error) {
	opts := tokenEncoderOptions{}
	TokenEncoderOptions(mods).Apply(&opts)
	enc := opts.encoder
	if enc == nil {
		if rng == nil {
			return nil, fmt.Errorf("token RNG must be provided")
		}
		var err error
		enc, err = BuildTokenEncoder(
			rng,
			WithEncoderValidity(opts.validity),
			WithEncoderSetters(opts.setters...),
		)
		if err != nil {
			return nil, err
		}
	}
	typeSetters := append([]token.TypeEncoderSetter{
		token.WithMarshaller(marshal.Json),
	}, opts.typeSetters...)
	return &TokenPathCodec{enc: token.NewTypeEncoder(enc, typeSetters...)}, nil
}

func (c TokenPathCodec) Encode(key string, params url.Values) (string, url.Values, error) {
	raw, err := c.enc.Encode(tokenPathPayload{Key: key, Params: encodeTokenParams(params)})
	if err != nil {
		return "", nil, err
	}
	return string(raw), url.Values{}, nil
}

func (c TokenPathCodec) Decode(encodedKey string, encodedParams url.Values) (string, url.Values, error) {
	var payload tokenPathPayload
	if _, err := c.enc.Decode(context.Background(), []byte(encodedKey), &payload); err != nil {
		return "", nil, err
	}
	params := decodeTokenParams(payload.Params)
	for k, v := range encodedParams {
		for _, value := range v {
			params.Add(k, value)
		}
	}
	return payload.Key, params, nil
}

// TokenCodec is a URLCodec composed of a chain of URLCodecs.
type TokenCodec struct {
	chain []URLCodec
}

func (c TokenCodec) Encode(key string, params url.Values) (string, url.Values, error) {
	var err error
	for _, codec := range c.chain {
		key, params, err = codec.Encode(key, params)
		if err != nil {
			return "", nil, err
		}
	}
	return key, params, nil
}

func (c TokenCodec) Decode(encodedKey string, encodedParams url.Values) (string, url.Values, error) {
	var err error
	for i := len(c.chain) - 1; i >= 0; i-- {
		encodedKey, encodedParams, err = c.chain[i].Decode(encodedKey, encodedParams)
		if err != nil {
			return "", nil, err
		}
	}
	return encodedKey, encodedParams, nil
}

type TokenCodecOption func(*tokenCodecOptions)

type tokenCodecOptions struct {
	validity       time.Duration
	setters        []token.SymmetricSetter
	rng            *rand.Rand
	chain          []URLCodec
	encoderFactory func(*tokenCodecOptions) (token.BinaryEncoder, error)
}

// WithTokenKeyCodec appends a key codec to the chain.
func WithTokenKeyCodec(codec URLCodec) TokenCodecOption {
	return func(o *tokenCodecOptions) {
		o.chain = append(o.chain, codec)
	}
}

// WithTokenParamsCodec appends a params codec to the chain.
func WithTokenParamsCodec(codec URLCodec) TokenCodecOption {
	return func(o *tokenCodecOptions) {
		o.chain = append(o.chain, codec)
	}
}

// WithTokenPathCodec sets the chain to a path-only codec.
func WithTokenPathCodec(codec URLCodec) TokenCodecOption {
	return func(o *tokenCodecOptions) {
		o.chain = []URLCodec{codec}
	}
}

// WithTokenValidity configures token expiry duration (0 disables expiry).
func WithTokenValidity(validity time.Duration) TokenCodecOption {
	return func(o *tokenCodecOptions) {
		o.validity = validity
	}
}

// WithTokenRand sets the RNG used to create token encoders.
func WithTokenRand(rng *rand.Rand) TokenCodecOption {
	return func(o *tokenCodecOptions) {
		o.rng = rng
	}
}

// WithTokenSetters provides symmetric setters used to build token encoders.
func WithTokenSetters(setters ...token.SymmetricSetter) TokenCodecOption {
	return func(o *tokenCodecOptions) {
		o.setters = append(o.setters, setters...)
	}
}

// WithTokenEncoder sets the encoder used for token codecs.
func WithTokenEncoder(enc token.BinaryEncoder) TokenCodecOption {
	return func(o *tokenCodecOptions) {
		o.encoderFactory = func(*tokenCodecOptions) (token.BinaryEncoder, error) {
			return enc, nil
		}
	}
}

// WithTokenEncoderFactory sets a factory used to build token encoders.
func WithTokenEncoderFactory(factory func(*tokenCodecOptions) (token.BinaryEncoder, error)) TokenCodecOption {
	return func(o *tokenCodecOptions) {
		o.encoderFactory = factory
	}
}

// WithTokenFlags applies settings from token.Flags and blob token flags.
func WithTokenFlags(flags *TokenCodecFlags) TokenCodecOption {
	return func(o *tokenCodecOptions) {
		if flags == nil {
			return
		}
		if flags.Flags != nil {
			o.validity = flags.Flags.Validity
			o.setters = append(o.setters, token.WithFlags(flags.Flags))
		}
	}
}

func NewTokenCodec(mods ...TokenCodecOption) (*TokenCodec, error) {
	opts := tokenCodecOptions{}
	for _, mod := range mods {
		if mod != nil {
			mod(&opts)
		}
	}
	if len(opts.chain) > 0 {
		return &TokenCodec{chain: opts.chain}, nil
	}

	var enc token.BinaryEncoder
	if opts.encoderFactory != nil {
		var err error
		enc, err = opts.encoderFactory(&opts)
		if err != nil {
			return nil, err
		}
	} else {
		if opts.rng == nil {
			return nil, fmt.Errorf("token RNG must be provided")
		}
		var err error
		enc, err = BuildTokenEncoder(
			opts.rng,
			WithEncoderValidity(opts.validity),
			WithEncoderSetters(opts.setters...),
		)
		if err != nil {
			return nil, err
		}
	}
	keyCodec, err := NewTokenKeyCodec(nil, WithEncoder(enc))
	if err != nil {
		return nil, err
	}
	paramsCodec, err := NewTokenParamsCodec(nil, "token", WithEncoder(enc))
	if err != nil {
		return nil, err
	}

	return &TokenCodec{chain: []URLCodec{keyCodec, paramsCodec}}, nil
}

// TokenCodecFlags configures token-based URL codecs.
type TokenCodecFlags struct {
	*token.Flags
}

func DefaultTokenCodecFlags() *TokenCodecFlags {
	return &TokenCodecFlags{
		Flags: token.DefaultFlags(),
	}
}

func (f *TokenCodecFlags) Register(set kflags.FlagSet, prefix string) *TokenCodecFlags {
	if f.Flags == nil {
		f.Flags = token.DefaultFlags()
	}
	f.Flags.Register(set, prefix)
	return f
}

// TokenEncoderOption configures NewSymmetricTokenEncoder.
type TokenEncoderOption func(*tokenEncoderOptions)

// TokenEncoderOptions is a slice of token encoder modifiers.
type TokenEncoderOptions []TokenEncoderOption

// Apply applies all modifiers to opts.
func (mods TokenEncoderOptions) Apply(opts *tokenEncoderOptions) {
	for _, mod := range mods {
		if mod != nil {
			mod(opts)
		}
	}
}

type tokenEncoderOptions struct {
	validity time.Duration
	setters  []token.SymmetricSetter
	encoder  token.BinaryEncoder
	typeSetters []token.TypeEncoderSetter
}

// WithEncoderValidity enables optional expiry in token encoders.
func WithEncoderValidity(validity time.Duration) TokenEncoderOption {
	return func(o *tokenEncoderOptions) {
		o.validity = validity
	}
}

// WithEncoderSetters provides symmetric setters used to build token encoders.
func WithEncoderSetters(setters ...token.SymmetricSetter) TokenEncoderOption {
	return func(o *tokenEncoderOptions) {
		o.setters = append(o.setters, setters...)
	}
}

// WithEncoderTypeSetters configures TypeEncoder options.
func WithEncoderTypeSetters(setters ...token.TypeEncoderSetter) TokenEncoderOption {
	return func(o *tokenEncoderOptions) {
		o.typeSetters = append(o.typeSetters, setters...)
	}
}

// WithEncoder provides a prebuilt binary encoder.
func WithEncoder(enc token.BinaryEncoder) TokenEncoderOption {
	return func(o *tokenEncoderOptions) {
		o.encoder = enc
	}
}

// NewSymmetricTokenEncoder returns a URL-safe symmetric encoder.
func NewSymmetricTokenEncoder(rng *rand.Rand, mods ...TokenEncoderOption) (token.BinaryEncoder, error) {
	if rng == nil {
		return nil, fmt.Errorf("token RNG must be provided")
	}
	return BuildTokenEncoder(rng, mods...)
}

// BuildTokenEncoder constructs the default token encoder.
func BuildTokenEncoder(rng *rand.Rand, mods ...TokenEncoderOption) (token.BinaryEncoder, error) {
	opts := tokenEncoderOptions{}
	TokenEncoderOptions(mods).Apply(&opts)
	if opts.encoder != nil {
		return opts.encoder, nil
	}
	if rng == nil {
		return nil, fmt.Errorf("token RNG must be provided")
	}
	setters := append([]token.SymmetricSetter{token.WithGeneratedSymmetricKey(256)}, opts.setters...)
	enc, err := token.NewSymmetricEncoder(rng, setters...)
	if err != nil {
		return nil, err
	}
	chain := []token.BinaryEncoder{}
	if opts.validity > 0 {
		chain = append(chain, token.NewExpireEncoder(nil, opts.validity))
	}
	chain = append(chain, enc, token.NewBase64UrlEncoder())
	return token.NewChainedEncoder(chain...), nil
}

func encodeTransferOptions(opts transferOptions) url.Values {
	values := url.Values{}
	if opts.Filename != "" {
		values.Set(queryFilename, opts.Filename)
	}
	if opts.ContentType != "" {
		values.Set(queryContentType, opts.ContentType)
	}
	return values
}

func decodeTransferOptions(values url.Values) transferOptions {
	return transferOptions{
		Filename:    values.Get(queryFilename),
		ContentType: values.Get(queryContentType),
	}
}

func encodeTokenParams(values url.Values) []tokenParamEntry {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	entries := make([]tokenParamEntry, 0, len(keys))
	for _, key := range keys {
		vals := append([]string(nil), values[key]...)
		sort.Strings(vals)
		entries = append(entries, tokenParamEntry{Key: key, Values: vals})
	}
	return entries
}

func decodeTokenParams(entries []tokenParamEntry) url.Values {
	values := url.Values{}
	for _, entry := range entries {
		if len(entry.Values) == 0 {
			values.Set(entry.Key, "")
			continue
		}
		for _, value := range entry.Values {
			values.Add(entry.Key, value)
		}
	}
	return values
}
