// Config store backed by bbolt.
//
// bbolt uses JSON encoding for values and is optimized for local, embedded use.
package bbolt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/config/directory"
	bolt "go.etcd.io/bbolt"
)

type Bolt struct {
	db *bolt.DB
}

type Loader struct {
	db    *bolt.DB
	scope []byte
}

type BoltStore struct {
	loader *Loader
}

type options struct {
	path    string
	timeout time.Duration
}

type Modifier func(*options) error

// WithPath specifies the filesystem path for the bbolt database.
func WithPath(path string) Modifier {
	return func(o *options) error {
		o.path = path
		return nil
	}
}

// WithTimeout sets the bbolt file lock timeout.
func WithTimeout(timeout time.Duration) Modifier {
	return func(o *options) error {
		o.timeout = timeout
		return nil
	}
}

// DefaultPath returns the default bbolt database path for an app/namespace.
func DefaultPath(app string, namespaces ...string) (string, error) {
	dir, err := directory.GetConfigDir(app, namespaces...)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.bbolt"), nil
}

// New opens a bbolt database.
func New(mods ...Modifier) (*Bolt, error) {
	db, err := openDB(mods...)
	if err != nil {
		return nil, err
	}
	return &Bolt{db: db}, nil
}

// Close releases the underlying database resources.
func (b *Bolt) Close() error {
	return b.db.Close()
}

// Open returns a JSON-backed config store scoped to the provided app and namespaces.
func (b *Bolt) Open(app string, namespaces ...string) (config.Store, error) {
	scope := storeScope(app, namespaces...)
	loader, err := newLoader(b.db, scope)
	if err != nil {
		return nil, err
	}
	return &BoltStore{loader: loader}, nil
}

func (l *Loader) List() ([]string, error) {
	var names []string
	err := l.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(l.scope)
		if bucket == nil {
			return nil
		}
		return bucket.ForEach(func(key, value []byte) error {
			if value == nil {
				return nil
			}
			names = append(names, string(key))
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return names, nil
}

func (l *Loader) Read(name string) ([]byte, error) {
	var result []byte
	err := l.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(l.scope)
		if bucket == nil {
			return os.ErrNotExist
		}
		value := bucket.Get([]byte(name))
		if value == nil {
			return os.ErrNotExist
		}
		result = append([]byte(nil), value...)
		return nil
	})
	return result, err
}

func (l *Loader) Write(name string, data []byte) error {
	return l.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists(l.scope)
		if err != nil {
			return err
		}
		return bucket.Put([]byte(name), data)
	})
}

func (l *Loader) Delete(name string) error {
	return l.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(l.scope)
		if bucket == nil {
			return os.ErrNotExist
		}
		key := []byte(name)
		if bucket.Get(key) == nil {
			return os.ErrNotExist
		}
		return bucket.Delete(key)
	})
}

func (s *BoltStore) List() ([]config.Descriptor, error) {
	names, err := s.loader.List()
	if err != nil {
		return nil, err
	}
	descs := make([]config.Descriptor, len(names))
	for i, name := range names {
		descs[i] = config.Key(name)
	}
	return descs, nil
}

func (s *BoltStore) Marshal(desc config.Descriptor, value interface{}) error {
	name, err := descriptorName(desc)
	if err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.loader.Write(name, data)
}

func (s *BoltStore) Unmarshal(desc config.Descriptor, value interface{}) (config.Descriptor, error) {
	name, err := descriptorName(desc)
	if err != nil {
		return nil, err
	}
	data, err := s.loader.Read(name)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return config.Key(name), nil
	}
	return config.Key(name), json.Unmarshal(data, value)
}

func (s *BoltStore) Delete(desc config.Descriptor) error {
	name, err := descriptorName(desc)
	if err != nil {
		return err
	}
	return s.loader.Delete(name)
}

func descriptorName(desc config.Descriptor) (string, error) {
	if desc == nil {
		return "", fmt.Errorf("bbolt store expects non-nil descriptor")
	}
	return desc.Key(), nil
}

func openDB(mods ...Modifier) (*bolt.DB, error) {
	opts := options{}
	for _, m := range mods {
		if err := m(&opts); err != nil {
			return nil, err
		}
	}
	if opts.path == "" {
		return nil, fmt.Errorf("bbolt path is required")
	}
	if err := os.MkdirAll(filepath.Dir(opts.path), 0770); err != nil {
		return nil, err
	}
	boltOpts := &bolt.Options{}
	if opts.timeout != 0 {
		boltOpts.Timeout = opts.timeout
	}
	return bolt.Open(opts.path, 0660, boltOpts)
}

func newLoader(db *bolt.DB, scope string) (*Loader, error) {
	scopeBytes := []byte(scope)
	err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(scopeBytes)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &Loader{db: db, scope: scopeBytes}, nil
}

func storeScope(app string, namespaces ...string) string {
	parts := append([]string{app}, namespaces...)
	return strings.Join(parts, "/")
}
