package kassets

import (
	"embed"
	"testing"

	"github.com/stretchr/testify/assert"
)

//go:embed testdata/*
var testFS embed.FS

func TestEmbedFSToMap(t *testing.T) {
	data, err := EmbedFSToMap(testFS)
	assert.NoError(t, err)
	assert.NotNil(t, data)

	content, ok := data["testdata/file1.txt"]
	assert.True(t, ok)
	assert.Equal(t, "hello world\n", string(content))

	content, ok = data["testdata/file2.txt"]
	assert.True(t, ok)
	assert.Equal(t, "another file\n", string(content))
}

func TestEmbedFSToMapOrPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		data := EmbedFSToMapOrPanic(testFS)
		assert.NotNil(t, data)

		content, ok := data["testdata/file1.txt"]
		assert.True(t, ok)
		assert.Equal(t, "hello world\n", string(content))
	})
}

func TestFSToMap(t *testing.T) {
	data, err := FSToMap(testFS)
	assert.NoError(t, err)
	assert.NotNil(t, data)

	content, ok := data["testdata/file1.txt"]
	assert.True(t, ok)
	assert.Equal(t, "hello world\n", string(content))
}

func TestEmbedSubdirToMapOrPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		data := EmbedSubdirToMapOrPanic(testFS, "testdata")
		assert.NotNil(t, data)

		content, ok := data["file2.txt"]
		assert.True(t, ok)
		assert.Equal(t, "another file\n", string(content))
	})
}
