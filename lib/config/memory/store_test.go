package memory

import (
	"testing"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/stretchr/testify/assert"
)

type sampleValue struct {
	Name string
}

func TestStoreRoundTripPointer(t *testing.T) {
	store := NewStore()
	value := &sampleValue{Name: "a"}

	err := store.Marshal(config.Key("k"), value)
	assert.NoError(t, err)

	value.Name = "b"

	var out sampleValue
	_, err = store.Unmarshal(config.Key("k"), &out)
	assert.NoError(t, err)
	assert.Equal(t, "b", out.Name)
}

func TestStoreRoundTripValue(t *testing.T) {
	store := NewStore()
	value := sampleValue{Name: "value"}

	err := store.Marshal(config.Key("k"), value)
	assert.NoError(t, err)

	var out sampleValue
	_, err = store.Unmarshal(config.Key("k"), &out)
	assert.NoError(t, err)
	assert.Equal(t, value, out)
}

func TestStoreTypeMismatch(t *testing.T) {
	store := NewStore()
	value := sampleValue{Name: "value"}

	err := store.Marshal(config.Key("k"), value)
	assert.NoError(t, err)

	var out int
	_, err = store.Unmarshal(config.Key("k"), &out)
	assert.Error(t, err)
}
