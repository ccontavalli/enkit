package kassets

import (
	"embed"
	"fmt"
	"io/fs"
)

// EmbedFSToMap converts an embed.FS to a map of file paths to their contents.
//
// Use this function to convert a go embed variable to a map of strings to bytes,
// very similar to how go_embed_data used to work in Bazel.
//
// Example usage:
//
// In your BUILD.bazel file:
//
// go_library(
//     name = "assets",
//     srcs = ["assets.go"],
//     embedsrcs = glob(["*.html"]),
//     importpath = "your/import/path/assets",
// )
//
// In your assets.go file:
//
// package assets
//
// import (
// 	"embed"
// 	"github.com/enfabrica/enkit/lib/khttp/kassets"
// )
//
// //go:embed *.html
// var embedded embed.FS
//
// func Data() map[string][]byte {
// 	return kassets.EmbedFSToMapOrPanic(embedded)
// }
func EmbedFSToMap(embedded embed.FS) (map[string][]byte, error) {
	data := make(map[string][]byte)
	err := fs.WalkDir(embedded, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		fileData, err := embedded.ReadFile(path)
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

// EmbedFSToMapOrPanic is like EmbedFSToMap but panics on error.
func EmbedFSToMapOrPanic(embedded embed.FS) map[string][]byte {
	data, err := EmbedFSToMap(embedded)
	if err != nil {
		panic(fmt.Sprintf("Parsing embedded file system filed: %v", err))
	}
	return data
}
