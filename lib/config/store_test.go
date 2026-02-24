package config_test

import (
	"testing"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/config/directory"
	"github.com/ccontavalli/enkit/lib/config/marshal"
	"github.com/stretchr/testify/assert"
)

func TestStoreImplementations(t *testing.T) {
	hd, err := directory.OpenHomeDir("application")
	assert.Nil(t, err)

	var _ = []config.Loader{
		hd,
	}

	var _ = []config.Store{
		config.OpenSimple(hd, marshal.Json),
		config.OpenMulti(hd, marshal.Toml, marshal.Json),
	}
}
