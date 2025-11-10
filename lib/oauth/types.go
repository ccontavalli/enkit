package oauth

import (
	"net/http"

	"github.com/ccontavalli/enkit/lib/kcerts"
	"github.com/ccontavalli/enkit/lib/khttp/kcookie"
	"golang.org/x/crypto/ssh"
	"golang.org/x/oauth2"
)

// An IAuthenticator is any object capable of performing authentication for a web server.
type IAuthenticator interface {
	// PerformLogin initiates the login process.
	//
	// PerformLogin will redirect the user to the oauth IdP login page, after
	// generating encrypted cookies containing enough information to verify success
	// at the end of the process and to carry application state.
	//
	// PerformLogin will initiate the process even if the user is already logged in.
	//
	// The PerformLogin may not support all the login modifiers. Specifically,
	// WithCookieOptions may be silently ignored if no cookie is used by the
	// specific implementation. If state is supplied with WithState and the
	// underlying implementation cannot propagate state, the error
	// ErrorStateUnsupported will be returned instead.
	PerformLogin(w http.ResponseWriter, r *http.Request, lm ...LoginModifier) error

	// PerformAuth turns the credentials received into AuthData (and a cookie).
	//
	// PerformAuth is invoked at the END of the authentication process. The URL
	// of the code invoking PerformAuth is typically configured as the oauth
	// endpoint.
	//
	// If no error is returned:
	// * AuthData is guaranteed to be usable, although the Complete() method in
	//   AuthData can be used to verify that the process returned valid
	//   credentials.
	// * A cookie is guaranteed to have been set with the client, allowing for
	//   GetCredentialsFromRequest to be usable. This function can either set
	//   the cookie directly, or assume the cookie was already set by a 3rd party
	//   (for example, at the end of a redirect based authentication).
	//
	// If the error returned is ErrorNotAuthenticated, it means that
	// authentication data was not found at all, meaning that a Login process
	// probably needs to be started. This is useful to create handlers that
	// can act both as Login and Auth handlers, or to write handlers that
	// conditionally start the login process.
	PerformAuth(w http.ResponseWriter, r *http.Request, mods ...kcookie.Modifier) (AuthData, error)

	// GetCredentialsFromRequest extracts the credentials from an http request.
	//
	// This is useful to check if - for example - a user already authenticated
	// before invoking PerformLogin, or to verify that a credential cookie has
	// been supplied in a gRPC or headless application.
	//
	// If no authentication cookie is found (eg, user has not ever attempted
	// login), ErrorNotAuthenticated is returned. In general, though, if an
	// error is returned by GetCredentialsFromRequest the caller of this API
	// should invoke PerformLogin to re-try the login process blindly.
	GetCredentialsFromRequest(r *http.Request) (*CredentialsCookie, string, error)
}

type Identity struct {
	// Id is a globally unique identifier of the user.
	//
	// It is oauth provider specific, generally contains an integer or string
	// uniquely identifying the user, and a domain name used to namespace the id.
	Id string

	// The name by which a user goes by.
	//
	// Note that the Username tied to a specific user may change over time.
	Username string

	// An organization this username belongs to.
	// It is generally the entity issuing the username, the namespace denoting the
	// validity of the username.
	//
	// For example: with a gsuite account, the organization would be the domain name
	// tied with the gsuite account, for example "enfabrica.net". The administrators
	// of "enfabrica.net" can create new accounts, accounts @enfabrica.net are
	// guaranteed unique within "enfabrica.net" only. With a github account instead,
	// even though the account is used within an organization like "enfabrica", users
	// register with "github.com", and the username must be unique across the entire
	// "github.com" organization. So github.com is the Organization here.
	//
	// Username + "@" + Organization is guaranteed globally unique.
	// But unlike an Id, the Username may change.
	Organization string
	// Groups is a list of string identifying the groups the user is part of.
	Groups []string
}

// GlobalName returns a human friendly string identifying the user.
//
// It looks like an email, but it may or may not be a valid email address.
//
// For example: github users will have github.com as organization, and their login as Username.
//
//	The GlobalName will be username@github.com. Not a valid email.
//
// On the other hand: gsuite users for enfabrica.net will have enfabrica.net as organization,
//
//	and their username as Username, forming a valid email.
//
// Interpret the result as meaning "user by this name" @ "organization by this name".
func (i *Identity) GlobalName() string {
	return i.Username + "@" + i.Organization
}

// Valid returns true if the identity has been initialized.
func (i *Identity) Valid() bool {
	return i.Id != "" && i.Username != "" && i.Organization != ""
}

// Based on the kind of identity obtained, returns a modifier able to generate
// certificates to support that specific identity type.
func (i *Identity) CertMod() kcerts.CertMod {
	if i.Organization == "github.com" {
		return func(certificate *ssh.Certificate) *ssh.Certificate {
			certificate.Extensions["login@github.com"] = i.Username
			return certificate
		}
	}
	return kcerts.NoOp
}

// CredentialsCookie is what is encrypted within the authentication cookie returned
// to the browser or client.
type CredentialsCookie struct {
	// An abstract representation of the identity of the user.
	// This is independent of the authentication provider.
	Identity Identity
	Token    oauth2.Token
}


