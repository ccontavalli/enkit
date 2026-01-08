package kassets

import (
	"embed"
	"fmt"
	"io/fs"
)

// FSToMap converts an fs.FS to a map of file paths to their contents.
//
// Use this function to convert a go embed variable to a map of strings to bytes,
// very similar to how go_embed_data used to work in Bazel.
//
// Example usage:
//
// In your BUILD.bazel file:
//
// go_library(
//
//	name = "assets",
//	srcs = ["assets.go"],
//	embedsrcs = glob(["*.html"]),
//	importpath = "your/import/path/assets",
//
// )
//
// In your assets.go file:
//
// package assets
//
// import (
//
//	"embed"
//	"github.com/ccontavalli/enkit/lib/khttp/kassets"
//
// )
//
// //go:embed *.html
// var embedded embed.FS
//
//	func Data() map[string][]byte {
//		return kassets.EmbedFSToMapOrPanic(embedded)
//	}
func FSToMap(fsys fs.FS) (map[string][]byte, error) {
	data := make(map[string][]byte)
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		fileData, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		data[path] = fileData
		return nil
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

// FSToMapOrPanic is like FSToMap but panics on error.
func FSToMapOrPanic(fsys fs.FS) map[string][]byte {
	data, err := FSToMap(fsys)
	if err != nil {
		panic(fmt.Sprintf("Parsing embedded file system filed: %v", err))
	}
	return data
}

// EmbedFSToMap converts an embed.FS to a map of file paths to their contents.
func EmbedFSToMap(embedded embed.FS) (map[string][]byte, error) {
	return FSToMap(embedded)
}

// EmbedFSToMapOrPanic is like EmbedFSToMap but panics on error.
func EmbedFSToMapOrPanic(embedded embed.FS) map[string][]byte {
	return FSToMapOrPanic(embedded)
}

// EmbedSubdirToMapOrPanic maps a subdirectory of an embed.FS to a file map.
func EmbedSubdirToMapOrPanic(embedded embed.FS, subdir string) map[string][]byte {
	sub, err := fs.Sub(embedded, subdir)
	if err != nil {
		panic(fmt.Sprintf("failed to access embedded file system %q: %v", subdir, err))
	}
	return FSToMapOrPanic(sub)
}
