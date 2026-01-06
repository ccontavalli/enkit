// Config store backed by SQLite.
//
// SQLite uses JSON encoding for values and is optimized for programmatic access.
// Use SQLiteMulti when you need multi-format compatibility (JSON/TOML/YAML/Gob),
// for example when configs must be edited by external tools.
//
// Tuning knobs:
// - WithJournalMode, WithSynchronous, WithBusyTimeout control SQLite pragmas.
// - WithMaxOpenConns, WithMaxIdleConns configure connection pool limits.
//
// Defaults: journal_mode=WAL, synchronous=NORMAL, busy_timeout=5000ms.
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
	"github.com/ccontavalli/enkit/lib/kflags"
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

// SQLiteMulti provides multi-format stores on top of SQLite for interoperability.
type SQLiteMulti struct {
	db *sql.DB
}

type options struct {
	dsn string

	journalMode  string
	synchronous  string
	busyTimeout  int
	maxOpenConns int
	maxIdleConns int
	cacheSize    int
	mmapSize     int
	tempStore    string
}

type Modifier func(*options) error

// Flags holds configuration options for SQLite stores.
type Flags struct {
	// Path specifies a filesystem path to the SQLite database.
	// If empty, DefaultPath is used.
	Path string
	// JournalMode configures PRAGMA journal_mode (for example, WAL).
	JournalMode string
	// Synchronous configures PRAGMA synchronous (for example, NORMAL).
	Synchronous string
	// BusyTimeoutMs configures PRAGMA busy_timeout in milliseconds.
	BusyTimeoutMs int
	// MaxOpenConns configures the database/sql connection pool limit.
	MaxOpenConns int
	// MaxIdleConns configures the database/sql idle connection pool size.
	MaxIdleConns int
	// CacheSize configures PRAGMA cache_size (pages; negative means KiB).
	CacheSize int
	// MmapSize configures PRAGMA mmap_size in bytes.
	MmapSize int
	// TempStore configures PRAGMA temp_store (DEFAULT, FILE, MEMORY).
	TempStore string
}

// DefaultFlags returns a new Flags struct with default values.
func DefaultFlags() *Flags {
	return &Flags{
		JournalMode:   "WAL",
		Synchronous:   "NORMAL",
		BusyTimeoutMs: 5000,
		MaxOpenConns:  8,
		MaxIdleConns:  8,
		CacheSize:     -2000,
		MmapSize:      64 * 1024 * 1024,
		TempStore:     "MEMORY",
	}
}

// Register registers the sqlite flags with the provided FlagSet.
func (f *Flags) Register(set kflags.FlagSet, prefix string) *Flags {
	set.StringVar(&f.Path, prefix+"config-store-sqlite-path", f.Path, "Custom path for SQLite config backend (optional, defaults to user config dir)")
	set.StringVar(&f.JournalMode, prefix+"config-store-sqlite-journal-mode", f.JournalMode, "SQLite journal mode (for example, WAL)")
	set.StringVar(&f.Synchronous, prefix+"config-store-sqlite-synchronous", f.Synchronous, "SQLite synchronous mode (for example, NORMAL)")
	set.IntVar(&f.BusyTimeoutMs, prefix+"config-store-sqlite-busy-timeout-ms", f.BusyTimeoutMs, "SQLite busy timeout in milliseconds")
	set.IntVar(&f.MaxOpenConns, prefix+"config-store-sqlite-max-open-conns", f.MaxOpenConns, "SQLite max open connections (database/sql)")
	set.IntVar(&f.MaxIdleConns, prefix+"config-store-sqlite-max-idle-conns", f.MaxIdleConns, "SQLite max idle connections (database/sql)")
	set.IntVar(&f.CacheSize, prefix+"config-store-sqlite-cache-size", f.CacheSize, "SQLite cache_size pragma (pages; negative means KiB)")
	set.IntVar(&f.MmapSize, prefix+"config-store-sqlite-mmap-size", f.MmapSize, "SQLite mmap_size pragma in bytes")
	set.StringVar(&f.TempStore, prefix+"config-store-sqlite-temp-store", f.TempStore, "SQLite temp_store pragma (DEFAULT, FILE, MEMORY)")
	return f
}

