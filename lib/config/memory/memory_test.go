package memory

import (
	"io"
	"os"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoaderReadWriteDelete(t *testing.T) {
	loader := New()

	keys, err := loader.List()
	assert.NoError(t, err)
	assert.Empty(t, keys)

	err = loader.Write("b", []byte("two"))
	assert.NoError(t, err)
	err = loader.Write("a", []byte("one"))
	assert.NoError(t, err)

	keys, err = loader.List()
	assert.NoError(t, err)
	assert.True(t, sort.StringsAreSorted(keys))
	assert.Equal(t, []string{"a", "b"}, keys)

	data, err := loader.Read("a")
	assert.NoError(t, err)
	assert.Equal(t, []byte("one"), data)

	_, err = loader.Read("missing")
	assert.True(t, os.IsNotExist(err))

	err = loader.Delete("a")
	assert.NoError(t, err)
	err = loader.Delete("missing")
	assert.True(t, os.IsNotExist(err))
	keys, err = loader.List()
	assert.NoError(t, err)
	assert.Equal(t, []string{"b"}, keys)
}

func TestLoaderStreamIO(t *testing.T) {
	loader := New()

	writer, err := loader.Writer("stream")
	assert.NoError(t, err)
	if err != nil {
		return
	}
	_, err = writer.Write([]byte("streamed"))
	assert.NoError(t, err)
	assert.NoError(t, writer.Close())

	reader, err := loader.Reader("stream")
	assert.NoError(t, err)
	if err != nil {
		return
	}
	defer reader.Close()

	payload, err := io.ReadAll(reader)
	assert.NoError(t, err)
	assert.Equal(t, []byte("streamed"), payload)
}
