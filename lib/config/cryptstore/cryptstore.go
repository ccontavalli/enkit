package cryptstore

import (
	"fmt"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/token"
)

// ListOptimizingKeyCodec can customize how list options are pushed to the underlying loader.
//
// The returned keys must always be plaintext keys. The returned optimization flags indicate
// which list operations were already applied by this codec implementation.
//
// Implementations are still required to preserve the lookup semantics expected by
// config.Loader: Encode must be deterministic and Decode must recover the original
// plaintext key.
type ListOptimizingKeyCodec interface {
	config.KeyCodec
	List(downstream func(...config.ListModifier) ([]string, error), opts config.ListOptions) ([]string, config.ListOptimized, error)
}

type options struct {
	keyCodec     config.KeyCodec
	valueEncoder token.BinaryEncoder
}

// Modifier configures cryptostore wrappers.
type Modifier func(*options) error

type Modifiers []Modifier

// WithKeyCodec sets the key codec used to transform keys at rest.
//
// The configured codec must be deterministic and reversible. cryptstore encodes the
// plaintext key independently for every Read, Write, Delete, and list decode step, so
// randomized encoders cannot be used here. If omitted, cryptstore keeps plaintext keys
// and uses config.DefaultKeyCodec() for escaping.
func WithKeyCodec(codec config.KeyCodec) Modifier {
	return func(o *options) error {
		if codec == nil {
			return fmt.Errorf("key codec is required")
		}
		o.keyCodec = codec
		return nil
	}
}

// WithValueEncoder sets the encoder used to encrypt and decrypt bytes at rest.
func WithValueEncoder(encoder token.BinaryEncoder) Modifier {
	return func(o *options) error {
		if encoder == nil {
			return fmt.Errorf("value encoder is required")
		}
		o.valueEncoder = encoder
		return nil
	}
}

func defaultOptions() options {
	return options{
		keyCodec: config.DefaultKeyCodec(),
	}
}

func (mods Modifiers) Apply(opts *options) error {
	for _, mod := range mods {
		if mod == nil {
			continue
		}
		if err := mod(opts); err != nil {
			return err
		}
	}
	return nil
}

func (opts options) Validate() error {
	if opts.keyCodec == nil {
		return fmt.Errorf("key codec is required")
	}
	if opts.valueEncoder == nil {
		return fmt.Errorf("value encoder is required")
	}
	return nil
}
