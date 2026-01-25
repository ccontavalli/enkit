package config

import (
	"strings"
)

// KeyCodec encodes and decodes store keys for safe storage.
type KeyCodec interface {
	Encode(string) string
	Decode(string) string
}

type defaultKeyCodec struct{}

func (defaultKeyCodec) Encode(key string) string {
	return EncodeKey(key)
}

func (defaultKeyCodec) Decode(key string) string {
	return DecodeKey(key)
}

type storeOptions struct {
	keyCodec KeyCodec
}

// StoreOption configures store creation.
type StoreOption func(*storeOptions)

// WithKeyCodec overrides the key encoder/decoder used by stores.
func WithKeyCodec(codec KeyCodec) StoreOption {
	return func(o *storeOptions) {
		if codec == nil {
			panic("nil KeyCodec")
		}
		o.keyCodec = codec
	}
}

func defaultStoreOptions() storeOptions {
	return storeOptions{keyCodec: DefaultKeyCodec()}
}

func applyStoreOptions(opts ...StoreOption) storeOptions {
	options := defaultStoreOptions()
	for _, opt := range opts {
		opt(&options)
	}
	return options
}

// DefaultKeyCodec returns the standard key encoder/decoder.
func DefaultKeyCodec() KeyCodec {
	return defaultKeyCodec{}
}

// EncodeKey encodes only '/', '%', and NUL bytes using %XX escapes.
func EncodeKey(key string) string {
	if key == "" {
		return key
	}
	var b strings.Builder
	b.Grow(len(key))
	for i := 0; i < len(key); i++ {
		switch key[i] {
		case '/', '%', 0:
			b.WriteString("%")
			b.WriteByte(toHex(key[i] >> 4))
			b.WriteByte(toHex(key[i] & 0x0f))
		default:
			b.WriteByte(key[i])
		}
	}
	return b.String()
}

// DecodeKey decodes any %XX sequences, leaving invalid escapes untouched.
func DecodeKey(encoded string) string {
	if encoded == "" {
		return encoded
	}
	var b strings.Builder
	b.Grow(len(encoded))
	for i := 0; i < len(encoded); i++ {
		if encoded[i] != '%' || i+2 >= len(encoded) {
			b.WriteByte(encoded[i])
			continue
		}
		hi, ok1 := fromHex(encoded[i+1])
		lo, ok2 := fromHex(encoded[i+2])
		if !ok1 || !ok2 {
			b.WriteByte(encoded[i])
			continue
		}
		b.WriteByte((hi << 4) | lo)
		i += 2
	}
	return b.String()
}

func toHex(v byte) byte {
	if v < 10 {
		return '0' + v
	}
	return 'A' + (v - 10)
}

func fromHex(b byte) (byte, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	default:
		return 0, false
	}
}
