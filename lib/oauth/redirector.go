package oauth

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/ccontavalli/enkit/lib/khttp"
	"github.com/ccontavalli/enkit/lib/khttp/kcookie"
)

// Redirector is an extractor capable of redirecting to an authentication server for login.
type Redirector struct {
	*Extractor

	// If user does not have authentication cookie, redirect user to this URL to get one.
	AuthURL *url.URL
	// After successful authentication via redirection, send user back here by default.
	DefaultTarget string
}

func (as *Redirector) PerformLogin(w http.ResponseWriter, r *http.Request, lm ...LoginModifier) error {
	options := LoginModifiers(lm).Apply(&LoginOptions{})
	// TODO(carlo): add support for state propagation.
	if options.State != nil {
		return ErrorStateUnsupported
	}

	if as.AuthURL == nil {
		return ErrorCannotAuthenticate
	}

	_, redirected := r.URL.Query()["_redirected"]
	if redirected {
		return ErrorLoops
	}

	authServer := *as.AuthURL
	target := as.DefaultTarget
	if options.Target != "" {
		target = options.Target
	}

	if target != "" {
		authServer.RawQuery = khttp.JoinURLQuery(authServer.RawQuery, "r="+url.QueryEscape(target))
	}
	http.Redirect(w, r, authServer.String(), http.StatusTemporaryRedirect)
	return nil
}

func (as *Redirector) PerformAuth(w http.ResponseWriter, r *http.Request, mods ...kcookie.Modifier) (AuthData, error) {
	creds, cookie, err := as.GetCredentialsFromRequest(r)
	if err != nil {
		return AuthData{}, err
	}

	// This in theory is not possible - added for defence in depth.
	if creds == nil {
		return AuthData{}, fmt.Errorf("invalid nil credentials")
	}

	// TODO(carlo): add support for state propagation.
	return AuthData{Creds: creds, Cookie: cookie}, nil
}

func (as *Redirector) Authenticate(w http.ResponseWriter, r *http.Request, rurl *url.URL) (*CredentialsCookie, error) {
	ad, err := as.PerformAuth(w, r)
	if ad.Complete() && err == nil {
		return ad.Creds, nil
	}

	return nil, as.PerformLogin(w, r, WithTarget(rurl.String()))
}
