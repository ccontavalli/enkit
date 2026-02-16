package blob

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/ccontavalli/enkit/lib/config/marshal"
	"github.com/ccontavalli/enkit/lib/khttp/krequest"
	"github.com/ccontavalli/enkit/lib/khttp/ktest"
	"github.com/ccontavalli/enkit/lib/khttp/protocol"
	"github.com/ccontavalli/enkit/lib/token"
	"github.com/stretchr/testify/assert"
)

type memoryStreamLoader struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMemoryStreamLoader() *memoryStreamLoader {
	return &memoryStreamLoader{data: make(map[string][]byte)}
}

func (m *memoryStreamLoader) List() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.data))
	for name := range m.data {
		names = append(names, name)
	}
	return names, nil
}

func (m *memoryStreamLoader) Reader(name string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.data[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return &testReadSeekerCloser{Reader: bytes.NewReader(data)}, nil
}

func (m *memoryStreamLoader) Writer(name string) (io.WriteCloser, error) {
	return &memoryWriter{loader: m, key: name}, nil
}

func (m *memoryStreamLoader) Delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[name]; !ok {
		return os.ErrNotExist
	}
	delete(m.data, name)
	return nil
}

type memoryWriter struct {
	loader *memoryStreamLoader
	key    string
	buf    bytes.Buffer
}

type testReadSeekerCloser struct {
	*bytes.Reader
}

func (r *testReadSeekerCloser) Close() error {
	return nil
}

func (w *memoryWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func (w *memoryWriter) Close() error {
	w.loader.mu.Lock()
	defer w.loader.mu.Unlock()
	w.loader.data[w.key] = append([]byte(nil), w.buf.Bytes()...)
	return nil
}

func TestServeStoreUploadDownload(t *testing.T) {
	loader := newMemoryStreamLoader()
	mux := http.NewServeMux()

	baseURL, err := ktest.StartURL(mux)
	assert.NoError(t, err, "StartURL")
	if err != nil {
		return
	}

	store, err := NewServeStore(loader, mux.HandleFunc, baseURL, WithPrefix("/blobs/"), WithMetadataStore(InlineMetadata{}))
	assert.NoError(t, err, "NewServeStore")
	if err != nil {
		return
	}

	uploadURL, err := store.UploadURL(Key("item"), WithFilename("item.txt"), WithContentType("text/plain"))
	assert.NoError(t, err, "UploadURL")
	if err != nil {
		return
	}
	assert.NoError(t, doRequest(http.MethodPut, uploadURL, "hello world"), "upload")

	downloadURL, err := store.DownloadURL(Key("item"))
	assert.NoError(t, err, "DownloadURL")
	if err != nil {
		return
	}
	resp := fetch(t, downloadURL.String(), nil)
	assert.Equal(t, http.StatusOK, resp.status, "unexpected status")
	assert.Equal(t, "text/plain", resp.headers.Get("Content-Type"), "unexpected content-type")
	assert.Contains(t, resp.headers.Get("Content-Disposition"), "item.txt", "unexpected content-disposition")
	assert.Equal(t, "hello world", string(resp.body), "unexpected body")

	rangeHeader := http.Header{}
	rangeHeader.Set("Range", "bytes=1-3")
	resp = fetch(t, downloadURL.String(), rangeHeader)
	assert.Equal(t, http.StatusPartialContent, resp.status, "unexpected range status")
	assert.Equal(t, "ell", string(resp.body), "unexpected range body")

	overrideURL, err := store.DownloadURL(Key("item"), WithFilename("override.bin"), WithContentType("application/octet-stream"))
	assert.NoError(t, err, "DownloadURL override")
	if err != nil {
		return
	}
	resp = fetch(t, overrideURL.String(), nil)
	assert.Equal(t, "application/octet-stream", resp.headers.Get("Content-Type"), "unexpected override content-type")
	assert.Contains(t, resp.headers.Get("Content-Disposition"), "override.bin", "unexpected override content-disposition")
}

func TestTokenCodecPathOnlyRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	enc, err := BuildTokenEncoder(rng)
	assert.NoError(t, err)
	if err != nil {
		return
	}
	pathCodec, err := NewTokenPathCodec(nil, WithEncoder(enc))
	assert.NoError(t, err)
	if err != nil {
		return
	}
	codec, err := NewTokenCodec(WithTokenPathCodec(pathCodec))
	assert.NoError(t, err)
	if err != nil {
		return
	}

	params := url.Values{}
	params.Set("filename", "report.pdf")
	params.Set("content-type", "application/pdf")
	params.Set("custom", "value")

	encodedKey, encodedParams, err := codec.Encode("key/with spaces", params)
	assert.NoError(t, err)
	assert.Empty(t, encodedParams)

	decodedKey, decodedParams, err := codec.Decode(encodedKey, encodedParams)
	assert.NoError(t, err)
	assert.Equal(t, "key/with spaces", decodedKey)
	assert.Equal(t, params, decodedParams)
}

