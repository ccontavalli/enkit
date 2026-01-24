// Package factory provides a mechanism to create a config.Opener based on configuration flags.
//
// It simplifies the process of initializing a configuration backend (like a directory-based store
// or Google Cloud Datastore) by abstracting the setup behind a set of standard flags.
//
// Example usage:
//
//	flags := factory.DefaultFlags().Register(flagSet, "my-app")
//	...
//	opener, err := factory.New(factory.FromFlags(flags))
//	if err != nil { ... }
//
//	store, err := opener("my-app", "users")
package factory

import (
	"fmt"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/config/datastore"
	"github.com/ccontavalli/enkit/lib/config/directory"
	"github.com/ccontavalli/enkit/lib/config/marshal"
	"github.com/ccontavalli/enkit/lib/config/sqlite"
	"github.com/ccontavalli/enkit/lib/kflags"
)

// Flags holds the configuration options for creating a config store.
// These are typically populated from command-line flags.
const (
	directoryModeSimple = "simple"
	directoryModeMulti  = "multi"
)

type Flags struct {
	// StoreType determines the backend to use. Supported values: "directory", "datastore", "sqlite".
	// StoreType determines the backend to use. Supported values: "directory", "datastore", "sqlite".
	StoreType string
	// DatastoreProject specifies the Google Cloud Project ID when using the "datastore" backend.
	// If empty, the library attempts to detect the project ID from the environment.
	DatastoreProject string
	// DirectoryPath specifies a custom root directory for the "directory" backend.
	// If empty, the user's default configuration directory (e.g., ~/.config/appname) is used.
	DirectoryPath string
	// DirectoryMode selects the directory store implementation: "simple" or "multi".
	DirectoryMode string
	// DirectoryFormat selects the marshalling format for the simple directory store.
	// Valid values: toml, json, yaml, gob. Ignored for multi mode.
	DirectoryFormat string
	// SQLite holds SQLite-specific configuration.
	SQLite *sqlite.Flags
}

// DefaultFlags returns a new Flags struct with sensible default values.
//
// By default, it selects the "directory" store type.
func DefaultFlags() *Flags {
	storeType := "directory"
	return &Flags{
		StoreType:       storeType,
		DirectoryMode:   directoryModeSimple,
		DirectoryFormat: "toml",
		SQLite:          sqlite.DefaultFlags(),
	}
}

// Register registers the configuration flags with the provided FlagSet.
//
// The flags will be prefixed with the given string.
// For example, if prefix is "server-", the flags will be "--server-config-store", etc.
func (f *Flags) Register(set kflags.FlagSet, prefix string) *Flags {
	set.StringVar(&f.StoreType, prefix+"config-store", f.StoreType, "Type of config store to use (datastore, directory, sqlite)")
	set.StringVar(&f.DatastoreProject, prefix+"config-store-datastore-project", f.DatastoreProject, "Project ID for Datastore config backend (optional, defaults to auto-detect)")
	set.StringVar(&f.DirectoryPath, prefix+"config-store-directory-path", f.DirectoryPath, "Custom path for Directory config backend (optional, defaults to user config dir)")
	set.StringVar(&f.DirectoryMode, prefix+"config-store-directory-mode", f.DirectoryMode, "Directory store mode (simple or multi)")
	set.StringVar(&f.DirectoryFormat, prefix+"config-store-directory-format", f.DirectoryFormat, "Directory store format for simple mode (toml, json, yaml, gob)")
	f.SQLite.Register(set, prefix)
	return f
}

// Options holds the internal configuration for the factory.
type Options struct {
	Flags *Flags
}

// Modifier is a function that modifies the factory Options.
type Modifier func(*Options)

// FromFlags returns a Modifier that sets the factory configuration from the provided Flags.
func FromFlags(flags *Flags) Modifier {
	return func(o *Options) {
		o.Flags = flags
	}
}

