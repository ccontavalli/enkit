package oauth

import (
	"errors"
	"net/http"

	"github.com/ccontavalli/enkit/lib/kcerts"
	"github.com/ccontavalli/enkit/lib/khttp/kcookie"
	"golang.org/x/crypto/ssh"
	"golang.org/x/oauth2"
)

var ErrorLoops = errors.New("You have been redirected back to this url - but you still don't have an authentication token.\n" +
	"As a sentinent web server, I've decided that you human don't deserve any further redirect, as that would cause a loop\n" +
	"which would be bad for the future of the internet, my load, and your bandwidth. Hit refresh if you want, but there's likely\n" +
	"something wrong in your cookies, or your setup")
var ErrorCannotAuthenticate = errors.New("Who are you? Sorry, you have no authentication cookie, and there is no authentication service configured")
var ErrorStateUnsupported = errors.New("Incorrect API usage - the authentication method does not support propagating state")
var ErrorNotAuthenticated = errors.New("No authentication information found")

// An IAuthenticator is any object capable of performing authentication for a web server.
type IAuthenticator interface {
	PerformLogin(w http.ResponseWriter, r *http.Request, lm ...LoginModifier) error
	PerformAuth(w http.ResponseWriter, r *http.Request, mods ...kcookie.Modifier) (AuthData, error)
	GetCredentialsFromRequest(r *http.Request) (*CredentialsCookie, string, error)
}

type Identity struct {
	Id           string
	Username     string
	Organization string
	Groups       []string
}

func (i *Identity) GlobalName() string {
	return i.Username + "@" + i.Organization
}

func (i *Identity) Valid() bool {
	return i.Id != "" && i.Username != "" && i.Organization != ""
}

func (i *Identity) CertMod() kcerts.CertMod {
	if i.Organization == "github.com" {
		return func(certificate *ssh.Certificate) *ssh.Certificate {
			certificate.Extensions["login@github.com"] = i.Username
			return certificate
		}
	}
	return kcerts.NoOp
}

// CredentialsCookie is what is encrypted/decrypted in the cookie itself.
// Identity represents the identity of the user.
// Token represents the data that was obtained through oauth authentication.
// 
// Note that Token could be empty/undefined if the credentials were not certificate
// via oauth - by using, for example, email authentication.
type CredentialsCookie struct {
	Identity Identity
	Token    oauth2.Token
}

type LoginState struct {
	Secret []byte
	Target string
	State  interface{}
}

type LoginOptions struct {
	CookieOptions kcookie.Modifiers
	Target        string
	State         interface{}
	TemplateData  map[string]interface{}
}

type LoginModifier func(*LoginOptions)

func WithCookieOptions(mod ...kcookie.Modifier) LoginModifier {
	return func(lo *LoginOptions) {
		lo.CookieOptions = append(lo.CookieOptions, mod...)
	}
}
func WithTarget(target string) LoginModifier {
	return func(lo *LoginOptions) {
		lo.Target = target
	}
}
func WithState(state interface{}) LoginModifier {
	return func(lo *LoginOptions) {
		lo.State = state
	}
}
func WithTemplateData(data map[string]interface{}) LoginModifier {
	return func(lo *LoginOptions) {
		lo.TemplateData = data
	}
}

type LoginModifiers []LoginModifier

func (lm LoginModifiers) Apply(lo *LoginOptions) *LoginOptions {
	for _, m := range lm {
		m(lo)
	}
	return lo
}

type AuthData struct {
	Creds      *CredentialsCookie
	Identities []Identity
	Cookie     string
	Target     string
	State      interface{}
}

func (a *AuthData) Complete() bool {
	if a.Creds == nil {
		return false
	}
	if !a.Creds.Identity.Valid() {
		return false
	}
	if !a.Creds.Token.Valid() {
		return false
	}
	return true
}