// FromFlags returns a Modifier that applies SQLite flags.
func FromFlags(flags *Flags, app string, namespaces ...string) Modifier {
	return func(o *options) error {
		if flags == nil {
			return nil
		}
		if flags.Path != "" {
			o.dsn = flags.Path
			return nil
		}
		if o.dsn == "" {
			path, err := DefaultPath(app, namespaces...)
			if err != nil {
				return err
			}
			o.dsn = path
		}
		o.journalMode = flags.JournalMode
		o.synchronous = flags.Synchronous
		o.busyTimeout = flags.BusyTimeoutMs
		o.maxOpenConns = flags.MaxOpenConns
		o.maxIdleConns = flags.MaxIdleConns
		o.cacheSize = flags.CacheSize
		o.mmapSize = flags.MmapSize
		o.tempStore = flags.TempStore
		return nil
	}
}

// WithDSN specifies the SQLite data source name.
func WithDSN(dsn string) Modifier {
	return func(o *options) error {
		o.dsn = dsn
		return nil
	}
}

// WithPath specifies a filesystem path to the SQLite database.
func WithPath(path string) Modifier {
	return func(o *options) error {
		o.dsn = path
		return nil
	}
}

// WithJournalMode sets the SQLite journal_mode pragma (for example, WAL).
func WithJournalMode(mode string) Modifier {
	return func(o *options) error {
		o.journalMode = mode
		return nil
	}
}

// WithSynchronous sets the SQLite synchronous pragma (for example, NORMAL).
func WithSynchronous(mode string) Modifier {
	return func(o *options) error {
		o.synchronous = mode
		return nil
	}
}

// WithBusyTimeout sets the SQLite busy_timeout pragma in milliseconds.
func WithBusyTimeout(timeoutMs int) Modifier {
	return func(o *options) error {
		o.busyTimeout = timeoutMs
		return nil
	}
}

// WithMaxOpenConns configures the database/sql connection pool limit.
func WithMaxOpenConns(count int) Modifier {
	return func(o *options) error {
		o.maxOpenConns = count
		return nil
	}
}

// WithMaxIdleConns configures the database/sql idle connection pool size.
func WithMaxIdleConns(count int) Modifier {
	return func(o *options) error {
		o.maxIdleConns = count
		return nil
	}
}

// WithCacheSize sets the SQLite cache_size pragma (pages; negative means KiB).
func WithCacheSize(size int) Modifier {
	return func(o *options) error {
		o.cacheSize = size
		return nil
	}
}

// WithMmapSize sets the SQLite mmap_size pragma in bytes.
func WithMmapSize(size int) Modifier {
	return func(o *options) error {
		o.mmapSize = size
		return nil
	}
}

// WithTempStore sets the SQLite temp_store pragma (DEFAULT, FILE, MEMORY).
func WithTempStore(mode string) Modifier {
	return func(o *options) error {
		o.tempStore = mode
		return nil
	}
}

// New opens a SQLite database and ensures the schema is ready.
func New(mods ...Modifier) (*SQLite, error) {
	db, err := openDB(mods...)
	if err != nil {
		return nil, err
	}
	return &SQLite{db: db}, nil
}

// NewMulti opens a SQLite database for a multi-format store.
func NewMulti(mods ...Modifier) (*SQLiteMulti, error) {
	db, err := openDB(mods...)
	if err != nil {
		return nil, err
	}
	return &SQLiteMulti{db: db}, nil
}

// DefaultPath returns the default sqlite database path for an app/namespace.
func DefaultPath(app string, namespaces ...string) (string, error) {
	dir, err := directory.GetConfigDir(app, namespaces...)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.db"), nil
}

// Close releases the underlying database resources.
func (s *SQLite) Close() error {
	return s.db.Close()
}

// Open returns a JSON-backed config store scoped to the provided app and namespaces.
func (s *SQLite) Open(app string, namespaces ...string) (config.Store, error) {
	scope := storeScope(app, namespaces...)
	loader, err := newLoader(s.db, scope)
	if err != nil {
		return nil, err
	}
	return &SQLiteStore{loader: loader}, nil
}

type Loader struct {
	db    *sql.DB
	scope string

	listStmt   *sql.Stmt
	readStmt   *sql.Stmt
	writeStmt  *sql.Stmt
	deleteStmt *sql.Stmt
}

func (l *Loader) List() ([]string, error) {
	rows, err := l.listStmt.Query(l.scope)
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
	err := l.readStmt.QueryRow(l.scope, name).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, os.ErrNotExist
	}
	return data, err
}

