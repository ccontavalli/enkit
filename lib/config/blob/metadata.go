package blob

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

const (
	headerSize      = 4096
	headerMagic     = "ENKTBLOB"
	headerVersion   = uint32(1)
	headerFixedSize = 16
)

// Metadata describes blob attributes that affect download response headers.
//
// When using InlineMetadata, metadata is stored in a fixed-size header
// (see headerSize) at the beginning of the blob stream.
//
// Example: disable metadata entirely when callers always set overrides
// in DownloadURL:
//
//	store, _ := blob.NewServeStore(loader, mux.HandleFunc, baseURL, blob.WithPrefix("/blobs/"), blob.WithMetadataStore(blob.NoMetadata{}))
type Metadata struct {
	Filename    string `json:"filename,omitempty"`
	ContentType string `json:"content_type,omitempty"`
}

// MetadataStore controls how blob metadata is stored and retrieved.
//
// Some backends only store raw blob bytes, so metadata (filename, content-type)
// has to be stored separately. A MetadataStore lets ServeStore and StreamLoader
// agree on where that metadata lives:
//
// - Inline metadata is embedded at the front of the blob stream (a fixed header).
// - Sidecar metadata is stored separately (for example as a parallel file or
//   separate database record) while the blob stream contains only payload bytes.
//
// This package currently provides InlineMetadata and NoMetadata. A sidecar
// implementation can be added by composing WrapReader/WrapWriter with an
// external store keyed by the blob name.
//
// WrapWriter should write any needed metadata and return a writer for the blob data.
// WrapReader should read metadata (if any) and return a reader for the blob data.
type MetadataStore interface {
	WrapWriter(key string, w io.WriteCloser, meta interface{}) (io.WriteCloser, error)
	WrapReader(key string, r io.ReadCloser, meta interface{}) (io.ReadCloser, error)
}

// InlineMetadata stores metadata in a fixed-size header at the front of the stream.
type InlineMetadata struct{}

func (InlineMetadata) WrapWriter(_ string, w io.WriteCloser, meta interface{}) (io.WriteCloser, error) {
	header, err := encodeHeader(meta)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(header); err != nil {
		return nil, err
	}
	return w, nil
}

func (InlineMetadata) WrapReader(_ string, r io.ReadCloser, meta interface{}) (io.ReadCloser, error) {
	raw, err := readHeaderRaw(r)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, meta); err != nil {
		return nil, err
	}
	if rs, ok := r.(io.ReadSeeker); ok {
		return &readSeekerCloser{
			ReadSeeker: &offsetReadSeeker{rs: rs, offset: headerSize},
			Closer:     r,
		}, nil
	}
	return r, nil
}

// NoMetadata ignores metadata and passes the stream through unmodified.
type NoMetadata struct{}

func (NoMetadata) WrapWriter(_ string, w io.WriteCloser, _ interface{}) (io.WriteCloser, error) {
	return w, nil
}

func (NoMetadata) WrapReader(_ string, r io.ReadCloser, _ interface{}) (io.ReadCloser, error) {
	return r, nil
}

func encodeHeader(meta interface{}) ([]byte, error) {
	raw, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}
	if len(raw) > headerSize-headerFixedSize {
		return nil, fmt.Errorf("metadata too large: %d bytes", len(raw))
	}
	buf := make([]byte, headerSize)
	copy(buf[:len(headerMagic)], headerMagic)
	binary.BigEndian.PutUint32(buf[len(headerMagic):len(headerMagic)+4], headerVersion)
	binary.BigEndian.PutUint32(buf[len(headerMagic)+4:headerFixedSize], uint32(len(raw)))
	copy(buf[headerFixedSize:], raw)
	return buf, nil
}

type offsetReadSeeker struct {
	rs     io.ReadSeeker
	offset int64
	size   int64
	sized  bool
}

func (o *offsetReadSeeker) Read(p []byte) (int, error) {
	return o.rs.Read(p)
}

func (o *offsetReadSeeker) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		pos, err := o.rs.Seek(o.offset+offset, io.SeekStart)
		return pos - o.offset, err
	case io.SeekCurrent:
		pos, err := o.rs.Seek(offset, io.SeekCurrent)
		return pos - o.offset, err
	case io.SeekEnd:
		size, err := o.dataSize()
		if err != nil {
			return 0, err
		}
		pos, err := o.rs.Seek(o.offset+size+offset, io.SeekStart)
		return pos - o.offset, err
	default:
		return 0, fmt.Errorf("unsupported seek mode: %d", whence)
	}
}

func (o *offsetReadSeeker) dataSize() (int64, error) {
	if o.sized {
		return o.size, nil
	}
	cur, err := o.rs.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	end, err := o.rs.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	if _, err := o.rs.Seek(cur, io.SeekStart); err != nil {
		return 0, err
	}
	o.size = end - o.offset
	o.sized = true
	return o.size, nil
}

type readSeekerCloser struct {
	io.ReadSeeker
	io.Closer
}

func readHeaderRaw(r io.Reader) ([]byte, error) {
	buf := make([]byte, headerSize)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	if len(buf) < headerSize {
		return nil, fmt.Errorf("header too short: %d bytes", len(buf))
	}
	if string(buf[:len(headerMagic)]) != headerMagic {
		return nil, fmt.Errorf("invalid header magic")
	}
	version := binary.BigEndian.Uint32(buf[len(headerMagic) : len(headerMagic)+4])
	if version != headerVersion {
		return nil, fmt.Errorf("unsupported header version: %d", version)
	}
	length := binary.BigEndian.Uint32(buf[len(headerMagic)+4 : headerFixedSize])
	if int(length) > headerSize-headerFixedSize {
		return nil, fmt.Errorf("invalid metadata length: %d", length)
	}
	if length == 0 {
		return nil, nil
	}
	raw := make([]byte, length)
	copy(raw, buf[headerFixedSize:headerFixedSize+length])
	return raw, nil
}
