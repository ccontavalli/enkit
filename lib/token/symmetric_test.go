package token

import (
	"context"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSymmetric(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	be, err := NewSymmetricEncoder(rng)
	assert.Nil(t, be)
	assert.NotNil(t, err)

	be, err = NewSymmetricEncoder(rng, WithGeneratedSymmetricKey(12))
	assert.Nil(t, be)
	assert.NotNil(t, err)

	be, err = NewSymmetricEncoder(rng, WithGeneratedSymmetricKey(128))
	assert.NotNil(t, be)
	assert.Nil(t, err)

	data, err := be.Encode([]byte{1, 2, 3, 4})
	assert.Nil(t, err)
	_, original, err := be.Decode(context.Background(), data)
	assert.Nil(t, err)
	assert.Equal(t, []byte{1, 2, 3, 4}, original)
}

func TestSymmetricFixedNonceDeterministic(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	key, err := GenerateSymmetricKey(rng, 128)
	assert.NoError(t, err)
	if err != nil {
		return
	}

	be, err := NewSymmetricEncoder(rng, UseSymmetricKey(key), UseFixedNonce(nil))
	assert.NoError(t, err)
	if err != nil {
		return
	}

	encoded1, err := be.Encode([]byte("payload"))
	assert.NoError(t, err)
	if err != nil {
		return
	}
	encoded2, err := be.Encode([]byte("payload"))
	assert.NoError(t, err)
	if err != nil {
		return
	}

	assert.Equal(t, encoded1, encoded2, "fixed nonce should produce deterministic output")
}

func TestSymmetricFixedNonceEmpty(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	key, err := GenerateSymmetricKey(rng, 128)
	assert.NoError(t, err)
	if err != nil {
		return
	}

	be, err := NewSymmetricEncoder(rng, UseSymmetricKey(key), UseFixedNonce([]byte{}))
	assert.NoError(t, err)
	if err != nil {
		return
	}

	encoded1, err := be.Encode([]byte("payload"))
	assert.NoError(t, err)
	if err != nil {
		return
	}
	encoded2, err := be.Encode([]byte("payload"))
	assert.NoError(t, err)
	if err != nil {
		return
	}

	assert.Equal(t, encoded1, encoded2, "empty nonce should produce deterministic output")
}

func TestSymmetricFixedNonceWrongSize(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	key, err := GenerateSymmetricKey(rng, 128)
	assert.NoError(t, err)
	if err != nil {
		return
	}

	be, err := NewSymmetricEncoder(rng, UseSymmetricKey(key), UseFixedNonce([]byte{0x01}))
	assert.Error(t, err)
	assert.Nil(t, be)
}
