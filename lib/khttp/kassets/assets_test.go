package kassets

import (
	"net/http"
	"testing"

	"github.com/ccontavalli/enkit/lib/khttp"
	"github.com/stretchr/testify/require"
)

func TestPrefixMapperPreservesTrailingSlash(t *testing.T) {
	t.Parallel()

	var got []string
	mapper := PrefixMapper("/user", func(original, name string, handler khttp.FuncHandler) []string {
		got = append(got, name)
		return []string{name}
	})

	mapper("/index.html", "/", func(http.ResponseWriter, *http.Request) {})
	mapper("/dir/index.html", "/dir/", func(http.ResponseWriter, *http.Request) {})

	require.Equal(t, []string{"/user/", "/user/dir/"}, got)
}

func TestPrefixMapperWithBasicMapperIndex(t *testing.T) {
	t.Parallel()

	var got []string
	child := func(original, name string, handler khttp.FuncHandler) []string {
		got = append(got, name)
		return []string{name}
	}
	mapper := PrefixMapper("/user", BasicMapper(child))

	mapper("/index.html", "/index.html", func(http.ResponseWriter, *http.Request) {})

	require.Equal(t, []string{"/user/index", "/user/"}, got)
}
