package typed_test

import (
	"testing"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/config/memory"
	"github.com/ccontavalli/enkit/lib/config/typed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type typedConfig struct {
	Value string
}

func TestTypedStoreHelpers(t *testing.T) {
	ws := memory.NewRaw()
	store, err := typed.OpenAs[typedConfig](ws, "app", "ns")
	require.NoError(t, err)
	defer store.Close()

	require.NoError(t, store.Marshal(config.Key("one"), typedConfig{Value: "a"}))
	require.NoError(t, store.Marshal(config.Key("two"), typedConfig{Value: "b"}))

	var out typedConfig
	_, err = store.Unmarshal(config.Key("one"), &out)
	require.NoError(t, err)
	assert.Equal(t, "a", out.Value)

	got, _, err := store.Get(config.Key("two"))
	require.NoError(t, err)
	assert.Equal(t, "b", got.Value)

	values, err := store.Values()
	require.NoError(t, err)
	assert.Len(t, values, 2)

	seen := map[string]bool{}
	err = store.Each(func(desc config.Descriptor, cfg *typedConfig) error {
		seen[desc.Key()] = true
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"one": true, "two": true}, seen)
}
