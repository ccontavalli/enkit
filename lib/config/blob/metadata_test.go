package blob

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHeaderRoundTrip(t *testing.T) {
	meta := Metadata{
		Filename:    "file.txt",
		ContentType: "text/plain",
	}
	buf, err := encodeHeader(meta)
	assert.NoError(t, err, "encodeHeader error")
	if err != nil {
		return
	}
	decodedRaw, err := readHeaderRaw(bytes.NewReader(buf))
	assert.NoError(t, err, "decodeHeader error")
	if err != nil {
		return
	}
	var decoded Metadata
	err = json.Unmarshal(decodedRaw, &decoded)
	assert.NoError(t, err, "unmarshal meta")
	if err != nil {
		return
	}
	assert.Equal(t, meta, decoded, "metadata mismatch")
}

func TestHeaderTooLarge(t *testing.T) {
	meta := Metadata{
		Filename: strings.Repeat("a", headerSize),
	}
	_, err := encodeHeader(meta)
	assert.Error(t, err, "expected encodeHeader to fail for oversized metadata")
}