func TestTokenCodecPathOnlyKeepsQueryParams(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	enc, err := BuildTokenEncoder(rng)
	assert.NoError(t, err)
	if err != nil {
		return
	}
	pathCodec, err := NewTokenPathCodec(nil, WithEncoder(enc))
	assert.NoError(t, err)
	if err != nil {
		return
	}
	codec, err := NewTokenCodec(WithTokenPathCodec(pathCodec))
	assert.NoError(t, err)
	if err != nil {
		return
	}

	params := url.Values{}
	params.Set("filename", "report.pdf")

	encodedKey, _, err := codec.Encode("key", params)
	assert.NoError(t, err)

	extra := url.Values{"extra": []string{"1"}}
	decodedKey, decodedParams, err := codec.Decode(encodedKey, extra)
	assert.NoError(t, err)
	assert.Equal(t, "key", decodedKey)
	assert.Equal(t, "1", decodedParams.Get("extra"))
}

func TestTokenCodecPathOnlyStableNonce(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	enc, err := BuildTokenEncoder(rng, WithEncoderSetters(token.UseFixedNonce(nil)))
	assert.NoError(t, err)
	if err != nil {
		return
	}
	pathCodec, err := NewTokenPathCodec(nil, WithEncoder(enc))
	assert.NoError(t, err)
	if err != nil {
		return
	}
	codec, err := NewTokenCodec(WithTokenPathCodec(pathCodec))
	assert.NoError(t, err)
	if err != nil {
		return
	}

	params := url.Values{}
	params.Set("x", "1")

	encodedKey1, encodedParams1, err := codec.Encode("key", params)
	assert.NoError(t, err)
	encodedKey2, encodedParams2, err := codec.Encode("key", params)
	assert.NoError(t, err)

	assert.Equal(t, encodedKey1, encodedKey2)
	assert.Equal(t, encodedParams1, encodedParams2)
}

func TestTokenPathPayloadDeterministic(t *testing.T) {
	params := url.Values{}
	params.Set("b", "2")
	params.Add("a", "1")
	params.Add("a", "0")

	payload := tokenPathPayload{
		Key:    "key",
		Params: encodeTokenParams(params),
	}
	raw1, err := marshal.Json.Marshal(payload)
	assert.NoError(t, err)
	raw2, err := marshal.Json.Marshal(payload)
	assert.NoError(t, err)
	assert.Equal(t, raw1, raw2)
}

func TestTokenEncoderFixedNonceDeterministic(t *testing.T) {
	enc, err := BuildTokenEncoder(
		rand.New(rand.NewSource(4)),
		WithEncoderSetters(token.UseFixedNonce(nil)),
	)
	assert.NoError(t, err)
	if err != nil {
		return
	}
	data := []byte("payload")
	encoded1, err := enc.Encode(data)
	assert.NoError(t, err)
	encoded2, err := enc.Encode(data)
	assert.NoError(t, err)
	assert.Equal(t, encoded1, encoded2)
}

func doRequest(method string, u *url.URL, body string) error {
	var status int
	err := protocol.Do(method, u.String(), func(url string, resp *http.Response, err error) error {
		if err != nil {
			return err
		}
		status = resp.StatusCode
		return nil
	}, protocol.WithContent(strings.NewReader(body)))
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", status)
	}
	return nil
}

type fetchResponse struct {
	status  int
	headers http.Header
	body    []byte
}

func fetch(t *testing.T, url string, header http.Header) fetchResponse {
	t.Helper()
	result := fetchResponse{}
	mods := make([]krequest.Modifier, 0, len(header))
	for key, values := range header {
		for _, value := range values {
			mods = append(mods, krequest.AddHeader(key, value))
		}
	}
	err := protocol.Do(http.MethodGet, url, func(url string, resp *http.Response, err error) error {
		if err != nil {
			return err
		}
		result.status = resp.StatusCode
		result.headers = resp.Header.Clone()
		result.body, err = io.ReadAll(resp.Body)
		return err
	}, protocol.WithRequestOptions(mods...))
	assert.NoError(t, err, "do request")
	return result
}
