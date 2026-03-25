package factory

import (
	"fmt"
	"math/rand"
	"strings"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/config/bbolt"
	"github.com/ccontavalli/enkit/lib/config/cryptstore"
	"github.com/ccontavalli/enkit/lib/config/datastore"
	"github.com/ccontavalli/enkit/lib/config/directory"
	"github.com/ccontavalli/enkit/lib/config/marshal"
	"github.com/ccontavalli/enkit/lib/config/memory"
	"github.com/ccontavalli/enkit/lib/config/sqlite"
)

// NewStore creates and returns a new config.StoreWorkspace based on the provided modifiers.
func NewStore(rng *rand.Rand, mods ...Modifier) (config.StoreWorkspace, error) {
	opts := &Options{
		Flags: DefaultFlags(),
		Rng:   rng,
	}
	for _, m := range mods {
		m(opts)
	}

	useCrypto, backend, format, err := parseStoreType(opts.Flags.StoreType)
	if err != nil {
		return nil, err
	}
	switch backend {
	case "datastore":
		if useCrypto {
			return nil, fmt.Errorf("crypto wrapper requires a loader-backed config store: %s", opts.Flags.StoreType)
		}
		if format != "" {
			return nil, fmt.Errorf("datastore does not support formats")
		}
		ds, err := datastore.New(datastore.FromFlags(opts.Flags.Datastore))
		if err != nil {
			return nil, fmt.Errorf("failed to create datastore: %w", err)
		}
		return ds, nil
	case "memory":
		if format == "raw" {
			if useCrypto {
				return nil, fmt.Errorf("crypto wrapper does not support raw config stores: %s", opts.Flags.StoreType)
			}
			return memory.NewRaw(), nil
		}
		// fallthrough
		fallthrough
	case "directory", "bbolt", "sqlite":
		loaderWorkspace, err := newLoaderWorkspace(opts, backend)
		if err != nil {
			return nil, err
		}
		loaderWorkspace, err = maybeWrapCryptstore(opts, useCrypto, loaderWorkspace)
		if err != nil {
			return nil, err
		}
		return storeFromLoaderWorkspace(loaderWorkspace, format)
	default:
		return nil, fmt.Errorf("unknown config store type: %s", opts.Flags.StoreType)
	}
}

func storeFromLoaderWorkspace(workspace config.LoaderWorkspace, format string) (config.StoreWorkspace, error) {
	if format == "" {
		format = "json"
	}
	if format == storeFormatMulti {
		return config.NewMulti(workspace, marshal.Known...), nil
	}
	marshaller, err := marshallerByFormat(format)
	if err != nil {
		return nil, err
	}
	return config.NewSimple(workspace, marshaller), nil
}

// NewLoader creates and returns a new config.LoaderWorkspace based on the provided modifiers.
func NewLoader(rng *rand.Rand, mods ...Modifier) (config.LoaderWorkspace, error) {
	opts := &Options{
		Flags: DefaultFlags(),
		Rng:   rng,
	}
	for _, m := range mods {
		m(opts)
	}
	useCrypto, backend, format, err := parseStoreType(opts.Flags.StoreType)
	if err != nil {
		return nil, err
	}
	if format != "" {
		return nil, fmt.Errorf("loader workspace does not accept format: %s", format)
	}
	workspace, err := newLoaderWorkspace(opts, backend)
	if err != nil {
		return nil, err
	}
	return maybeWrapCryptstore(opts, useCrypto, workspace)
}

func newLoaderWorkspace(opts *Options, backend string) (config.LoaderWorkspace, error) {
	switch backend {
	case "directory":
		return directory.New("", directory.FromFlags(opts.Flags.Directory)), nil
	case "memory":
		return memory.NewMarshal(), nil
	case "bbolt":
		return bbolt.New(bbolt.FromFlags(opts.Flags.Bbolt))
	case "sqlite":
		return sqlite.New(sqlite.FromFlags(opts.Flags.SQLite))
	default:
		return nil, fmt.Errorf("unknown loader backend: %s", backend)
	}
}

func maybeWrapCryptstore(opts *Options, enabled bool, workspace config.LoaderWorkspace) (config.LoaderWorkspace, error) {
	if !enabled {
		return workspace, nil
	}
	flags := opts.Flags.Crypt
	if flags == nil {
		flags = cryptstore.DefaultFlags()
	}
	return cryptstore.NewLoaderWorkspace(workspace, cryptstore.FromFlags(flags, opts.Rng))
}

func parseStoreType(storeType string) (bool, string, string, error) {
	if storeType == "" {
		return false, "", "", nil
	}
	parts := strings.Split(storeType, ":")
	useCrypto := false
	if parts[0] == "crypto" {
		useCrypto = true
		parts = parts[1:]
	}
	if len(parts) == 0 || len(parts) > 2 || parts[0] == "" {
		return false, "", "", fmt.Errorf("invalid config store type: %s", storeType)
	}
	if len(parts) == 1 {
		return useCrypto, parts[0], "", nil
	}
	return useCrypto, parts[0], parts[1], nil
}
