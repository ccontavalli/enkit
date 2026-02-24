// Simple abstractions to read and write configuration parameters by name.
//
// At the heart of the config module there is the `Store` interface, which allows to
// load (Unmarshal) or store (Marshal) a configuration through a unique name.
//
// For example, once you have a Store object, you can store or load a configuration
// with:
//
//	config := Config{
//	    Server: "127.0.0.1",
//	    Port: 53,
//	}
//
//	if err := store.Marshal(Key("server-config"), config); err != nil {
//	   ...
//	}
//
//	... load it later ...
//
//	if _, err := store.Unmarshal(Key("server-config"), &config); err != nil {
//	   ...
//	}
//
// Naming convention:
// - New* returns a Workspace (StoreWorkspace or LoaderWorkspace).
// - Open* returns a Store or Loader.
// - Workspace.Open returns Store or Loader based on the interface.
//
// The "server-config" string is ... just a string. A key by which the configuration
// is known by. Different Store implementations will use it differently: they may
// turn it into a file name, into the key of a database, ...
//
// Internally, a `Store` does two things:
//  1. It converts your config object into some binary blob (marshal, unmarshal).
//  2. It reads and writes this blob somewhere.
//
// Some databases and config stores use their own marshalling mechanism, while
// others have no built in marshalling, and rely on a standard mechanism like
// a json, yaml, or gob encoder.
//
// If you need to store configs on a file system or database that does not have
// a native marshalling/unmarshalling scheme, you can implement the `Loader`
// interface, and then use OpenSimple() or OpenMulti() to turn a Loader into a Store.
// If you have a LoaderWorkspace, use NewSimple() or NewMulti() to turn it into a StoreWorkspace.
//
// OpenSimple and OpenMulti wrap a store around an object capable of using one
// of the standard encoders/decoders provided by go.
package config

// Represents an entry in a store.
//
// The Key() method returns the original string that was used to Marshal
// or Unamrshal the entry. Some config stores allow the same key to be
// stored in multiple entries, typically under error conditions or When
// some undesired/unexpcted condition happen (for example, if multiple
// files are allowed, and a key/value was saved first as json and then
// as toml, or a database is not configured correctly for unique keys).
//
// Descriptors allow to handle this cleanly: a descriptor always
// refers to a precise entry in the Database, so operations like
// Marshal or Delete on the Descriptor are guaranteed to work on the
// correct entry.
//
// Additionally, and more importantly, some database engines or use
// cases require transforming the key to be suitably indexed in the
// database. For example, by encrypting it, by escaping invalid
// characters, or similar. The Descriptor interface abstracts those
// mechanics.
type Descriptor interface {
	Key() string
}

// Key is a simple Descriptor implementation for string identifiers.
type Key string

func (k Key) Key() string {
	return string(k)
}

// Opener is any function that is capable of opening a store.
type Opener func(name string, namespace ...string) (Store, error)

// Explorer lists and deletes child namespaces.
type Explorer interface {
	// List returns child namespaces available under the current path.
	List(mods ...ListModifier) ([]Descriptor, error)
	// Delete removes the namespace represented by the descriptor.
	Delete(descriptor Descriptor) error
	// Close releases any resources owned by the namespace store.
	Close() error
}

// LoaderWorkspace opens namespace loaders and provides namespace exploration.
type LoaderWorkspace interface {
	Open(name string, namespace ...string) (Loader, error)
	Explore(name string, namespace ...string) (Explorer, error)
}

// StoreWorkspace opens namespace stores and provides namespace exploration.
type StoreWorkspace interface {
	Open(name string, namespace ...string) (Store, error)
	Explore(name string, namespace ...string) (Explorer, error)
}

// Store is the interface normally used from this library.
//
// It allows to load config files and store them, by using the Marshal and Unmarshal interface.
type Store interface {
	// List the object names available for unmarshalling.
	List(mods ...ListModifier) ([]Descriptor, error)

	// Marshal saves an object, specified by value, under the name specified in descriptor.
	//
	// descriptor is either a Key, indicating the desired unique name to store the
	// object as, or an object returned by Unmarshal.
	//
	// Using a descriptor returned by Unmarshal guarantees that the object is written
	// in exactly the same location where it was retrieved. This is useful for object
	// stores that allow writing in multiple locations at once.
	Marshal(descriptor Descriptor, value interface{}) error

	// Unmarshal will read an object from the config store, and parse it into the value supplied,
	// which should generally be a pointer.
	//
	// Unmarshal returns a descriptor that can be passed back to Marshal to store data into this object.
	//
	// In case the config file cannot be found, os.IsNotExist(error) will return true.
	Unmarshal(descriptor Descriptor, value interface{}) (Descriptor, error)

	// Deletes an object.
	//
	// descriptor is either a Key, indicating the desired unique name of the object
	// to delete, or an object returned by Unmarshal.
	//
	// When specifying a Key, Delete guarantees that all copies of the object known
	// by that string are deleted.
	//
	// When specifying a Descriptor, Delete will only delete that one instance of the object.
	//
	// If the object does not exist, os.IsNotExist(error) will return true.
	Delete(descriptor Descriptor) error

	// Close releases any resources owned by the store.
	Close() error
}

// Implement the Loader interface to prvoide mechanisms to read and write configuration files.
//
// If you have an object implementing the Loader interface, you can then use
// OpenSimple() or OpenMulti() to turn it into a Store.
type Loader interface {
	List(mods ...ListModifier) ([]string, error)

	// Read returns the raw stored bytes for a key.
	//
	// If the object does not exist, os.IsNotExist(error) will return true.
	Read(name string) ([]byte, error)

	// Write stores raw bytes for a key.
	Write(name string, data []byte) error

	// Delete removes a stored object.
	//
	// If the object does not exist, os.IsNotExist(error) will return true.
	Delete(name string) error

	// Close releases any resources owned by the loader.
	Close() error
}
