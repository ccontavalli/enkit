package cryptstore

import (
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/config/directory"
	"github.com/ccontavalli/enkit/lib/config/marshal"
	"github.com/ccontavalli/enkit/lib/config/memory"
	"github.com/ccontavalli/enkit/lib/kflags"
	"github.com/ccontavalli/enkit/lib/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testConfig struct {
	Value string `json:"value"`
}

type captureFlagSet struct {
	bools        map[string]*bool
	durations    map[string]*time.Duration
	strings      map[string]*string
	stringArrays map[string]*[]string
	byteFiles    map[string]*[]byte
	ints         map[string]*int
}

func newCaptureFlagSet() *captureFlagSet {
	return &captureFlagSet{
		bools:        map[string]*bool{},
		durations:    map[string]*time.Duration{},
		strings:      map[string]*string{},
		stringArrays: map[string]*[]string{},
		byteFiles:    map[string]*[]byte{},
		ints:         map[string]*int{},
	}
}

func (c *captureFlagSet) BoolVar(target *bool, name string, value bool, usage string) {
	*target = value
	c.bools[name] = target
}

func (c *captureFlagSet) DurationVar(target *time.Duration, name string, value time.Duration, usage string) {
	*target = value
	c.durations[name] = target
}

func (c *captureFlagSet) StringVar(target *string, name string, value string, usage string) {
	*target = value
	c.strings[name] = target
}

func (c *captureFlagSet) StringArrayVar(target *[]string, name string, value []string, usage string) {
	copy := append([]string{}, value...)
	*target = copy
	c.stringArrays[name] = target
}

func (c *captureFlagSet) ByteFileVar(target *[]byte, name string, defaultFile string, usage string, mods ...kflags.ByteFileModifier) {
	*target = []byte{}
	c.byteFiles[name] = target
}

func (c *captureFlagSet) IntVar(target *int, name string, value int, usage string) {
	*target = value
	c.ints[name] = target
}

type reverseKeyCodec struct{}

func (reverseKeyCodec) Encode(key string) (string, error) {
	if key == "" {
		return key, nil
	}
	out := []byte(key)
	for i, b := range out {
		if b >= 'a' && b <= 'z' {
			out[i] = 'z' - (b - 'a')
		}
	}
	return string(out), nil
}

func (c reverseKeyCodec) Decode(key string) (string, error) {
	return c.Encode(key)
}

type failingKeyCodec struct {
	err error
}

func (c failingKeyCodec) Encode(string) (string, error) {
	return "", c.err
}

func (c failingKeyCodec) Decode(string) (string, error) {
	return "", c.err
}

