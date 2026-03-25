package khttp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReplaceableHandler(t *testing.T) {
	handler := NewReplaceableHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte("first"))
		assert.NoError(t, err)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	assert.Equal(t, "first", resp.Body.String())

	handler.Swap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte("second"))
		assert.NoError(t, err)
	}))

	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	assert.Equal(t, "second", resp.Body.String())
}
