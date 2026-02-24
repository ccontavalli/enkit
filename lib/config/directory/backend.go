package directory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/kflags"
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
			ws.root = flags.Path
		}
		return nil
	}
}

// Modifier configures the directory workspace.
type Modifier func(*Workspace) error

// New returns a directory loader workspace rooted at the provided path.
// If root is empty, the user config directory is used.
func New(root string, mods ...Modifier) *Workspace {
	ws := &Workspace{root: root}
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
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
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
	path := config.NamespacePathFromDescriptor(e.base, desc)
	target, err := e.backend.namespacePath(e.app, path...)
	if err != nil {
		return err
	}
	if err := validateRemoveAllTarget(e.backend.root, target); err != nil {
		return err
	}
	// NOTE: os.RemoveAll returns nil if the path does not exist, but we want
	// os.ErrNotExist for consistency with other backends. This pre-check is racy
	// by nature, but acceptable for namespace deletion semantics.
	if _, err := os.Stat(target); err != nil {
		if os.IsNotExist(err) {
			return os.ErrNotExist
		}
		return err
	}
	return os.RemoveAll(target)
}

func (e *explorator) Close() error { return nil }

func validateRemoveAllTarget(root string, target string) error {
	if target == "" || target == string(filepath.Separator) {
		return fmt.Errorf("refusing to delete empty path")
	}
	if root == "" {
		return nil
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absRoot, absTarget)
	if err != nil {
		return err
	}
	if rel == "." || rel == string(filepath.Separator) || rel == "" {
		return fmt.Errorf("refusing to delete root path %q", absTarget)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to delete path outside root: %q", absTarget)
	}
	return nil
}