func (l *Loader) Write(name string, data []byte) error {
	_, err := l.writeStmt.Exec(l.scope, name, data)
	return err
}

func (l *Loader) Delete(name string) error {
	result, err := l.deleteStmt.Exec(l.scope, name)
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
// Close releases the underlying database resources.
func (s *SQLiteMulti) Close() error {
	return s.db.Close()
}

// Open returns a multi-format config store scoped to the provided app and namespaces.
func (s *SQLiteMulti) Open(app string, namespaces ...string) (config.Store, error) {
	scope := storeScope(app, namespaces...)
	loader, err := newLoader(s.db, scope)
	if err != nil {
		return nil, err
	}
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

func openDB(mods ...Modifier) (*sql.DB, error) {
	opts := options{
		journalMode:  "WAL",
		synchronous:  "NORMAL",
		busyTimeout:  5000,
		maxOpenConns: 8,
		maxIdleConns: 8,
		cacheSize:    -2000,
		mmapSize:     64 * 1024 * 1024,
		tempStore:    "MEMORY",
	}
	for _, m := range mods {
		if err := m(&opts); err != nil {
			return nil, err
		}
	}
	if opts.dsn == "" {
		return nil, fmt.Errorf("sqlite dsn is required")
	}

	db, err := sql.Open("sqlite", opts.dsn)
	if err != nil {
		return nil, err
	}
	if opts.maxOpenConns > 0 {
		db.SetMaxOpenConns(opts.maxOpenConns)
	}
	if opts.maxIdleConns > 0 {
		db.SetMaxIdleConns(opts.maxIdleConns)
	}
	if opts.journalMode != "" {
		if _, err := db.Exec("PRAGMA journal_mode=" + opts.journalMode + ";"); err != nil {
			db.Close()
			return nil, err
		}
	}
	if opts.synchronous != "" {
		if _, err := db.Exec("PRAGMA synchronous=" + opts.synchronous + ";"); err != nil {
			db.Close()
			return nil, err
		}
	}
	if opts.busyTimeout > 0 {
		if _, err := db.Exec(fmt.Sprintf("PRAGMA busy_timeout=%d;", opts.busyTimeout)); err != nil {
			db.Close()
			return nil, err
		}
	}
	if opts.cacheSize != 0 {
		if _, err := db.Exec(fmt.Sprintf("PRAGMA cache_size=%d;", opts.cacheSize)); err != nil {
			db.Close()
			return nil, err
		}
	}
	if opts.mmapSize > 0 {
		if _, err := db.Exec(fmt.Sprintf("PRAGMA mmap_size=%d;", opts.mmapSize)); err != nil {
			db.Close()
			return nil, err
		}
	}
	if opts.tempStore != "" {
		if _, err := db.Exec("PRAGMA temp_store=" + opts.tempStore + ";"); err != nil {
			db.Close()
			return nil, err
		}
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func newLoader(db *sql.DB, scope string) (*Loader, error) {
	listStmt, err := db.Prepare(`SELECT name FROM configs WHERE scope = ? ORDER BY name`)
	if err != nil {
		return nil, err
	}

	readStmt, err := db.Prepare(`SELECT data FROM configs WHERE scope = ? AND name = ?`)
	if err != nil {
		_ = listStmt.Close()
		return nil, err
	}

	writeStmt, err := db.Prepare(
		`INSERT INTO configs (scope, name, data) VALUES (?, ?, ?)
		 ON CONFLICT(scope, name) DO UPDATE SET data = excluded.data`,
	)
	if err != nil {
		_ = listStmt.Close()
		_ = readStmt.Close()
		return nil, err
	}

	deleteStmt, err := db.Prepare(`DELETE FROM configs WHERE scope = ? AND name = ?`)
	if err != nil {
		_ = listStmt.Close()
		_ = readStmt.Close()
		_ = writeStmt.Close()
		return nil, err
	}

	return &Loader{
		db:         db,
		scope:      scope,
		listStmt:   listStmt,
		readStmt:   readStmt,
		writeStmt:  writeStmt,
		deleteStmt: deleteStmt,
	}, nil
}

func storeScope(app string, namespaces ...string) string {
	parts := append([]string{app}, namespaces...)
	return strings.Join(parts, "/")
}
