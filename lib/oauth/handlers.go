package oauth

import (
	"context"
	"log"
	"net/http"
	"path/filepath"

	"github.com/ccontavalli/enkit/lib/khttp"
	"github.com/ccontavalli/enkit/lib/khttp/kassets"
)

// Mapper configures all the URLs to redirect to / unless an authentication cookie is provided by the browser.
// Further, it configures / to redirect and perform oauth authentication.
func Mapper(a IAuthenticator, mapper kassets.AssetMapper, lm ...LoginModifier) kassets.AssetMapper {
	return func(original, name string, handler khttp.FuncHandler) []string {
		ext := filepath.Ext(original)
		switch {
		case name == "/favicon.ico":
			return mapper(original, name, handler)
		case name == "/":
			return mapper(original, name, MakeAuthHandler(a, MakeLoginHandler(a, handler, lm...)))
		case ext == ".html":
			return mapper(original, name, WithCredentialsOrRedirect(a, handler, "/"))
		default:
			return mapper(original, name, WithCredentialsOrError(a, handler))
		}
	}
}

// GetCredentials returns the credentials of a user extracted from an authentication cookie.
// Returns nil if the context has no credentials.
func GetCredentials(ctx context.Context) *CredentialsCookie {
	creds, _ := ctx.Value("creds").(*CredentialsCookie)
	return creds
}

// SetCredentials returns a context with the credentials of the user added.
// Use GetCredentials to retrieve them later.
func SetCredentials(ctx context.Context, creds *CredentialsCookie) context.Context {
	return context.WithValue(ctx, "creds", creds)
}

// WithCredentials invokes the handler with the identity of the user supplied in the context.
func WithCredentials(a IAuthenticator, handler khttp.FuncHandler) khttp.FuncHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		creds, _, err := a.GetCredentialsFromRequest(r)
		if creds != nil && err == nil {
			r = r.WithContext(SetCredentials(r.Context(), creds))
		}
		handler(w, r)
	}
}

// WithCredentialsOrRedirect invokes the handler if credentials are available, or redirects if they are not.
func WithCredentialsOrRedirect(a IAuthenticator, handler khttp.FuncHandler, target string) khttp.FuncHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		creds, _, err := a.GetCredentialsFromRequest(r)
		if creds == nil || err != nil {
			http.Redirect(w, r, target, http.StatusTemporaryRedirect)
		} else {
			r = r.WithContext(SetCredentials(r.Context(), creds))
			handler(w, r)
		}
	}
}

// WithCredentialsOrError invokes the handler if credentials are available, errors out if not.
func WithCredentialsOrError(a IAuthenticator, handler khttp.FuncHandler) khttp.FuncHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		creds, _, err := a.GetCredentialsFromRequest(r)
		if creds == nil || err != nil {
			http.Error(w, "not authorized", http.StatusUnauthorized)
		} else {
			r = r.WithContext(SetCredentials(r.Context(), creds))
			handler(w, r)
		}
	}
}

// MakeLoginHandler turns the specified handler into a LoginHandler.
func MakeLoginHandler(a IAuthenticator, handler khttp.FuncHandler, lm ...LoginModifier) khttp.FuncHandler {
	loginHandler := LoginHandler(a, lm...)

	return func(w http.ResponseWriter, r *http.Request) {
		creds := GetCredentials(r.Context())
		if creds != nil {
			r = r.WithContext(SetCredentials(r.Context(), creds))
			handler(w, r)
			return
		}

		creds, _, err := a.GetCredentialsFromRequest(r)
		if creds != nil && err == nil {
			r = r.WithContext(SetCredentials(r.Context(), creds))
			handler(w, r)
			return
		}
		loginHandler(w, r)
	}
}

// LoginHandler creates and returns a LoginHandler.
func LoginHandler(a IAuthenticator, lm ...LoginModifier) khttp.FuncHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		err := a.PerformLogin(w, r, lm...)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			log.Printf("ERROR - could not complete login - %s", err)
		}
	}
}

// MakeAuthHandler turns the specified handler into an AuthHandler.
func MakeAuthHandler(a IAuthenticator, handler khttp.FuncHandler) khttp.FuncHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := a.PerformAuth(w, r)
		if err == nil && data.Complete() {
			ctx := SetCredentials(r.Context(), data.Creds)
			r = r.WithContext(ctx)
		}
		if !CheckRedirect(w, r, data) {
			handler(w, r)
		}
	}
}

// AuthHandler returns the http handler to be invoked at the end of the oauth process.
func AuthHandler(a IAuthenticator) khttp.FuncHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := a.PerformAuth(w, r)
		if err != nil || !data.Complete() {
			http.Error(w, "your lack of authentication cookie is impressive - something went wrong", http.StatusInternalServerError)
			log.Printf("ERROR - could not complete authentication - %s", err)
			return
		}

		if !CheckRedirect(w, r, data) {
			http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		}
	}
}

// CheckRedirect checks AuthData to see if its state warrants a redirect.
// Returns true if it did redirect, false if a redirect was unnecessary.
func CheckRedirect(w http.ResponseWriter, r *http.Request, ad AuthData) bool {
	if ad.Target == "" {
		return false
	}
	http.Redirect(w, r, ad.Target, http.StatusTemporaryRedirect)
	return true
}
