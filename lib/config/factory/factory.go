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
