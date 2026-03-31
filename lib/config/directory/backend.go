package directory

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/kflags"
	"github.com/mitchellh/go-homedir"
)

// Workspace provides Open and Explore over directory-based loaders.
type Workspace struct {
	root string
}

// Flags holds configuration options for directory stores.
type Flags struct {
	// Path specifies a filesystem root for directory stores.
	Path string
}

// DefaultFlags returns a new Flags struct with default values.
func DefaultFlags() *Flags {
	return &Flags{}
}

// Register registers the directory flags with the provided FlagSet.
func (f *Flags) Register(set kflags.FlagSet, prefix string) *Flags {
	set.StringVar(&f.Path, prefix+"config-store-directory-path", f.Path, "Custom path for Directory config backend (optional, defaults to user config dir)")
	return f
}

// FromFlags returns a Modifier that applies directory flags.
func FromFlags(flags *Flags) Modifier {
	return func(ws *Workspace) error {
		if flags == nil {
			return nil
		}
		if flags.Path != "" {
			ws.root = normalizeRoot(flags.Path)
		}
		return nil
	}
}

// Modifier configures the directory workspace.
type Modifier func(*Workspace) error

// New returns a directory loader workspace rooted at the provided path.
// If root is empty, the user config directory is used.
func New(root string, mods ...Modifier) *Workspace {
	ws := &Workspace{root: normalizeRoot(root)}
	for _, mod := range mods {
		if mod != nil {
			_ = mod(ws)
		}
	}
	return ws
}

// Open returns a Loader for the provided namespace.
func (d *Workspace) Open(app string, namespaces ...string) (config.Loader, error) {
	path, err := d.namespacePath(app, namespaces...)
	if err != nil {
		return nil, err
	}
	return OpenDir(path)
}

// Explore returns an explorer that lists child namespaces.
func (d *Workspace) Explore(app string, namespaces ...string) (config.Explorer, error) {
	return &explorator{backend: d, app: app, base: append([]string(nil), namespaces...)}, nil
}

func (d *Workspace) ParsePath(path string) (config.ParsedPath, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return config.ParsedPath{}, fmt.Errorf("config path cannot be empty")
	}
	if d.root == "" {
		return config.ParsedPath{}, fmt.Errorf("filesystem path parsing requires an explicit directory root")
	}

	path, err := homedir.Expand(path)
	if err != nil {
		return config.ParsedPath{}, err
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return config.ParsedPath{}, err
	}

	absRoot, err := filepath.Abs(d.root)
	if err != nil {
		return config.ParsedPath{}, err
	}

	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return config.ParsedPath{}, err
	}
	if rel == "." || rel == "" {
		return config.ParsedPath{}, fmt.Errorf("config path %q must identify a file", path)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return config.ParsedPath{}, fmt.Errorf("config path %q is outside directory root %q", path, absRoot)
	}

	dir := filepath.Dir(rel)
	base := filepath.Base(rel)
	namespaces := []string{}
	if dir != "." {
		namespaces = strings.Split(dir, string(filepath.Separator))
	}

	key := base
	ext := filepath.Ext(base)
	format := strings.TrimPrefix(ext, ".")
	if ext != "" {
		key = strings.TrimSuffix(base, ext)
	}
	key = config.DecodeKey(key)

	return config.ParsedPath{
		StoreRoot: config.StoreRoot{
			AppName:    "/",
			Namespaces: namespaces,
		},
		Descriptor: config.RequestedFormatKey(key, format),
	}, nil
}

func (d *Workspace) Close() error {
	return nil
}

func (d *Workspace) namespacePath(app string, namespaces ...string) (string, error) {
	if d.root == "" {
		return GetConfigDir(app, namespaces...)
	}
	parts := append([]string{d.root, app}, namespaces...)
	return filepath.Join(parts...), nil
}

type explorator struct {
	backend *Workspace
	app     string
	base    []string
}

func (e *explorator) List(mods ...config.ListModifier) ([]config.Descriptor, error) {
	opts := &config.ListOptions{}
	if err := config.ListModifiers(mods).Apply(opts); err != nil {
		return nil, err
	}
	if opts.Unmarshal != nil {
		return nil, fmt.Errorf("namespace list does not support unmarshal")
	}

	path, err := e.backend.namespacePath(e.app, e.base...)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, err
	}
	children := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			children = append(children, entry.Name())
		}
	}
	descs := config.SortedNamespaceDescriptors(e.base, children)
	return opts.Apply(descs, 0), nil
}

func (e *explorator) Delete(desc config.Descriptor) error {
	parent, err := e.backend.namespacePath(e.app, e.base...)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return err
	}
	defer root.Close()
	path := config.NamespacePathFromDescriptor(e.base, desc)
	rel := path[len(e.base):]
	target := filepath.Join(rel...)
	if target == "" || target == "." {
		return fmt.Errorf("refusing to delete empty namespace path")
	}
	if _, err := root.Lstat(target); err != nil {
		if os.IsNotExist(err) {
			return os.ErrNotExist
		}
		return err
	}
	return root.RemoveAll(target)
}

func (e *explorator) Close() error { return nil }

func normalizeRoot(root string) string {
	if root == "" {
		return ""
	}
	return filepath.Clean(root)
}
