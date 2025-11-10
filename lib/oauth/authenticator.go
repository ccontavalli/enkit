package oauth

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"net/http"

	"github.com/ccontavalli/enkit/lib/khttp/kcookie"
	"github.com/ccontavalli/enkit/lib/logger"
	"github.com/ccontavalli/enkit/lib/oauth/cookie"
	"github.com/ccontavalli/enkit/lib/token"
	"golang.org/x/oauth2"
)

type Authenticator struct {
	Extractor

	rng         *rand.Rand
	log         logger.Logger
	authEncoder *token.TypeEncoder

	conf *oauth2.Config

	verifiers []Verifier
}

// LoginURL computes the URL the user is redirected to to perform login.
//
// After the user authenticates, it is redirected back to URL set as auth handler,
// which verifies the credentials, and creates the authentication cookie.
//
// At this point, either the auth handler returns a page directly (for example, when
// you set up your own handler with MakeAuthHandler), or, if a target parameter is
// set, the user is redirected to the configured target.
//
// State is not used by the auth handler. You can basically pass anything you like
// and have it forwarded to you at the end of the authentication.
//
// Returns: the url to use, a secure token, and nil or an error, in order.
func (a *Authenticator) LoginURL(target string, state interface{}) (string, []byte, error) {
	secret := make([]byte, 16)
	_, err := a.rng.Read(secret)
	if err != nil {
		return "", nil, err
	}
	// This is not necessary. We could just pass the secret to the AuthCodeURL function.
	// But it needs to be escaped. AuthoCookie.Encode will sign it, as well as Encode it. Cannot hurt.
	esecret, err := a.authEncoder.Encode(LoginState{Secret: secret, Target: target, State: state})
	if err != nil {
		return "", nil, err
	}
	url := a.conf.AuthCodeURL(string(esecret))
	///* oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "login"), oauth2.SetAuthURLParam("approval_prompt", "force"), oauth2.SetAuthURLParam("max_age", "0") */)
	return url, secret, nil
}

// PerformLogin writes the response to the request to actually perform the login.
func (a *Authenticator) PerformLogin(w http.ResponseWriter, r *http.Request, lm ...LoginModifier) error {
	options := LoginModifiers(lm).Apply(&LoginOptions{})
	url, secret, err := a.LoginURL(options.Target, options.State)
	if err != nil {
		return err
	}

	authcookie, err := a.authEncoder.Encode(secret)
	if err != nil {
		return err
	}

	http.SetCookie(w, options.CookieOptions.Apply(&http.Cookie{
		Name:     authEncoder(a.baseCookie),
		Value:    string(authcookie),
		HttpOnly: true,
	}))

	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
	return nil
}

func (a *Authenticator) ExtractAuth(w http.ResponseWriter, r *http.Request) (AuthData, error) {
	cookie, err := r.Cookie(authEncoder(a.baseCookie))
	if err != nil || cookie == nil {
		return AuthData{}, ErrorNotAuthenticated
	}

	var secretExpected []byte
	if _, err := a.authEncoder.Decode(context.Background(), []byte(cookie.Value), &secretExpected); err != nil {
		return AuthData{}, fmt.Errorf("Cookie decoding failed - %w", err)
	}

	query := r.URL.Query()
	state := query.Get("state")
	var received LoginState
	if _, err := a.authEncoder.Decode(context.Background(), []byte(state), &received); err != nil {
		return AuthData{}, fmt.Errorf("State decoding failed - %w", err)
	}

	if !bytes.Equal(secretExpected, received.Secret) {
		return AuthData{}, fmt.Errorf("Secret did not match")
	}

	http.SetCookie(w, &http.Cookie{
		Name:   authEncoder(a.baseCookie),
		MaxAge: -1,
	})
	code := query.Get("code")
	tok, err := a.conf.Exchange(oauth2.NoContext, code)
	if err != nil {
		return AuthData{}, fmt.Errorf("Could not retrieve token - %w", err)
	}
	if !tok.Valid() {
		return AuthData{}, fmt.Errorf("Invalid token retrieved")
	}

	identity := &Identity{}
	for _, verifier := range a.verifiers {
		identity, err = verifier.Verify(a.log, identity, tok)
		if err != nil {
			return AuthData{}, fmt.Errorf("Invalid token - %w", err)
		}
	}
	if identity.Id == "" || identity.Username == "" {
		return AuthData{}, fmt.Errorf("Authentication process succeeded with no credentials")
	}

	creds := CredentialsCookie{Identity: *identity, Token: *tok}
	return AuthData{Creds: &creds, Target: received.Target, State: received.State}, nil
}

func (a *Authenticator) SetAuthCookie(ad AuthData, w http.ResponseWriter, co ...kcookie.Modifier) (AuthData, error) {
	ccookie, err := a.EncodeCredentials(*ad.Creds)
	if err != nil {
		return AuthData{}, err
	}
	http.SetCookie(w, a.CredentialsCookie(ccookie, co...))
	return AuthData{Creds: ad.Creds, Cookie: ccookie, Target: ad.Target, State: ad.State}, nil
}

// CredentialsCookie will create an http.Cookie object containing the user credentials.
func (a *Authenticator) CredentialsCookie(value string, co ...kcookie.Modifier) *http.Cookie {
	return cookie.CredentialsCookie(a.baseCookie, value, co...)
}

// PerformAuth implements the logic to handle an oauth request from an oauth provider.
func (a *Authenticator) PerformAuth(w http.ResponseWriter, r *http.Request, co ...kcookie.Modifier) (AuthData, error) {
	auth, err := a.ExtractAuth(w, r)
	if err != nil {
		return AuthData{}, err
	}

	auth, err = a.SetAuthCookie(auth, w, co...)
	if err != nil {
		return AuthData{}, err
	}

	return auth, nil
}
