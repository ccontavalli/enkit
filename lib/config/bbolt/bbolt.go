// Config store backed by bbolt.
//
// bbolt uses JSON encoding for values and is optimized for local, embedded use.
package bbolt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/config/directory"
	"github.com/ccontavalli/enkit/lib/kflags"
	bolt "go.etcd.io/bbolt"
)

type Bolt struct {
	db *bolt.DB
}

type Loader struct {
	db    *bolt.DB
	scope []byte
}

type options struct {
	path    string
	timeout time.Duration
}

type Modifier func(*options) error

// Flags holds configuration options for bbolt stores.
type Flags struct {
	// Path specifies a filesystem path to the bbolt database.
	Path string
	// Timeout sets the bbolt file lock timeout.
	Timeout time.Duration
}

// DefaultFlags returns a new Flags struct with default values.
func DefaultFlags() *Flags {
	return &Flags{}
}

// Register registers the bbolt flags with the provided FlagSet.
func (f *Flags) Register(set kflags.FlagSet, prefix string) *Flags {
	set.StringVar(&f.Path, prefix+"config-store-bbolt-path", f.Path, "Path to bbolt database (required when using bbolt)")
	set.DurationVar(&f.Timeout, prefix+"config-store-bbolt-timeout", f.Timeout, "bbolt file lock timeout (optional)")
	return f
}

// FromFlags returns a Modifier that applies bbolt flags.
func FromFlags(flags *Flags) Modifier {
	return func(o *options) error {
		if flags == nil {
			return nil
		}
		if flags.Path != "" {
			o.path = flags.Path
		}
		if flags.Timeout != 0 {
			o.timeout = flags.Timeout
		}
		return nil
	}
}

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

// Open returns a Loader scoped to the provided app and namespaces.
func (b *Bolt) Open(app string, namespaces ...string) (config.Loader, error) {
	scope := storeScope(app, namespaces...)
	loader, err := newLoader(b.db, scope)
	if err != nil {
		return nil, err
	}
	return loader, nil
}

// Explore returns a store that lists child namespaces under the provided path.
func (b *Bolt) Explore(app string, namespaces ...string) (config.Explorer, error) {
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

func (l *Loader) List(mods ...config.ListModifier) ([]string, error) {
	opts := &config.ListOptions{}
	if err := config.ListModifiers(mods).Apply(opts); err != nil {
		return nil, err
	}
	var names []string
	index := 0
	seen := 0
	err := l.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(l.scope)
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
			name := string(key)
			if opts.Data != nil {
				if err := opts.Data(config.Key(name), value); err != nil {
					return err
				}
			} else {
				names = append(names, name)
			}
			index++
			seen++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	optimized := config.OptimizedStartFrom | config.OptimizedOffsetLimit | config.OptimizedData
	return opts.FinalizeKeys(l, names, optimized)
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
	return nil
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