func mustValueEncoder(t *testing.T) token.BinaryEncoder {
	t.Helper()
	encoder, err := NewRandomSymmetricValueEncoder(rand.New(rand.NewSource(1234)), []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	return encoder
}

func TestRegisterIncludesCryptstoreFlags(t *testing.T) {
	flags := DefaultFlags()
	set := newCaptureFlagSet()
	flags.Register(set, "")

	mode, ok := set.strings["config-store-crypt-key-mode"]
	if assert.True(t, ok, "key mode flag was not registered") {
		assert.Equal(t, KeyModePlain, *mode)
	}

	_, ok = set.byteFiles["config-store-crypt-key-encryption-key"]
	assert.True(t, ok, "key encryption key flag was not registered")

	_, ok = set.byteFiles["config-store-crypt-key-encryption-nonce"]
	assert.True(t, ok, "key encryption nonce flag was not registered")

	_, ok = set.byteFiles["config-store-crypt-value-encryption-key"]
	assert.True(t, ok, "value encryption key flag was not registered")
}

func TestFromFlagsUsesPlainKeysByDefault(t *testing.T) {
	raw := memory.Open()
	flags := DefaultFlags()
	flags.ValueEncryptionKey = []byte("0123456789abcdef0123456789abcdef")

	wrapped, err := NewLoader(raw, FromFlags(flags, rand.New(rand.NewSource(1234))))
	require.NoError(t, err)

	require.NoError(t, wrapped.Write("a/b", []byte("top-secret")))

	stored, err := raw.Read(config.EncodeKey("a/b"))
	require.NoError(t, err)
	assert.NotContains(t, string(stored), "top-secret")

	plain, err := wrapped.Read("a/b")
	require.NoError(t, err)
	assert.Equal(t, "top-secret", string(plain))
}

func TestFromFlagsBuildsDeterministicTokenKeyCodec(t *testing.T) {
	raw := memory.Open()
	flags := DefaultFlags()
	flags.KeyMode = KeyModeDeterministicToken
	flags.KeyEncryptionKey = []byte("abcdef0123456789abcdef0123456789")
	flags.KeyEncryptionNonce = []byte("123456789012")
	flags.ValueEncryptionKey = []byte("0123456789abcdef0123456789abcdef")

	wrapped, err := NewLoader(raw, FromFlags(flags, rand.New(rand.NewSource(1234))))
	require.NoError(t, err)

	require.NoError(t, wrapped.Write("a", []byte("payload-a")))
	require.NoError(t, wrapped.Write("b", []byte("payload-b")))

	rawKeys, err := raw.List()
	require.NoError(t, err)
	assert.NotContains(t, rawKeys, "a")
	assert.NotContains(t, rawKeys, "b")

	keys, err := wrapped.List(config.WithStartFrom(config.Key("b")), config.WithLimit(1))
	require.NoError(t, err)
	assert.Equal(t, []string{"b"}, keys)
}

func TestFromFlagsRejectsUnknownKeyMode(t *testing.T) {
	raw := memory.Open()
	flags := DefaultFlags()
	flags.KeyMode = "invalid"
	flags.ValueEncryptionKey = []byte("0123456789abcdef0123456789abcdef")

	_, err := NewLoader(raw, FromFlags(flags, rand.New(rand.NewSource(1234))))
	require.EqualError(t, err, "unknown cryptstore key mode: invalid")
}

func TestFromFlagsRejectsMissingValueEncryptionKey(t *testing.T) {
	raw := memory.Open()
	flags := DefaultFlags()

	_, err := NewLoader(raw, FromFlags(flags, rand.New(rand.NewSource(1234))))
	require.EqualError(t, err, "value encryption key is required")
}

func TestLoaderWrapEncryptsAtRest(t *testing.T) {
	raw := memory.Open()
	wrapped, err := NewLoader(raw, WithValueEncoder(mustValueEncoder(t)))
	require.NoError(t, err)

	err = wrapped.Write("a/b", []byte("top-secret"))
	require.NoError(t, err)

	stored, err := raw.Read(config.EncodeKey("a/b"))
	require.NoError(t, err)
	assert.NotContains(t, string(stored), "top-secret")

	plain, err := wrapped.Read("a/b")
	require.NoError(t, err)
	assert.Equal(t, "top-secret", string(plain))
}

func TestLoaderWrapPreservesListWithPlainKeyCodec(t *testing.T) {
	raw := memory.Open()
	wrapped, err := NewLoader(raw, WithValueEncoder(mustValueEncoder(t)))
	require.NoError(t, err)

	for _, key := range []string{"a", "b", "c"} {
		require.NoError(t, wrapped.Write(key, []byte("payload-"+key)))
	}

	keys, err := wrapped.List(config.WithStartFrom(config.Key("b")), config.WithLimit(1))
	require.NoError(t, err)
	require.Equal(t, []string{"b"}, keys)
}

func TestLoaderWrapDeterministicKeyCodecImplementsListFallback(t *testing.T) {
	raw := memory.Open()
	kcodec, err := NewDeterministicTokenKeyCodec([]byte("0123456789abcdef0123456789abcdef"), []byte("123456789012"))
	require.NoError(t, err)
	wrapped, err := NewLoader(raw, WithKeyCodec(kcodec), WithValueEncoder(mustValueEncoder(t)))
	require.NoError(t, err)

	for _, key := range []string{"a", "b", "c"} {
		require.NoError(t, wrapped.Write(key, []byte("payload-"+key)))
	}

	rawKeys, err := raw.List()
	require.NoError(t, err)
	require.NotContains(t, rawKeys, "a")
	require.NotContains(t, rawKeys, "b")
	require.NotContains(t, rawKeys, "c")

	keys, err := wrapped.List(config.WithStartFrom(config.Key("b")), config.WithLimit(1))
	require.NoError(t, err)
	require.Equal(t, []string{"b"}, keys)
}

func TestLoaderWorkspaceWrapForwardsParsePath(t *testing.T) {
	root, err := os.MkdirTemp("", "cryptstore-parsepath")
	require.NoError(t, err)
	defer os.RemoveAll(root)

	wrapped, err := NewLoaderWorkspace(directory.New(root), WithValueEncoder(mustValueEncoder(t)))
	require.NoError(t, err)

	parsed, err := wrapped.ParsePath(filepath.Join(root, "etc", "enproxy.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "/", parsed.AppName)
	assert.Equal(t, []string{"etc"}, parsed.Namespaces)
	assert.Equal(t, "enproxy", parsed.Descriptor.Key())

	hinted, ok := parsed.Descriptor.(config.RequestedFormatDescriptor)
	if assert.True(t, ok) {
		assert.Equal(t, "yaml", hinted.Format())
	}
}

func TestLoaderWrapPropagatesKeyCodecEncodeError(t *testing.T) {
	raw := memory.Open()
	boom := errors.New("boom")
	wrapped, err := NewLoader(raw, WithKeyCodec(failingKeyCodec{err: boom}), WithValueEncoder(mustValueEncoder(t)))
	require.NoError(t, err)

	err = wrapped.Write("a", []byte("payload-a"))
	require.ErrorIs(t, err, boom)
}

func TestLoaderWrapDeterministicKeyCodecPreservesEmptyKeyInList(t *testing.T) {
	raw := memory.Open()
	kcodec, err := NewDeterministicTokenKeyCodec([]byte("0123456789abcdef0123456789abcdef"), []byte("123456789012"))
	require.NoError(t, err)
	wrapped, err := NewLoader(raw, WithKeyCodec(kcodec), WithValueEncoder(mustValueEncoder(t)))
	require.NoError(t, err)

	require.NoError(t, wrapped.Write("", []byte("payload-empty")))
	require.NoError(t, wrapped.Write("b", []byte("payload-b")))

	keys, err := wrapped.List()
	require.NoError(t, err)
	assert.Equal(t, []string{"", "b"}, keys)
}

func TestLoaderWrapDeterministicKeyCodecListFailsOnCorruptKey(t *testing.T) {
	raw := memory.Open()
	kcodec, err := NewDeterministicTokenKeyCodec([]byte("0123456789abcdef0123456789abcdef"), []byte("123456789012"))
	require.NoError(t, err)
	wrapped, err := NewLoader(raw, WithKeyCodec(kcodec), WithValueEncoder(mustValueEncoder(t)))
	require.NoError(t, err)

	require.NoError(t, wrapped.Write("a", []byte("payload-a")))
	require.NoError(t, raw.Write("not-valid-token", []byte("junk")))

	_, err = wrapped.List()
	require.Error(t, err)
}

func TestLoaderWrapNonOptimizingCodecPreservesPlaintextListSemantics(t *testing.T) {
	raw := memory.Open()
	wrapped, err := NewLoader(raw, WithKeyCodec(reverseKeyCodec{}), WithValueEncoder(mustValueEncoder(t)))
	require.NoError(t, err)

	for _, key := range []string{"a", "b", "c", "d"} {
		require.NoError(t, wrapped.Write(key, []byte("payload-"+key)))
	}

	rawKeys, err := raw.List()
	require.NoError(t, err)
	assert.Equal(t, []string{"w", "x", "y", "z"}, rawKeys)

	keys, err := wrapped.List(
		config.WithStartFrom(config.Key("b")),
		config.WithOffset(1),
		config.WithLimit(2),
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"c", "d"}, keys)
}

func TestLoaderWrapComposesWithSimpleStore(t *testing.T) {
	raw := memory.Open()
	loader, err := NewLoader(raw, WithValueEncoder(mustValueEncoder(t)))
	require.NoError(t, err)
	store := config.OpenSimple(loader, marshal.Json)

	require.NoError(t, store.Marshal(config.Key("a"), testConfig{Value: "one"}))
	require.NoError(t, store.Marshal(config.Key("b"), testConfig{Value: "two"}))

	var out testConfig
	desc, err := store.Unmarshal(config.Key("a"), &out)
	require.NoError(t, err)
	require.Equal(t, "a", desc.Key())
	require.Equal(t, "one", out.Value)

	var gotKeys []string
	var target testConfig
	_, err = store.List(
		config.WithStartFrom(config.Key("b")),
		config.WithLimit(1),
		config.Unmarshal(&target, func(desc config.Descriptor, value *testConfig) error {
			gotKeys = append(gotKeys, desc.Key()+"="+value.Value)
			return nil
		}),
	)
	require.NoError(t, err)
	require.Equal(t, []string{"b=two"}, gotKeys)
}

func TestLoaderWrapSimpleStorePreservesPlaintextListSemantics(t *testing.T) {
	raw := memory.Open()
	loader, err := NewLoader(raw, WithKeyCodec(reverseKeyCodec{}), WithValueEncoder(mustValueEncoder(t)))
	require.NoError(t, err)
	store := config.OpenSimple(loader, marshal.Json)

	require.NoError(t, store.Marshal(config.Key("a"), testConfig{Value: "one"}))
	require.NoError(t, store.Marshal(config.Key("b"), testConfig{Value: "two"}))
	require.NoError(t, store.Marshal(config.Key("c"), testConfig{Value: "three"}))
	require.NoError(t, store.Marshal(config.Key("d"), testConfig{Value: "four"}))

	var got []string
	var target testConfig
	descs, err := store.List(
		config.WithStartFrom(config.Key("b")),
		config.WithOffset(1),
		config.WithLimit(2),
		config.Unmarshal(&target, func(desc config.Descriptor, value *testConfig) error {
			got = append(got, desc.Key()+"="+value.Value)
			return nil
		}),
	)
	require.NoError(t, err)
	assert.Empty(t, descs)
	assert.Equal(t, []string{"c=three", "d=four"}, got)
}
