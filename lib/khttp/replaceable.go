package khttp

import (
	"net/http"
	"sync/atomic"
)

// ReplaceableHandler is an http.Handler whose target can be swapped atomically at run time.
// This is useful to implement config reload of http handlers, or dynamic handlers.
type ReplaceableHandler struct {
	current atomic.Pointer[http.Handler]
}

// NewReplaceableHandler returns an http.Handler whose implementation can be swapped atomically.
func NewReplaceableHandler(handler http.Handler) *ReplaceableHandler {
	rh := &ReplaceableHandler{}
	rh.Swap(handler)
	return rh
}

// Swap atomically replaces the wrapped handler for future requests.
func (rh *ReplaceableHandler) Swap(handler http.Handler) {
	rh.current.Store(&handler)
}

// ServeHTTP forwards the request to the currently active handler.
func (rh *ReplaceableHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	(*rh.current.Load()).ServeHTTP(w, r)
}
