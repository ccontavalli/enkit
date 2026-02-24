package factory

import (
	"fmt"
	"strings"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/config/bbolt"
	"github.com/ccontavalli/enkit/lib/config/datastore"
	"github.com/ccontavalli/enkit/lib/config/directory"
	"github.com/ccontavalli/enkit/lib/config/marshal"
	"github.com/ccontavalli/enkit/lib/config/memory"
	"github.com/ccontavalli/enkit/lib/config/sqlite"
)

// NewStore creates and returns a new config.StoreWorkspace based on the provided modifiers.
func NewStore(mods ...Modifier) (config.StoreWorkspace, error) {
	opts := &Options{
		Flags: DefaultFlags(),
	}
	for _, m := range mods {
		m(opts)
	}

	backend, format := parseStoreType(opts.Flags.StoreType)
	switch backend {
	case "datastore":
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
			return memory.NewRaw(), nil
		}
		// fallthrough
		fallthrough
	case "directory", "bbolt", "sqlite":
		loaderWorkspace, err := newLoaderWorkspace(opts, backend)
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
func NewLoader(mods ...Modifier) (config.LoaderWorkspace, error) {
	opts := &Options{
		Flags: DefaultFlags(),
	}
	for _, m := range mods {
		m(opts)
	}
	backend, format := parseStoreType(opts.Flags.StoreType)
	if format != "" {
		return nil, fmt.Errorf("loader workspace does not accept format: %s", format)
	}
	return newLoaderWorkspace(opts, backend)
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

func parseStoreType(storeType string) (string, string) {
	if storeType == "" {
		return "", ""
	}
	parts := strings.SplitN(storeType, ":", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}
