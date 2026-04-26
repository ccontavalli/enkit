package blob

import (
	"math/rand"
	"net/url"
	"testing"
	"time"

	"github.com/ccontavalli/enkit/lib/token"
	"github.com/stretchr/testify/assert"
)

func TestTokenParamsOnlyCodecIgnoresUnsignedQueryParams(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	codec, err := NewTokenParamsOnlyCodec(rng, "token")
	assert.NoError(t, err)
	if err != nil {
		return
	}

	signed := url.Values{}
	signed.Set("filename", "signed.txt")
	signed.Set("content-type", "text/plain")

	encodedKey, encodedParams, err := codec.Encode("blob-key", signed)
	assert.NoError(t, err)
	if err != nil {
		return
	}

	mixed := url.Values{}
	for k, v := range encodedParams {
		mixed[k] = append([]string(nil), v...)
	}
	mixed.Set("filename", "unsigned.txt")
	mixed.Set("content-type", "application/octet-stream")
	mixed.Set("extra", "ignored")

	decodedKey, decodedParams, err := codec.Decode(encodedKey, mixed)
	assert.NoError(t, err)
	assert.Equal(t, "blob-key", decodedKey)
	assert.Equal(t, signed, decodedParams)
}

func TestTokenParamsCodecIgnoresUnsignedQueryParams(t *testing.T) {
	rng := rand.New(rand.NewSource(6))
	codec, err := NewTokenParamsCodec(rng, "token")
	assert.NoError(t, err)
	if err != nil {
		return
	}

	signed := url.Values{}
	signed.Set("filename", "signed.txt")
	signed.Set("content-type", "text/plain")

	encodedKey, encodedParams, err := codec.Encode("blob-key", signed)
	assert.NoError(t, err)
	if err != nil {
		return
	}

	mixed := url.Values{}
	for k, v := range encodedParams {
		mixed[k] = append([]string(nil), v...)
	}
	mixed.Set("filename", "unsigned.txt")
	mixed.Set("content-type", "application/octet-stream")
	mixed.Set("extra", "ignored")

	decodedKey, decodedParams, err := codec.Decode(encodedKey, mixed)
	assert.NoError(t, err)
	assert.Equal(t, "blob-key", decodedKey)
	assert.Equal(t, signed, decodedParams)
}

func TestTokenParamsCodecRejectsDifferentKey(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	codec, err := NewTokenParamsCodec(rng, "token")
	assert.NoError(t, err)
	if err != nil {
		return
	}

	_, encodedParams, err := codec.Encode("blob-key", url.Values{"filename": []string{"signed.txt"}})
	assert.NoError(t, err)
	if err != nil {
		return
	}

	_, _, err = codec.Decode("other-key", encodedParams)
	assert.Error(t, err)
}

func TestTokenParamsOnlyCodecRejectsMissingToken(t *testing.T) {
	rng := rand.New(rand.NewSource(8))
	codec, err := NewTokenParamsOnlyCodec(rng, "token")
	assert.NoError(t, err)
	if err != nil {
		return
	}

	_, _, err = codec.Decode("blob-key", url.Values{"filename": []string{"unsigned.txt"}})
	assert.Error(t, err)
}

func TestTokenParamsCodecRejectsMissingToken(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	codec, err := NewTokenParamsCodec(rng, "token")
	assert.NoError(t, err)
	if err != nil {
		return
	}

	_, _, err = codec.Decode("blob-key", url.Values{"filename": []string{"unsigned.txt"}})
	assert.Error(t, err)
}

func TestTokenParamsCodecRejectsEncoderWithoutAssociatedData(t *testing.T) {
	codec, err := NewTokenParamsCodec(nil, "token", WithEncoder(token.NewBase64UrlEncoder()))
	assert.Nil(t, codec)
	assert.ErrorIs(t, err, token.ErrAssociatedDataUnsupported)
}

func TestTokenParamsCodecRejectsChainedEncoderWithoutAssociatedData(t *testing.T) {
	codec, err := NewTokenParamsCodec(nil, "token", WithEncoder(token.NewChainedEncoder(
		token.NewExpireEncoder(nil, time.Minute),
		token.NewBase64UrlEncoder(),
	)))
	assert.Nil(t, codec)
	assert.ErrorIs(t, err, token.ErrAssociatedDataUnsupported)
}
