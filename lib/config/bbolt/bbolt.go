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

// Explore returns a store that lists child namespaces under the provided path.
func (b *Bolt) Explore(app string, namespaces ...string) (config.Explorator, error) {
	return &explorator{db: b.db, app: app, base: append([]string(nil), namespaces...)}, nil
}

type explorator struct {
	db   *bolt.DB
	app  string
	base []string
}

func (s *explorator) List(mods ...config.ListModifier) ([]config.Descriptor, error) {
	opts := &config.ListOptions{}
	if err := config.ListModifiers(mods).Apply(opts); err != nil {
		return nil, err
	}
	if opts.Unmarshal != nil {
		return nil, fmt.Errorf("namespace list does not support unmarshal")
	}

	prefix := storeScope(s.app, s.base...)
	if prefix != "" {
		prefix += "/"
	}
	childSet := map[string]struct{}{}
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.ForEach(func(name []byte, _ *bolt.Bucket) error {
			bucket := string(name)
			if !strings.HasPrefix(bucket, prefix) {
				return nil
			}
			rest := strings.TrimPrefix(bucket, prefix)
			if rest == "" {
				return nil
			}
			parts := strings.Split(rest, "/")
			if len(parts) == 0 || parts[0] == "" {
				return nil
			}
			childSet[parts[0]] = struct{}{}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}

	descs := config.SortedNamespaceDescriptors(s.base, config.KeysFromSet(childSet))
	return opts.Apply(descs, 0), nil
}

func (s *explorator) Delete(desc config.Descriptor) error {
	path := config.NamespacePathFromDescriptor(s.base, desc)
	target := storeScope(s.app, path...)
	prefix := target + "/"
	var buckets [][]byte

	err := s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.ForEach(func(name []byte, _ *bolt.Bucket) error {
			bucket := string(name)
			if bucket == target || strings.HasPrefix(bucket, prefix) {
				buckets = append(buckets, append([]byte(nil), name...))
			}
			return nil
		}); err != nil {
			return err
		}
		if len(buckets) == 0 {
			return os.ErrNotExist
		}
		for _, bucket := range buckets {
			if err := tx.DeleteBucket(bucket); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

func (s *explorator) Close() error { return nil }

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

func (l *Loader) Close() error {
	return l.db.Close()
}

func (s *BoltStore) List(mods ...config.ListModifier) ([]config.Descriptor, error) {
	opts := &config.ListOptions{}
	if err := config.ListModifiers(mods).Apply(opts); err != nil {
		return nil, err
	}
	var out []config.Descriptor
	index := 0
	seen := 0
	err := s.loader.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(s.loader.scope)
		if bucket == nil {
			return nil
		}
		cursor := bucket.Cursor()
		var key, value []byte
		if opts.StartFrom != "" {
			key, value = cursor.Seek([]byte(opts.StartFrom))
		} else {
			key, value = cursor.First()
		}
		for ; key != nil; key, value = cursor.Next() {
			if value == nil {
				continue
			}
			if index < opts.Offset {
				index++
				continue
			}
			if opts.Limit > 0 && seen >= opts.Limit {
				break
			}
			desc := config.Key(string(key))
			if opts.Unmarshal != nil {
				if err := opts.Unmarshal.UnmarshalAndCall(desc, value, json.Unmarshal); err != nil {
					return err
				}
			} else {
				out = append(out, desc)
			}
			index++
			seen++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return opts.Finalize(s, out, config.OptimizedStartFrom|config.OptimizedOffsetLimit|config.OptimizedUnmarshal)
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

func (s *BoltStore) Close() error {
	return s.loader.db.Close()
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
