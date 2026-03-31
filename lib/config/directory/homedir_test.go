package directory

import (
	"io"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOpenHomeDir(t *testing.T) {
	home := t.TempDir()
	os.Clearenv()
	os.Setenv("HOME", home)
	Refresh()
	dir, err := OpenHomeDir("app", "identity")
	assert.Nil(t, err)
	assert.True(t, strings.HasPrefix(dir.path, home), "path %s", dir.path)
}

func TestOpenDir(t *testing.T) {
	dir, err := ioutil.TempDir("", "opendir")
	assert.Nil(t, err)

	hd, err := OpenDir(filepath.Join(dir, "test"))
	assert.Nil(t, err)

	confs, err := hd.List()
	assert.Nil(t, err)
	assert.Equal(t, 0, len(confs))

	data, err := hd.Read("test")
	assert.True(t, os.IsNotExist(err))
	assert.Equal(t, 0, len(data))

	err = hd.Delete("test")
	assert.True(t, os.IsNotExist(err))

	quote := []byte("the burden of proof has to be placed on authority, and that it should be dismantled if that burden cannot be met")
	err = hd.Write("test", quote)
	assert.Nil(t, err)

	data, err = hd.Read("test")
	assert.Nil(t, err)
	assert.Equal(t, quote, data)

	confs, err = hd.List()
	assert.Nil(t, err)
	assert.Equal(t, []string{"test"}, confs)

	err = hd.Delete("test")
	assert.Nil(t, err)

	confs, err = hd.List()
	assert.Nil(t, err)
	assert.Equal(t, []string{}, confs)
}

func TestOpenDirDoesNotCreateDirectory(t *testing.T) {
	dir, err := ioutil.TempDir("", "opendir")
	assert.Nil(t, err)

	path := filepath.Join(dir, "missing")
	hd, err := OpenDir(path)
	assert.Nil(t, err)

	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err))

	_, err = hd.Read("test")
	assert.True(t, os.IsNotExist(err))

	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err))
}

func TestOpenDirReadsExistingDirectoryWithoutWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing")
	assert.NoError(t, os.MkdirAll(path, 0770))
	assert.NoError(t, os.WriteFile(filepath.Join(path, "test"), []byte("payload"), 0660))

	hd, err := OpenDir(path)
	assert.NoError(t, err)

	data, err := hd.Read("test")
	assert.NoError(t, err)
	assert.Equal(t, []byte("payload"), data)

	confs, err := hd.List()
	assert.NoError(t, err)
	assert.Equal(t, []string{"test"}, confs)
}

func TestDirectoryStreamIO(t *testing.T) {
	dir, err := ioutil.TempDir("", "stream")
	assert.Nil(t, err)

	hd, err := OpenDir(dir)
	assert.Nil(t, err)

	writer, err := hd.Writer("streamed")
	assert.Nil(t, err)
	if err != nil {
		return
	}

	payload := []byte("streamed payload")
	_, err = writer.Write(payload)
	assert.Nil(t, err)
	assert.Nil(t, writer.Close())

	reader, err := hd.Reader("streamed")
	assert.Nil(t, err)
	if err != nil {
		return
	}
	defer reader.Close()

	readPayload, err := io.ReadAll(reader)
	assert.Nil(t, err)
	assert.Equal(t, payload, readPayload)
}

func TestDirectoryStoreRejectsParentTraversal(t *testing.T) {
	dir, err := ioutil.TempDir("", "opendir")
	assert.Nil(t, err)

	hd, err := OpenDir(dir)
	assert.Nil(t, err)

	err = hd.Write("../escape", []byte("payload"))
	assert.Error(t, err)

	_, err = os.Stat(filepath.Join(filepath.Dir(dir), "escape"))
	assert.True(t, os.IsNotExist(err))
}

func TestDirectoryStoreRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not reliably available on windows test hosts")
	}

	root, err := ioutil.TempDir("", "root")
	assert.Nil(t, err)
	outside, err := ioutil.TempDir("", "outside")
	assert.Nil(t, err)

	err = os.Symlink(outside, filepath.Join(root, "escape"))
	assert.Nil(t, err)

	hd, err := OpenDir(root)
	assert.Nil(t, err)

	err = hd.Write("escape/payload", []byte("payload"))
	assert.Error(t, err)

	_, err = os.Stat(filepath.Join(outside, "payload"))
	assert.True(t, os.IsNotExist(err))
}

func TestDirectoryStoreCloseMakesFutureOperationsFail(t *testing.T) {
	dir := t.TempDir()

	hd, err := OpenDir(dir)
	assert.NoError(t, err)
	assert.NoError(t, hd.Close())

	_, err = hd.Read("test")
	assert.ErrorIs(t, err, fs.ErrClosed)

	err = hd.Write("test", []byte("payload"))
	assert.ErrorIs(t, err, fs.ErrClosed)

	_, err = hd.List()
	assert.ErrorIs(t, err, fs.ErrClosed)
}
