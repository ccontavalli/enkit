package s3

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCleanPrefix(t *testing.T) {
	assert.Equal(t, "", cleanPrefix(""))
	assert.Equal(t, "foo/", cleanPrefix("foo"))
	assert.Equal(t, "foo/", cleanPrefix("foo/"))
}

func TestObjectKey(t *testing.T) {
	s := &Store{prefix: "base/"}
	assert.Equal(t, "base/key", s.objectKey("key"))
	assert.Equal(t, "base/path/key", s.objectKey("path/key"))
}

func TestContentDisposition(t *testing.T) {
	assert.Equal(t, "attachment; filename=\"report.pdf\"", contentDisposition("report.pdf"))
	assert.Equal(t, "attachment; filename=\"badname\"", contentDisposition("bad\"name"))
}

func TestFromFlags(t *testing.T) {
	flags := &Flags{
		Bucket:    "bucket",
		Region:    "us-east-1",
		Prefix:    "prefix/",
		Endpoint:  "https://s3.example.com",
		PathStyle: true,
	}
	opts := options{}
	err := FromFlags(flags)(&opts)
	assert.NoError(t, err)
	assert.Equal(t, "bucket", opts.bucket)
	assert.Equal(t, "us-east-1", opts.region)
	assert.Equal(t, "prefix/", opts.prefix)
	assert.Equal(t, "https://s3.example.com", opts.endpoint)
	assert.True(t, opts.pathStyle)
}
