package cryptstore

import (
	"context"
	"fmt"
	"github.com/ccontavalli/enkit/lib/token"
	"math/rand"
)

// NewRandomSymmetricValueEncoder builds an AES-GCM value encoder with random nonce per encryption.
func NewRandomSymmetricValueEncoder(rng *rand.Rand, key []byte) (token.BinaryEncoder, error) {
	if rng == nil {
		return nil, fmt.Errorf("rng is required")
	}
	if len(key) == 0 {
		return nil, fmt.Errorf("key is required")
	}
	return token.NewSymmetricEncoder(rng, token.UseSymmetricKey(key))
}

// DeterministicTokenKeyCodec encrypts keys with AES-GCM and a fixed nonce.
//
// This preserves exact key lookup behavior but does not preserve lexicographic ordering.
type DeterministicTokenKeyCodec struct {
	encoder token.BinaryEncoder
}

// NewDeterministicTokenKeyCodec creates a deterministic key codec using key+fixed nonce.
//
// The fixed nonce is intentional: cryptstore requires stable key encoding so later reads
// and deletes can find the same backend entry that was written earlier.
func NewDeterministicTokenKeyCodec(key []byte, nonce []byte) (*DeterministicTokenKeyCodec, error) {
	if len(key) == 0 {
		return nil, fmt.Errorf("key is required")
	}
	if len(nonce) == 0 {
		return nil, fmt.Errorf("fixed nonce is required")
	}
	symmetric, err := token.NewSymmetricEncoder(nil, token.UseSymmetricKey(key), token.UseFixedNonce(nonce))
	if err != nil {
		return nil, err
	}
	encoder := token.NewChainedEncoder(symmetric, token.NewBase64UrlEncoder())
	return &DeterministicTokenKeyCodec{encoder: encoder}, nil
}

func (c *DeterministicTokenKeyCodec) Encode(key string) (string, error) {
	cipher, err := c.encoder.Encode([]byte(key))
	if err != nil {
		return "", err
	}
	return string(cipher), nil
}

func (c *DeterministicTokenKeyCodec) Decode(encoded string) (string, error) {
	_, plain, err := c.encoder.Decode(context.Background(), []byte(encoded))
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
