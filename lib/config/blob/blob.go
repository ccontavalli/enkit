package blob

import (
	"io"
	"net/url"

	"github.com/ccontavalli/enkit/lib/config"
)

// Package blob provides a small blob-storage interface with URL-based access.
//
// Typical server-side usage:
//
// 1) Serve blobs from a StreamLoader:
//
//	mux := http.NewServeMux()
//	baseURL, _ := url.Parse("https://example.com")
//	blobStore, _ := blob.NewServeStore(streamLoader, mux.HandleFunc, baseURL, blob.WithPrefix("/blobs/"))
//
// If you need to prevent tampering with keys or download parameters, use
// URL codecs. For example, use TokenCodec with a symmetric token
// encoder and pass it via WithCodec.
//
// StreamLoader uses string keys (same as config.Loader), while Store exposes
// config.Descriptor to match the config.Store API.
type Descriptor = config.Descriptor
type Key = config.Key

// Store is a URL-first interface for large blobs.
type Store interface {
	List() ([]Descriptor, error)
	DownloadURL(desc Descriptor, opts ...TransferOption) (*url.URL, error)
	UploadURL(desc Descriptor, opts ...TransferOption) (*url.URL, error)
	Delete(desc Descriptor) error
}

// StreamLoader provides streaming access to a backend.
type StreamLoader interface {
	List() ([]string, error)
	Reader(name string) (io.ReadCloser, error)
	Writer(name string) (io.WriteCloser, error)
	Delete(name string) error
}

type transferOptions struct {
	Filename    string
	ContentType string
}

// TransferOption configures upload/download URLs.
type TransferOption func(*transferOptions)

type TransferOptions []TransferOption

func (opts TransferOptions) Apply() transferOptions {
	var options transferOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	return options
}

func WithFilename(name string) TransferOption {
	return func(o *transferOptions) {
		o.Filename = name
	}
}

func WithContentType(contentType string) TransferOption {
	return func(o *transferOptions) {
		o.ContentType = contentType
	}
}
