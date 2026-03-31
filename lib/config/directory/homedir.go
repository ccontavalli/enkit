// Config loaders to read/write files in directories.
package directory

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"sync"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/kirsle/configdir"
)

type DirectoryStore struct {
	path   string
	mu     sync.Mutex
	closed bool
	root   *os.Root
}

// Returns the absolute path to a specific folder within the
// system default configuration directory for the current user.
//
// On Linux systems, this generally means ~/.config/<app>/<namespace>
func GetConfigDir(app string, namespaces ...string) (string, error) {
	paths := append([]string{app}, namespaces...)
	dir := configdir.LocalConfig(paths...)
	if !filepath.IsAbs(dir) {
		user, err := user.Current()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(user.HomeDir, dir)
	}

	return dir, nil
}

// OpenHomeDir returns a Loader capable of loading and creating config
// files in the system default configuration directory for the current
// user.
//
// On Linux systems, this generally means ~/.config/<app>/<namespace>/.
func OpenHomeDir(app string, namespaces ...string) (*DirectoryStore, error) {
	dir, err := GetConfigDir(app, namespaces...)
	if err != nil {
		return nil, err
	}

	return &DirectoryStore{path: dir}, nil
}

// Refresh values cached by OpenHomeDir.
//
// Internally, OpenHomeDir caches some of the computed paths. Refresh() will cause
// those paths to be re-computed.
//
// Don't bother calling Refresh() unless your project mingles with the HOME
// environment variable, or variables like XDG_CONFIG_HOME.
func Refresh() {
	configdir.Refresh()
}

// OpenDir returns a Loader capable of loading and creating config
// files in the specified directory.
func OpenDir(base string, sub ...string) (*DirectoryStore, error) {
	path := filepath.Join(append([]string{base}, sub...)...)
	return &DirectoryStore{path: path}, nil
}

func (hd *DirectoryStore) List(mods ...config.ListModifier) ([]string, error) {
	opts := &config.ListOptions{}
	if err := config.ListModifiers(mods).Apply(opts); err != nil {
		return nil, err
	}
	root, err := hd.currentRoot(false)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	files, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, err
	}
	paths := []string{}
	for _, file := range files {
		if !file.Type().IsRegular() {
			continue
		}
		paths = append(paths, file.Name())
	}
	sort.Strings(paths)
	return opts.FinalizeKeys(hd, paths, 0)
}

func (hd *DirectoryStore) Delete(name string) error {
	root, err := hd.currentRoot(false)
	if err != nil {
		return err
	}
	return root.Remove(name)
}

func (hd *DirectoryStore) Read(name string) ([]byte, error) {
	root, err := hd.currentRoot(false)
	if err != nil {
		return nil, err
	}
	return root.ReadFile(name)
}

func (hd *DirectoryStore) Write(name string, data []byte) error {
	root, err := hd.currentRoot(true)
	if err != nil {
		return err
	}

	// Don't write the file in place, use rename to guarantee filesystem atomicity.
	tmpName, tmp, err := createTempFile(hd.path, name)
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		root.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		root.Remove(tmpName)
		return err
	}
	if err := root.Rename(tmpName, name); err != nil {
		root.Remove(tmpName)
		return err
	}
	return nil
}

func (hd *DirectoryStore) Close() error {
	hd.mu.Lock()
	root := hd.root
	hd.closed = true
	hd.mu.Unlock()

	if root == nil {
		return nil
	}
	return root.Close()
}

func (hd *DirectoryStore) Reader(name string) (io.ReadCloser, error) {
	root, err := hd.currentRoot(false)
	if err != nil {
		return nil, err
	}
	return root.Open(name)
}

type atomicFileWriter struct {
	root      *os.Root
	file      *os.File
	tmpName   string
	finalName string
}

func (w *atomicFileWriter) Write(p []byte) (int, error) {
	return w.file.Write(p)
}

func (w *atomicFileWriter) Close() error {
	if err := w.file.Close(); err != nil {
		w.root.Remove(w.tmpName)
		return err
	}
	if err := w.root.Rename(w.tmpName, w.finalName); err != nil {
		w.root.Remove(w.tmpName)
		return err
	}
	return nil
}

func (hd *DirectoryStore) Writer(name string) (io.WriteCloser, error) {
	root, err := hd.currentRoot(true)
	if err != nil {
		return nil, err
	}
	tmpName, tmp, err := createTempFile(hd.path, name)
	if err != nil {
		return nil, err
	}
	return &atomicFileWriter{
		root:      root,
		file:      tmp,
		tmpName:   tmpName,
		finalName: name,
	}, nil
}

func (hd *DirectoryStore) currentRoot(create bool) (*os.Root, error) {
	hd.mu.Lock()
	defer hd.mu.Unlock()
	if hd.closed {
		return nil, fs.ErrClosed
	}
	if hd.root != nil {
		return hd.root, nil
	}
	if create {
		if err := os.MkdirAll(hd.path, 0770); err != nil {
			return nil, err
		}
	}
	root, err := os.OpenRoot(hd.path)
	if err != nil {
		return nil, err
	}
	hd.root = root
	return root, nil
}

func createTempFile(dir string, name string) (string, *os.File, error) {
	prefix := filepath.Base(name)
	if prefix == "" || prefix == "." {
		prefix = "config"
	}
	file, err := os.CreateTemp(dir, fmt.Sprintf(".%s.tmp.*", prefix))
	if err != nil {
		return "", nil, err
	}
	return filepath.Base(file.Name()), file, nil
}
