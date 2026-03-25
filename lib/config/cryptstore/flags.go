package cryptstore

import (
	"fmt"
	"math/rand"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/kflags"
)

const (
	// KeyModePlain keeps plaintext keys and applies the default escaping codec.
	KeyModePlain = "plain"
	// KeyModeDeterministicToken encrypts keys with a deterministic token codec.
	KeyModeDeterministicToken = "deterministic-token"
)

// Flags configures cryptstore wrappers from a CLI-friendly surface.
type Flags struct {
	// KeyMode selects how keys are stored at rest. Valid values are "plain" and
	// "deterministic-token".
	KeyMode string

	// KeyEncryptionKey is used only when KeyMode is "deterministic-token".
	KeyEncryptionKey []byte
	// KeyEncryptionNonce is used only when KeyMode is "deterministic-token".
	KeyEncryptionNonce []byte

	// ValueEncryptionKey is always required. It is used with random nonces so
	// repeated writes of the same plaintext produce different ciphertext.
	ValueEncryptionKey []byte
}

// DefaultFlags returns a new Flags struct with default values.
func DefaultFlags() *Flags {
	return &Flags{
		KeyMode: KeyModePlain,
	}
}

// Register registers cryptstore flags with the provided FlagSet.
func (f *Flags) Register(set kflags.FlagSet, prefix string) *Flags {
	set.StringVar(&f.KeyMode, prefix+"config-store-crypt-key-mode", f.KeyMode, "How to store keys at rest: plain or deterministic-token")
	set.ByteFileVar(&f.KeyEncryptionKey, prefix+"config-store-crypt-key-encryption-key", "", "Path to the symmetric key file used when --"+prefix+"config-store-crypt-key-mode=deterministic-token")
	set.ByteFileVar(&f.KeyEncryptionNonce, prefix+"config-store-crypt-key-encryption-nonce", "", "Path to the fixed nonce file used when --"+prefix+"config-store-crypt-key-mode=deterministic-token")
	set.ByteFileVar(&f.ValueEncryptionKey, prefix+"config-store-crypt-value-encryption-key", "", "Path to the symmetric key file used to encrypt stored values")
	return f
}

// FromFlags returns a Modifier that applies the provided flags.
//
// rng is required to build the value encoder, which uses random nonces per write.
func FromFlags(flags *Flags, rng *rand.Rand) Modifier {
	return func(o *options) error {
		if flags == nil {
			return nil
		}
		if rng == nil {
			return fmt.Errorf("rng is required")
		}
		if len(flags.ValueEncryptionKey) == 0 {
			return fmt.Errorf("value encryption key is required")
		}

		valueEncoder, err := NewRandomSymmetricValueEncoder(rng, flags.ValueEncryptionKey)
		if err != nil {
			return err
		}
		if err := WithValueEncoder(valueEncoder)(o); err != nil {
			return err
		}

		switch flags.KeyMode {
		case "", KeyModePlain:
			return WithKeyCodec(config.DefaultKeyCodec())(o)
		case KeyModeDeterministicToken:
			keyCodec, err := NewDeterministicTokenKeyCodec(flags.KeyEncryptionKey, flags.KeyEncryptionNonce)
			if err != nil {
				return err
			}
			return WithKeyCodec(keyCodec)(o)
		default:
			return fmt.Errorf("unknown cryptstore key mode: %s", flags.KeyMode)
		}
	}
}

// WithFlags is an alias for FromFlags.
func WithFlags(flags *Flags, rng *rand.Rand) Modifier {
	return FromFlags(flags, rng)
}
