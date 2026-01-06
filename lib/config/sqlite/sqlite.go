// Config store backed by SQLite.
package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/config/directory"
	"github.com/ccontavalli/enkit/lib/config/marshal"
	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS configs (
  scope TEXT NOT NULL,
  name TEXT NOT NULL,
  data BLOB NOT NULL,
  PRIMARY KEY (scope, name)
);
`

type SQLite struct {
	db *sql.DB
}

type options struct {
	dsn string
}

type Modifier func(*options)

// WithDSN specifies the SQLite data source name.
func WithDSN(dsn string) Modifier {
	return func(o *options) {
		o.dsn = dsn
	}
}

// WithPath specifies a filesystem path to the SQLite database.
func WithPath(path string) Modifier {
	return func(o *options) {
		o.dsn = path
	}
}

// New opens a SQLite database and ensures the schema is ready.
func New(mods ...Modifier) (*SQLite, error) {
	opts := options{}
	for _, m := range mods {
		m(&opts)
	}
	if opts.dsn == "" {
		return nil, fmt.Errorf("sqlite dsn is required")
	}

	db, err := sql.Open("sqlite", opts.dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &SQLite{db: db}, nil
}

// Open opens a SQLite database at the provided path.
func Open(path string) (*SQLite, error) {
	return New(WithPath(path))
}

// DefaultPath returns the default sqlite database path for an app/namespace.
func DefaultPath(app string, namespaces ...string) (string, error) {
	dir, err := directory.GetConfigDir(app, namespaces...)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.db"), nil
}

// OpenStore opens a config store backed by SQLite, using path when provided.
func OpenStore(path string, app string, namespaces ...string) (config.Store, error) {
	if path == "" {
		defaultPath, err := DefaultPath(app, namespaces...)
		if err != nil {
			return nil, err
		}
		path = defaultPath
	}

	db, err := Open(path)
	if err != nil {
		return nil, err
	}
	return db.Open(app, namespaces...)
}

// Close releases the underlying database resources.
func (s *SQLite) Close() error {
	return s.db.Close()
}

// Open returns a JSON-backed config store scoped to the provided app and namespaces.
func (s *SQLite) Open(app string, namespaces ...string) (config.Store, error) {
	scope := storeScope(app, namespaces...)
	loader := &Loader{db: s.db, scope: scope}
	return &SQLiteStore{loader: loader}, nil
}

type Loader struct {
	db    *sql.DB
	scope string
}

func (l *Loader) List() ([]string, error) {
	rows, err := l.db.Query(`SELECT name FROM configs WHERE scope = ? ORDER BY name`, l.scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return names, nil
}

func (l *Loader) Read(name string) ([]byte, error) {
	var data []byte
	err := l.db.QueryRow(`SELECT data FROM configs WHERE scope = ? AND name = ?`, l.scope, name).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, os.ErrNotExist
	}
	return data, err
}

func (l *Loader) Write(name string, data []byte) error {
	_, err := l.db.Exec(
		`INSERT INTO configs (scope, name, data) VALUES (?, ?, ?)
		 ON CONFLICT(scope, name) DO UPDATE SET data = excluded.data`,
		l.scope, name, data,
	)
	return err
}

func (l *Loader) Delete(name string) error {
	result, err := l.db.Exec(`DELETE FROM configs WHERE scope = ? AND name = ?`, l.scope, name)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return os.ErrNotExist
	}
	return nil
}

type SQLiteStore struct {
	loader *Loader
}

func (s *SQLiteStore) List() ([]config.Descriptor, error) {
	names, err := s.loader.List()
	if err != nil {
		return nil, err
	}
	descs := make([]config.Descriptor, len(names))
	for i, name := range names {
		descs[i] = name
	}
	return descs, nil
}

func (s *SQLiteStore) Marshal(desc config.Descriptor, value interface{}) error {
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

func (s *SQLiteStore) Unmarshal(name string, value interface{}) (config.Descriptor, error) {
	data, err := s.loader.Read(name)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return name, nil
	}
	return name, json.Unmarshal(data, value)
}

func (s *SQLiteStore) Delete(desc config.Descriptor) error {
	name, err := descriptorName(desc)
	if err != nil {
		return err
	}
	return s.loader.Delete(name)
}

// OpenMulti returns a multi-format store on top of the SQLite loader.
func (s *SQLite) OpenMulti(app string, namespaces ...string) (config.Store, error) {
	scope := storeScope(app, namespaces...)
	loader := &Loader{db: s.db, scope: scope}
	return config.NewMulti(loader, marshal.Known...), nil
}

func descriptorName(desc config.Descriptor) (string, error) {
	switch value := desc.(type) {
	case string:
		return value, nil
	default:
		return "", fmt.Errorf("sqlite store expects string descriptor, got %T", desc)
	}
}

func storeScope(app string, namespaces ...string) string {
	parts := append([]string{app}, namespaces...)
	return strings.Join(parts, "/")
}
