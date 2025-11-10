package oauth

import (
	"net/http"
	"net/url"

	"github.com/ccontavalli/enkit/lib/khttp"
)

// Authenticate parses the request received to authenticate the user.
type Authenticate func(w http.ResponseWriter, r *http.Request, rurl *url.URL) (*CredentialsCookie, error)

func CreateRedirectURL(r *http.Request) *url.URL {
	rurl := khttp.RequestURL(r)
	rurl.RawQuery = khttp.JoinURLQuery(rurl.RawQuery, "_redirected")
	return rurl
}

// authEncoder returns the name of the authentication cookie.
func authEncoder(namespace string) string {
	return namespace + "Auth"
}