// WithDirectoryMode overrides the directory store mode (simple or multi).
func WithDirectoryMode(mode string) Modifier {
	return func(o *Options) {
		if o.Flags == nil {
			o.Flags = DefaultFlags()
		}
		o.Flags.DirectoryMode = mode
	}
}

// WithDirectoryFormat overrides the directory store format used by simple mode.
func WithDirectoryFormat(format string) Modifier {
	return func(o *Options) {
		if o.Flags == nil {
			o.Flags = DefaultFlags()
		}
		o.Flags.DirectoryFormat = format
	}
}

// WithDirectorySimple selects the simple directory store with the requested format.
func WithDirectorySimple(format string) Modifier {
	return func(o *Options) {
		if o.Flags == nil {
			o.Flags = DefaultFlags()
		}
		o.Flags.DirectoryMode = directoryModeSimple
		o.Flags.DirectoryFormat = format
	}
}

// WithDirectoryMulti selects the multi-format directory store.
func WithDirectoryMulti() Modifier {
	return func(o *Options) {
		if o.Flags == nil {
			o.Flags = DefaultFlags()
		}
		o.Flags.DirectoryMode = directoryModeMulti
		o.Flags.DirectoryFormat = ""
	}
}

func directoryMarshaller(format string) (marshal.FileMarshaller, error) {
	if format == "" {
		format = "toml"
	}
	switch format {
	case "toml":
		return marshal.Toml, nil
	case "json":
		return marshal.Json, nil
	case "yaml":
		return marshal.Yaml, nil
	case "gob":
		return marshal.Gob, nil
	default:
		return nil, fmt.Errorf("unknown directory format: %s", format)
	}
}

// New creates and returns a new config.Opener based on the provided modifiers.
//
// The returned Opener can be used to open specific configuration stores (namespaces)
// using the configured backend.
func New(mods ...Modifier) (config.Opener, error) {
	opts := &Options{
		Flags: DefaultFlags(),
	}
	for _, m := range mods {
		m(opts)
	}

	switch opts.Flags.StoreType {
	case "datastore":
		dsMods := []datastore.Modifier{}
		if opts.Flags.DatastoreProject != "" {
			dsMods = append(dsMods, datastore.WithProject(opts.Flags.DatastoreProject))
		}
		ds, err := datastore.New(dsMods...)
		if err != nil {
			return nil, fmt.Errorf("failed to create datastore: %w", err)
		}
		return ds.Open, nil

	case "directory":
		return func(name string, namespace ...string) (config.Store, error) {
			var loader config.Loader
			var err error

			if opts.Flags.DirectoryPath != "" {
				// If custom path provided, use OpenDir with that path as base.
				// We append the app name and namespace to the custom path to maintain structure.
				parts := append([]string{opts.Flags.DirectoryPath, name}, namespace...)
				loader, err = directory.OpenDir(parts[0], parts[1:]...)
			} else {
				loader, err = directory.OpenHomeDir(name, namespace...)
			}

			if err != nil {
				return nil, err
			}

			switch opts.Flags.DirectoryMode {
			case "", directoryModeSimple:
				marshaller, err := directoryMarshaller(opts.Flags.DirectoryFormat)
				if err != nil {
					return nil, err
				}
				return config.NewSimple(loader, marshaller), nil
			case directoryModeMulti:
				if opts.Flags.DirectoryFormat != "" {
					return nil, fmt.Errorf("directory format is only valid for simple mode")
				}
				return config.NewMulti(loader, marshal.Known...), nil
			default:
				return nil, fmt.Errorf("unknown directory mode: %s", opts.Flags.DirectoryMode)
			}
		}, nil

	case "sqlite":
		return func(name string, namespace ...string) (config.Store, error) {
			db, err := sqlite.New(sqlite.FromFlags(opts.Flags.SQLite, name, namespace...))
			if err != nil {
				return nil, err
			}
			return db.Open(name, namespace...)
		}, nil

	default:
		return nil, fmt.Errorf("unknown config store type: %s", opts.Flags.StoreType)
	}
}
