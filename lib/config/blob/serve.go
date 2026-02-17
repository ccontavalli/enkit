package blob

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ccontavalli/enkit/lib/khttp"
)

// ServeStore publishes upload/download URLs and serves blob content.
//
// Recommended usage:
// - Use a URL codec that encrypts or signs keys and parameters to prevent
//   tampering (for example TokenCodec).
// - Use separate encoders for keys and parameters so each can be rotated
//   independently.
// - If you need deterministic URLs for caching, use a stable nonce, but be
//   aware this is cryptographically weaker because it reveals repeats.
//
// Example: serve blob URLs with inline metadata.
//
//    mux := http.NewServeMux()
//    baseURL, _ := url.Parse("https://example.com")
//    store, _ := blob.NewServeStore(loader, mux.HandleFunc, baseURL,
//        blob.WithPrefix("/blobs/"),
//        blob.WithMetadataStore(blob.InlineMetadata{}))
//
//    // In a handler:
//    url, _ := store.DownloadURL(blob.Key("report.pdf"),
//        blob.WithFilename("report.pdf"),
//        blob.WithContentType("application/pdf"))
//    // embed url.String() in HTML or JSON response

const (
	defaultPrefix    = "/blobs/"
	queryFilename    = "filename"
	queryContentType = "content-type"
)

type ServeStore struct {
	loader         StreamLoader
	baseURL        *url.URL
	prefix         string
	downloadPrefix string
	uploadPrefix   string
	metadata       MetadataStore
	codec          URLCodec
}

type ServeStoreOption func(*serveStoreOptions)

type serveStoreOptions struct {
	prefix   string
	metadata MetadataStore
	codec    URLCodec
}

func WithPrefix(prefix string) ServeStoreOption {
	return func(o *serveStoreOptions) {
		o.prefix = prefix
	}
}

func WithMetadataStore(meta MetadataStore) ServeStoreOption {
	return func(o *serveStoreOptions) {
		o.metadata = meta
	}
}

func WithCodec(codec URLCodec) ServeStoreOption {
	return func(o *serveStoreOptions) {
		o.codec = codec
	}
}

func NewServeStore(loader StreamLoader, register khttp.RegisterFunc, baseURL *url.URL, mods ...ServeStoreOption) (*ServeStore, error) {
	if loader == nil {
		return nil, fmt.Errorf("stream loader is required")
	}
	if register == nil {
		return nil, fmt.Errorf("register function is required")
	}
	if baseURL == nil {
		return nil, fmt.Errorf("base URL is required")
	}
	opts := serveStoreOptions{
		prefix:   defaultPrefix,
		metadata: InlineMetadata{},
		codec:    PlainCodec{},
	}
	for _, mod := range mods {
		if mod != nil {
			mod(&opts)
		}
	}
	if opts.metadata == nil {
		opts.metadata = InlineMetadata{}
	}
	if opts.codec == nil {
		opts.codec = PlainCodec{}
	}
	normalized := normalizePrefix(opts.prefix)
	store := &ServeStore{
		loader:         loader,
		baseURL:        baseURL,
		prefix:         normalized,
		downloadPrefix: khttp.JoinPreserve(normalized, "download/"),
		uploadPrefix:   khttp.JoinPreserve(normalized, "upload/"),
		metadata:       opts.metadata,
		codec:          opts.codec,
	}
	register(store.downloadPrefix, store.handleDownload)
	register(store.uploadPrefix, store.handleUpload)
	return store, nil
}

func normalizePrefix(prefix string) string {
	normalized := strings.TrimSpace(prefix)
	if normalized == "" {
		normalized = defaultPrefix
	}
	if !strings.HasPrefix(normalized, "/") {
		normalized = "/" + normalized
	}
	if !strings.HasSuffix(normalized, "/") {
		normalized += "/"
	}
	return khttp.CleanPreserve(normalized)
}

func (s *ServeStore) List() ([]Descriptor, error) {
	names, err := s.loader.List()
	if err != nil {
		return nil, err
	}
	descs := make([]Descriptor, len(names))
	for i, name := range names {
		descs[i] = Key(name)
	}
	return descs, nil
}

