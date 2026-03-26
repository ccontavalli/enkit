package httpp

import (
	"fmt"
	"github.com/ccontavalli/enkit/lib/khttp"
	"github.com/ccontavalli/enkit/proxy/amux/amuxie"
	"github.com/stretchr/testify/assert"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
)

type staticHandler struct{}

func (*staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {}

func creator(mapping Mapping) (http.Handler, error) {
	return NewProxy(mapping.From.Path, mapping.To, mapping.Transform)
}

func TestBuild(t *testing.T) {
	backends := []*httptest.Server{}
	for ix := 0; ix < 10; ix++ {
		proxyId := ix
		server := httptest.NewServer(&khttp.Dumper{Log: log.Printf, Real: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "GOT %d:%s", proxyId+1, r.URL.String())
		})})
		defer server.Close()

		backends = append(backends, server)
	}

	mapping := []Mapping{{
		From: HostPath{
			Path: "/host/b1",
		},
		To: backends[0].URL + "/backend1",
	}, {
		From: HostPath{
			Path: "/host/b1/b3",
		},
		To: backends[2].URL + "/backend3",
	},
		{
			From: HostPath{
				Path: "/host/b1/b4/",
			},
			To: backends[3].URL + "/backend4/longer/",
		},
	}

	mux := amuxie.New()

	compiled, err := CompileBindings(mapping, nil, creator)
	assert.Nil(t, err)
	assert.Equal(t, []string{}, compiled.Domains)

	InstallBindings(mux, nil, compiled.Bindings)

	proxy := httptest.NewServer(mux)

	get := func(path string) string {
		resp, err := http.Get(proxy.URL + path)
		assert.Nil(t, err)
		body, err := io.ReadAll(resp.Body)
		assert.Nil(t, err)
		return string(body)
	}

	assert.Equal(t, "GOT 1:/backend1", get("/host/b1"))
	assert.Equal(t, "404 page not found\n", get("/host/b1/"))
	assert.Equal(t, "GOT 3:/backend3", get("/host/b1/b3"))
	assert.Equal(t, "GOT 4:/backend4/longer", get("/host/b1/b4"))
	assert.Equal(t, "GOT 4:/backend4/longer/", get("/host/b1/b4/"))
	assert.Equal(t, "GOT 4:/backend4/longer/fuffa", get("/host/b1/b4/fuffa"))
}

func TestCompileBindingsReusesHandlers(t *testing.T) {
	created := 0
	create := func(mapping Mapping) (http.Handler, error) {
		created++
		return &staticHandler{}, nil
	}

	mappings := []Mapping{
		{
			From: HostPath{
				Host: "test.lan",
				Path: "/",
			},
			To: "https://backend-1.example.com",
		},
		{
			From: HostPath{
				Host: "other.lan",
				Path: "/app",
			},
			To: "https://backend-2.example.com",
		},
	}

	first, err := CompileBindings(mappings, nil, create)
	assert.NoError(t, err)
	assert.Equal(t, 2, created)

	second, err := CompileBindings(mappings, first.Handlers, create)
	assert.NoError(t, err)
	assert.Equal(t, 2, created)

	for key, handler := range first.Handlers {
		assert.Same(t, handler, second.Handlers[key])
	}
}

func TestCompileBindingsSharesHandlersAcrossHosts(t *testing.T) {
	created := 0
	create := func(mapping Mapping) (http.Handler, error) {
		created++
		return &staticHandler{}, nil
	}

	mappings := []Mapping{
		{
			From: HostPath{
				Host: "one.lan",
				Path: "/",
			},
			To: "https://backend.example.com",
		},
		{
			From: HostPath{
				Host: "two.lan",
				Path: "/",
			},
			To: "https://backend.example.com",
		},
	}

	compiled, err := CompileBindings(mappings, nil, create)
	assert.NoError(t, err)
	assert.Equal(t, 1, created)
	assert.Len(t, compiled.Handlers, 1)
}

func TestCompileBindingsReusesEquivalentEmptyTransforms(t *testing.T) {
	created := 0
	create := func(mapping Mapping) (http.Handler, error) {
		created++
		return &staticHandler{}, nil
	}

	firstMappings := []Mapping{
		{
			From: HostPath{
				Host: "test.lan",
				Path: "/",
			},
			To: "https://backend.example.com",
		},
	}
	secondMappings := []Mapping{
		{
			From: HostPath{
				Host: "test.lan",
				Path: "/",
			},
			To: "https://backend.example.com",
			Transform: &Transform{
				StripCookie:              []string{},
				MapRequestHeaders:        map[string]string{},
				MapRequestHeadersByGroup: []HeaderGroupMapping{},
			},
		},
	}

	first, err := CompileBindings(firstMappings, nil, create)
	assert.NoError(t, err)
	assert.Equal(t, 1, created)

	second, err := CompileBindings(secondMappings, first.Handlers, create)
	assert.NoError(t, err)
	assert.Equal(t, 1, created)

	for key, handler := range first.Handlers {
		assert.Same(t, handler, second.Handlers[key])
	}
}

func TestCompileBindingsSeparatesNamedHandlers(t *testing.T) {
	created := 0
	create := func(mapping Mapping) (http.Handler, error) {
		created++
		return &staticHandler{}, nil
	}

	mappings := []Mapping{
		{
			Name: "one",
			From: HostPath{
				Host: "one.lan",
				Path: "/",
			},
			To: "https://backend.example.com",
		},
		{
			Name: "two",
			From: HostPath{
				Host: "two.lan",
				Path: "/",
			},
			To: "https://backend.example.com",
		},
	}

	compiled, err := CompileBindings(mappings, nil, create)
	assert.NoError(t, err)
	assert.Equal(t, 2, created)
	assert.Len(t, compiled.Handlers, 2)
}

func TestCompileBindingsRejectsNormalizedHostCollisions(t *testing.T) {
	create := func(mapping Mapping) (http.Handler, error) {
		return &staticHandler{}, nil
	}

	mappings := []Mapping{
		{
			From: HostPath{
				Host: "TEST.LAN",
				Path: "/",
			},
			To: "https://backend-1.example.com",
		},
		{
			From: HostPath{
				Host: "test.lan.",
				Path: "/",
			},
			To: "https://backend-2.example.com",
		},
	}

	compiled, err := CompileBindings(mappings, nil, create)
	assert.Nil(t, compiled)
	assert.Regexp(t, "duplicate route", err)
}
