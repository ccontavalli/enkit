package token

import (
	"context"
	"github.com/ccontavalli/enkit/lib/config/marshal"
	"github.com/stretchr/testify/assert"
	"math/rand"
	"testing"
	"time"
)

func TestTypeEncoder(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	be, err := NewSymmetricEncoder(rng, WithGeneratedSymmetricKey(128))
	assert.NotNil(t, be)
	assert.Nil(t, err)

	te := NewTypeEncoder(be)
	assert.NotNil(t, te)

	data1, err := te.Encode("this is a string")
	assert.Nil(t, err)
	data2, err := te.Encode("this is a string")
	assert.Nil(t, err)
	assert.NotEqual(t, data1, data2)

	var result string
	_, err = te.Decode(context.Background(), data1, &result)
	assert.Nil(t, err)
	assert.Equal(t, "this is a string", result)
}

func TestTypeEncoderMarshal(t *testing.T) {
	be := NewBase64UrlEncoder()
	assert.NotNil(t, be)

	tgob := NewTypeEncoder(be)
	assert.NotNil(t, tgob)

	tyaml := NewTypeEncoder(be, WithMarshaller(marshal.Yaml))
	assert.NotNil(t, tyaml)

	data := "When morality comes up against profit, it is seldom that profit loses."

	result1, err := tgob.Encode(data)
	assert.Nil(t, err)
	result2, err := tgob.Encode(data)
	assert.Nil(t, err)
	result3, err := tyaml.Encode(data)

	// This is just to verify that there is no entropy accidentally added.
	assert.Equal(t, result1, result2)
	assert.NotEqual(t, result1, result3)

	var decoded string
	_, err = tyaml.Decode(context.Background(), result3, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, data, decoded)
}

func TestTypeEncoderAssociatedData(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	be, err := NewSymmetricEncoder(rng, WithGeneratedSymmetricKey(128))
	assert.NoError(t, err)
	if err != nil {
		return
	}

	te := NewTypeEncoder(NewChainedEncoder(NewExpireEncoder(nil, time.Minute), be, NewBase64UrlEncoder()))
	encoded, err := te.EncodeWithAssociatedData("payload", []byte("key"))
	assert.NoError(t, err)
	if err != nil {
		return
	}

	var decoded string
	_, err = te.DecodeWithAssociatedData(context.Background(), encoded, []byte("key"), &decoded)
	assert.NoError(t, err)
	assert.Equal(t, "payload", decoded)

	_, err = te.DecodeWithAssociatedData(context.Background(), encoded, []byte("other"), &decoded)
	assert.Error(t, err)
}

func TestSupportsAssociatedData(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	symmetric, err := NewSymmetricEncoder(rng, WithGeneratedSymmetricKey(128))
	assert.NoError(t, err)
	if err != nil {
		return
	}

	assert.False(t, SupportsAssociatedData(NewChainedEncoder(
		NewExpireEncoder(nil, time.Minute),
		NewBase64UrlEncoder(),
	)))
	assert.True(t, SupportsAssociatedData(NewChainedEncoder(
		NewExpireEncoder(nil, time.Minute),
		symmetric,
		NewBase64UrlEncoder(),
	)))
	assert.False(t, NewTypeEncoder(NewChainedEncoder(
		NewExpireEncoder(nil, time.Minute),
		NewBase64UrlEncoder(),
	)).SupportsAssociatedData())
}

func TestEncodeWithAssociatedDataUnsupported(t *testing.T) {
	_, err := EncodeWithAssociatedData(NewBase64UrlEncoder(), []byte("payload"), []byte("key"))
	assert.ErrorIs(t, err, ErrAssociatedDataUnsupported)
}

func TestTypeEncoderDecodeWithAssociatedDataMalformedTokenOnSupportedChain(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	be, err := NewSymmetricEncoder(rng, WithGeneratedSymmetricKey(128))
	assert.NoError(t, err)
	if err != nil {
		return
	}

	te := NewTypeEncoder(NewChainedEncoder(NewExpireEncoder(nil, time.Minute), be, NewBase64UrlEncoder()))

	var decoded string
	_, err = te.DecodeWithAssociatedData(context.Background(), []byte("%%%"), []byte("key"), &decoded)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrAssociatedDataUnsupported)
}

func TestTypeEncoderDecodeWithAssociatedDataDoesNotExposeExpiredPayloadWhenUnsupported(t *testing.T) {
	now := time.Unix(1000, 0)
	te := NewTypeEncoder(NewChainedEncoder(
		NewExpireEncoder(func() time.Time { return now }, time.Second),
		NewBase64UrlEncoder(),
	))

	encoded, err := te.Encode("payload")
	assert.NoError(t, err)
	if err != nil {
		return
	}

	now = now.Add(2 * time.Second)

	decoded := "unchanged"
	_, err = te.DecodeWithAssociatedData(context.Background(), encoded, []byte("key"), &decoded)
	assert.ErrorIs(t, err, ErrAssociatedDataUnsupported)
	assert.ErrorIs(t, err, ExpiredError)
	assert.Equal(t, "unchanged", decoded)
}
