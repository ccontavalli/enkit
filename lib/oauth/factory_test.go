package oauth

import (
	"math/rand"
	"testing"

	"github.com/ccontavalli/enkit/lib/srand"
	"github.com/ccontavalli/enkit/lib/token"
	"github.com/stretchr/testify/assert"
)

func TestSigningExtractorFlags(t *testing.T) {
	rng := rand.New(srand.Source)

	t.Run("NoKeys", func(t *testing.T) {
		flags := DefaultSigningExtractorFlags()
		_, err := NewExtractor(WithRng(rng), WithSigningExtractorFlags(flags))
		assert.NoError(t, err, "NewExtractor should succeed and generate keys if none are provided")
	})

	t.Run("AllKeysProvided", func(t *testing.T) {
		flags := DefaultSigningExtractorFlags()
		symmetricKey, err := token.GenerateSymmetricKey(rng, 256)
		assert.NoError(t, err)
		flags.SymmetricKey = symmetricKey

		verify, sign, err := token.GenerateSigningKey(rng)
		assert.NoError(t, err)
		flags.TokenSigningKey = (*sign.ToBytes())[:]
		flags.TokenVerifyingKey = (*verify.ToBytes())[:]

		_, err = NewExtractor(WithRng(rng), WithSigningExtractorFlags(flags))
		assert.NoError(t, err, "NewExtractor should succeed if all keys are provided")
	})

	t.Run("OnlySigningKey", func(t *testing.T) {
		flags := DefaultSigningExtractorFlags()
		_, sign, err := token.GenerateSigningKey(rng)
		assert.NoError(t, err)
		flags.TokenSigningKey = (*sign.ToBytes())[:]
		_, err = NewExtractor(WithRng(rng), WithSigningExtractorFlags(flags))
		assert.Error(t, err, "NewExtractor should fail if only a signing key is provided")
	})

	t.Run("OnlyVerifyingKey", func(t *testing.T) {
		flags := DefaultSigningExtractorFlags()
		verify, _, err := token.GenerateSigningKey(rng)
		assert.NoError(t, err)
		flags.TokenVerifyingKey = (*verify.ToBytes())[:]
		_, err = NewExtractor(WithRng(rng), WithSigningExtractorFlags(flags))
		assert.NoError(t, err, "NewExtractor should succeed if only a verifying key is provided for a signing extractor")
	})
}
