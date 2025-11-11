package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ccontavalli/enkit/lib/khttp/kcookie"
	"github.com/ccontavalli/enkit/lib/oauth/cookie"
	"github.com/ccontavalli/enkit/lib/token"
)

// Extractor is an object capable of extracting and verifying authentication information.
type Extractor struct {
	version int

	// Two versions of token.
	loginEncoder0 *token.TypeEncoder
	loginEncoder1 *token.TypeEncoder

	// String to prepend to the cookie name.
	// This is necessary when multiple instances of the oauth library are used within
	// the same application, or to ensure the uniqueness of the cookie name in a complex app.
	baseCookie string
}

type credentialsKey string

var CredentialsVersionKey = credentialsKey("version")

type CredentialsMeta struct {
	context.Context
}

func (ctx CredentialsMeta) Issued() time.Time {
	issued, _ := ctx.Value(token.IssuedTimeKey).(time.Time)
	return issued
}

func (ctx CredentialsMeta) Expires() time.Time {
	expire, _ := ctx.Value(token.ExpiresTimeKey).(time.Time)
	return expire
}

func (ctx CredentialsMeta) Max() time.Time {
	max, _ := ctx.Value(token.MaxTimeKey).(time.Time)
	return max
}

func (ctx CredentialsMeta) Version() int {
	version, _ := ctx.Value(CredentialsVersionKey).(int)
	return version
}

// ParseCredentialsCookie parses a string containing a CredentialsCookie, and returns the corresponding object.
func (a *Extractor) ParseCredentialsCookie(cookie string) (CredentialsMeta, *CredentialsCookie, error) {
	var credentials CredentialsCookie
	var err error
	var ctx context.Context

	if strings.HasPrefix(cookie, "1:") {
		ctx, err = a.loginEncoder1.Decode(context.Background(), []byte(cookie[2:]), &credentials)
		ctx = context.WithValue(ctx, CredentialsVersionKey, 1)
	} else {
		ctx, err = a.loginEncoder0.Decode(context.Background(), []byte(cookie), &credentials)
	}
	return CredentialsMeta{ctx}, &credentials, err
}

// EncodeCredentials generates a string containing a CredentialsCookie.
func (a *Extractor) EncodeCredentials(creds CredentialsCookie) (string, error) {
	var result []byte
	var cookie string
	var err error
	switch a.version {
	case 0:
		result, err = a.loginEncoder0.Encode(creds)
		cookie = string(result)
	case 1:
		result, err = a.loginEncoder1.Encode(creds)
		cookie = "1:" + string(result)
	default:
		err = fmt.Errorf("invalid version %d", a.version)
	}
	if err != nil {
		return "", err
	}
	return cookie, nil
}

// GetCredentialsFromRequest will parse and validate the credentials in an http request.
//
// If successful, it will return a CredentialsCookie pointer and the string content of the cookie.
// If no credentials, or invalid credentials, an error is returned with nil credentials and no cookie.
func (a *Extractor) GetCredentialsFromRequest(r *http.Request) (*CredentialsCookie, string, error) {
	cookie, err := r.Cookie(a.CredentialsCookieName())
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			return nil, "", ErrorNotAuthenticated
		}

		return nil, "", err
	}

	_, credentials, err := a.ParseCredentialsCookie(cookie.Value)
	if err != nil {
		return nil, "", err
	}
	if credentials == nil {
		return nil, "", fmt.Errorf("invalid nil credentials")
	}
	return credentials, cookie.Value, nil
}

func (a *Extractor) SetCredentialsOnResponse(ad AuthData, w http.ResponseWriter, co ...kcookie.Modifier) (AuthData, error) {
	ccookie, err := a.EncodeCredentials(*ad.Creds)
	if err != nil {
		return AuthData{}, err
	}
	http.SetCookie(w, cookie.CredentialsCookie(a.baseCookie, ccookie, co...))
	return AuthData{Creds: ad.Creds, Cookie: ccookie, Target: ad.Target, State: ad.State}, nil
}

// CredentialsCookieName returns the name of the cookie maintaing the set of user credentials.
//
// This cookie is the one used to determine what the user can and cannot do on the UI.
func (a *Extractor) CredentialsCookieName() string {
	return cookie.CredentialsCookieName(a.baseCookie)
}
