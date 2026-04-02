// Package factory provides a mechanism to create config workspaces based on flags.
//
// Example usage:
//
//	flags := factory.DefaultFlags().Register(flagSet, "my-app")
//	...
//	workspace, err := factory.NewStore(rng, factory.FromFlags(flags))
//	if err != nil { ... }
//
//	store, err := workspace.Open("my-app", "users")
package factory

import (
	"fmt"
	"math/rand"

	"github.com/ccontavalli/enkit/lib/config/bbolt"
	"github.com/ccontavalli/enkit/lib/config/cryptstore"
	"github.com/ccontavalli/enkit/lib/config/datastore"
	"github.com/ccontavalli/enkit/lib/config/directory"
	"github.com/ccontavalli/enkit/lib/config/marshal"
	"github.com/ccontavalli/enkit/lib/config/sqlite"
	"github.com/ccontavalli/enkit/lib/kflags"
)

// Flags holds the configuration options for creating a config store.
// These are typically populated from command-line flags.
const (
	storeFormatMulti = "multi"
)

type Flags struct {
	// StoreType determines the backend and optional format to use.
	// Examples: "directory:toml", "directory:multi", "memory:json",
	// "bbolt:json", "crypto:directory:toml".
	StoreType string
	// Datastore holds Datastore-specific configuration.
	Datastore *datastore.Flags
	// Directory holds Directory-specific configuration.
	Directory *directory.Flags
	// SQLite holds SQLite-specific configuration.
	SQLite *sqlite.Flags
	// Bbolt holds bbolt-specific configuration.
	Bbolt *bbolt.Flags
	// Crypt holds cryptstore-specific configuration used when StoreType has a
	// "crypto:" prefix.
	Crypt *cryptstore.Flags
}

// DefaultFlags returns a new Flags struct with sensible default values.
//
// By default, it selects the "directory" store type.
func DefaultFlags() *Flags {
	return &Flags{
		StoreType: "directory:toml",
		SQLite:    sqlite.DefaultFlags(),
		Bbolt:     bbolt.DefaultFlags(),
		Datastore: datastore.DefaultFlags(),
		Directory: directory.DefaultFlags(),
		Crypt:     cryptstore.DefaultFlags(),
	}
}

// DefaultConfigFileFlags returns store flags suitable for applications that
// want --config to address a concrete, readable and writable config file.
//
// The returned flags use a directory-backed multi-format store rooted at "/",
// so backend-native path parsing can accept filesystem paths and the file
// extension selects the serialization format.
//
// Example:
// Use this if you want --config=/etc/config.yaml to resolve to that specific
// file on disk.
func DefaultConfigFileFlags() *Flags {
	flags := DefaultFlags()
	flags.StoreType = "directory:multi"
	if flags.Directory == nil {
		flags.Directory = directory.DefaultFlags()
	}
	flags.Directory.Path = "/"
	return flags
}

// DefaultAppConfigFlags returns store flags suitable for an application's
// default config object stored under its normal app/namespace location.
//
// The returned flags use a directory-backed multi-format store with the normal
// backend-selected root, so callers can resolve a logical app/key path via
// ResolvePathWithinStore without hardcoding filesystem layout.
//
// Example:
// Use this if you want --config=enproxy/config to mean "load application
// enproxy, key config" from the configured store backend.
//
// With the default directory store on a Linux system, that would resolve to
// a file such as ~/.config/enproxy/config.toml, ~/.config/enproxy/config.yaml,
// or ~/.config/enproxy/config.json, depending on which format exists or is
// written by the selected store wrapper.
func DefaultAppConfigFlags() *Flags {
	flags := DefaultFlags()
	flags.StoreType = "directory:multi"
	if flags.Directory == nil {
		flags.Directory = directory.DefaultFlags()
	}
	flags.Directory.Path = ""
	return flags
}

// Register registers the configuration flags with the provided FlagSet.
//
// The flags will be prefixed with the given string.
// For example, if prefix is "server-", the flags will be "--server-config-store", etc.
func (f *Flags) Register(set kflags.FlagSet, prefix string) *Flags {
	set.StringVar(&f.StoreType, prefix+"config-store", f.StoreType, "Type of config store to use (backend[:format] or crypto:backend[:format])")
	f.SQLite.Register(set, prefix)
	f.Bbolt.Register(set, prefix)
	f.Datastore.Register(set, prefix)
	f.Directory.Register(set, prefix)
	if f.Crypt == nil {
		f.Crypt = cryptstore.DefaultFlags()
	}
	f.Crypt.Register(set, prefix)
	return f
}

// Options holds the internal configuration for the factory.
type Options struct {
	Flags *Flags
	Rng   *rand.Rand
}

// Modifier is a function that modifies the factory Options.
type Modifier func(*Options)

// FromFlags returns a Modifier that sets the factory configuration from the provided Flags.
func FromFlags(flags *Flags) Modifier {
	return func(o *Options) {
		o.Flags = flags
	}
}

func marshallerByFormat(format string) (marshal.FileMarshaller, error) {
	if format == "" {
		return nil, fmt.Errorf("format is required")
	}
	marshaller := marshal.ByFormat(format)
	if marshaller == nil {
		return nil, fmt.Errorf("unknown format: %s", format)
	}
	return marshaller, nil
}

// New creates and returns a new config.Opener based on the provided modifiers.
//
// The returned Opener can be used to open specific configuration stores (namespaces)
// using the configured backend.
