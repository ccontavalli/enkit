package token

import (
	"fmt"
	"time"

	"github.com/ccontavalli/enkit/lib/kflags"
)

// Flags configures symmetric token encryption.
//
// Use Register to populate the fields from flags, and WithFlags to apply them
// to a SymmetricEncoder.
type Flags struct {
	// SymmetricKey holds the key bytes read from a file or embedded asset.
	SymmetricKey []byte

	// Validity is an optional token validity duration (0 disables expiry).
	Validity time.Duration
}

func DefaultFlags() *Flags {
	return &Flags{}
}

func (f *Flags) Register(set kflags.FlagSet, prefix string) *Flags {
	set.ByteFileVar(&f.SymmetricKey, prefix+"token-encryption-key", "",
		"Path of the file (or embedded asset) containing the symmetric key to use to encrypt/decrypt tokens")
	set.DurationVar(&f.Validity, prefix+"token-validity", f.Validity,
		"How long should generated tokens be valid for (0 disables expiry)")
	return f
}

// WithFlags applies the configuration from Flags to a SymmetricEncoder.
//
// If SymmetricKey is empty, a new key is generated using the encoder RNG.
func WithFlags(f *Flags) SymmetricSetter {
	return func(be *SymmetricEncoder) error {
		if f == nil {
			return fmt.Errorf("token flags cannot be nil")
		}
		if len(f.SymmetricKey) == 0 {
			if be.rng == nil {
				return fmt.Errorf("rng must be set to generate symmetric key")
			}
			key, err := GenerateSymmetricKey(be.rng, 0)
			if err != nil {
				return err
			}
			f.SymmetricKey = key
		}
		return UseSymmetricKey(f.SymmetricKey)(be)
	}
}