func (s *ServeStore) DownloadURL(desc Descriptor, opts ...TransferOption) (*url.URL, error) {
	key := desc.Key()
	params := encodeTransferOptions(TransferOptions(opts).Apply())
	encoded, encodedParams, err := s.codec.Encode(key, params)
	if err != nil {
		return nil, err
	}
	path := khttp.JoinPreserve(s.downloadPrefix, url.PathEscape(encoded))
	return s.buildURL(path, encodedParams), nil
}

func (s *ServeStore) UploadURL(desc Descriptor, opts ...TransferOption) (*url.URL, error) {
	key := desc.Key()
	params := encodeTransferOptions(TransferOptions(opts).Apply())
	encoded, encodedParams, err := s.codec.Encode(key, params)
	if err != nil {
		return nil, err
	}
	path := khttp.JoinPreserve(s.uploadPrefix, url.PathEscape(encoded))
	return s.buildURL(path, encodedParams), nil
}

func (s *ServeStore) Delete(desc Descriptor) error {
	return s.loader.Delete(desc.Key())
}

func (s *ServeStore) buildURL(path string, params url.Values) *url.URL {
	u := *s.baseURL
	u.Path = khttp.JoinPreserve(u.Path, path)
	if len(params) > 0 {
		u.RawQuery = params.Encode()
	}
	return &u
}

func (s *ServeStore) handleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	encodedKey, err := parseKey(r.URL.Path, s.downloadPrefix)
	if err != nil {
		http.Error(w, "invalid key", http.StatusBadRequest)
		return
	}

	key, params, err := s.codec.Decode(encodedKey, r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	overrides := decodeTransferOptions(params)

	reader, err := s.loader.Reader(key)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer reader.Close()

	meta := Metadata{}
	data, err := s.metadata.WrapReader(key, reader, &meta)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if overrides.Filename != "" {
		meta.Filename = overrides.Filename
	}
	if overrides.ContentType != "" {
		meta.ContentType = overrides.ContentType
	}

	if meta.ContentType != "" {
		w.Header().Set("Content-Type", meta.ContentType)
	}
	if meta.Filename != "" {
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{
			"filename": meta.Filename,
		}))
	}

	if rs, ok := data.(io.ReadSeeker); ok {
		http.ServeContent(w, r, "", time.Time{}, rs)
		return
	}

	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	if _, err := io.Copy(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *ServeStore) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	encodedKey, err := parseKey(r.URL.Path, s.uploadPrefix)
	if err != nil {
		http.Error(w, "invalid key", http.StatusBadRequest)
		return
	}

	key, params, err := s.codec.Decode(encodedKey, r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	queryOpts := decodeTransferOptions(params)

	writer, err := s.loader.Writer(key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer writer.Close()

	meta := Metadata{}
	meta.Filename = queryOpts.Filename
	meta.ContentType = queryOpts.ContentType
	if meta.ContentType == "" && r.Header.Get("Content-Type") != "" {
		meta.ContentType = r.Header.Get("Content-Type")
	}
	if meta.Filename == "" {
		if disp := r.Header.Get("Content-Disposition"); disp != "" {
			_, params, err := mime.ParseMediaType(disp)
			if err == nil {
				meta.Filename = params["filename"]
			}
		}
	}

	writer, err = s.metadata.WrapWriter(key, writer, meta)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if _, err := io.Copy(writer, r.Body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func parseKey(path, prefix string) (string, error) {
	cleaned := khttp.CleanPreserve(path)
	if !strings.HasPrefix(cleaned, prefix) {
		return "", fmt.Errorf("path %q does not match prefix %q", cleaned, prefix)
	}
	keyEscaped := strings.TrimPrefix(cleaned, prefix)
	if keyEscaped == "" {
		return "", fmt.Errorf("empty key")
	}
	encoded, err := url.PathUnescape(keyEscaped)
	if err != nil {
		return "", err
	}
	return encoded, nil
}
