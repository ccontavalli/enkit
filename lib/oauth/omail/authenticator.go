package omail

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/url"

	"github.com/ccontavalli/enkit/lib/kflags"
	"github.com/ccontavalli/enkit/lib/khttp/kcookie"
	"github.com/ccontavalli/enkit/lib/oauth"
)

// Authenticator implements the full IAuthenticator interface for email-based authentication.
type Authenticator struct {
	*Emailer
	extractor            *oauth.Extractor
	emailSentRedirectURL *url.URL
}

// AuthenticatorFlags combines flags for the Emailer and the oauth.Extractor.
type AuthenticatorFlags struct {
	EmailerFlags
	oauth.SigningExtractorFlags
	EmailSentRedirectURL string
}

// Register registers the flags for the Authenticator on the given FlagSet.
func (f *AuthenticatorFlags) Register(fs kflags.FlagSet, prefix string) *AuthenticatorFlags {
	f.EmailerFlags.Register(fs, prefix+"email-auth-")
	f.SigningExtractorFlags.Register(fs, prefix+"email-auth-")

	fs.StringVar(&f.EmailSentRedirectURL, prefix+"email-auth-sent-redirect-url", "", "URL to redirect to after the login email has been sent.")
	return f
}

func (f *AuthenticatorFlags) GetEmailSentRedirectURL() (*url.URL, error) {
	if f.EmailSentRedirectURL == "" {
		return nil, nil
	}
	return url.Parse(f.EmailSentRedirectURL)
}

type authenticatorOptions struct {
	flags            *AuthenticatorFlags
	oauthOptions     oauth.Options
	emailerModifiers []EmailerModifier
}

func newAuthenticatorOptions(rng *rand.Rand) *authenticatorOptions {
	return &authenticatorOptions{
		oauthOptions: oauth.DefaultOptions(rng),
	}
}

// AuthenticatorModifier is a function that applies a configuration change to the authenticator options.
type AuthenticatorModifier func(*authenticatorOptions) error

// FromAuthenticatorFlags returns a modifier that applies configuration from the AuthenticatorFlags struct.
func FromAuthenticatorFlags(flags *AuthenticatorFlags) AuthenticatorModifier {
	return func(o *authenticatorOptions) error {
		o.flags = flags
		if err := oauth.WithSigningExtractorFlags(&flags.SigningExtractorFlags)(&o.oauthOptions); err != nil {
			return err
		}
		o.emailerModifiers = append(o.emailerModifiers, FromEmailerFlags(&flags.EmailerFlags))
		return nil
	}
}

// WithOAuthModifiers allows passing oauth.Modifier functions to the authenticator.
func WithOAuthModifiers(mods ...oauth.Modifier) AuthenticatorModifier {
	return func(o *authenticatorOptions) error {
		return oauth.Modifiers(mods).Apply(&o.oauthOptions)
	}
}

// WithEmailerModifiers allows passing EmailerModifier functions to the authenticator.
func WithEmailerModifiers(mods ...EmailerModifier) AuthenticatorModifier {
	return func(o *authenticatorOptions) error {
		o.emailerModifiers = append(o.emailerModifiers, mods...)
		return nil
	}
}

// NewAuthenticator creates a new email-based authenticator.
func NewAuthenticator(rng *rand.Rand, mods ...AuthenticatorModifier) (*Authenticator, error) {
	opts := newAuthenticatorOptions(rng)
	for _, mod := range mods {
		if err := mod(opts); err != nil {
			return nil, err
		}
	}

	extractor, err := opts.oauthOptions.NewExtractor()
	if err != nil {
		return nil, fmt.Errorf("failed to create extractor: %w", err)
	}

	emailer, err := NewEmailer(rng, opts.emailerModifiers...)
	if err != nil {
		return nil, fmt.Errorf("failed to create emailer: %w", err)
	}

	var emailSentRedirectURL *url.URL
	if opts.flags != nil {
		emailSentRedirectURL, err = opts.flags.GetEmailSentRedirectURL()
		if err != nil {
			return nil, kflags.NewUsageErrorf("invalid email-sent-redirect-url: %w", err)
		}
	}

	return &Authenticator{
		Emailer:              emailer,
		extractor:            extractor,
		emailSentRedirectURL: emailSentRedirectURL,
	}, nil
}

// PerformLogin sends a login email to the user.
func (a *Authenticator) PerformLogin(w http.ResponseWriter, r *http.Request, lm ...oauth.LoginModifier) error {
	if err := r.ParseForm(); err != nil {
		return err
	}

	if err := a.SendLoginEmail(r.Form, lm...); err != nil {
		return err
	}

	if a.emailSentRedirectURL != nil {
		http.Redirect(w, r, a.emailSentRedirectURL.String(), http.StatusFound)
	} else {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "Login email sent. Please check your inbox.")
	}

	return nil
}

// PerformAuth validates the email token and creates a session cookie.
func (a *Authenticator) PerformAuth(w http.ResponseWriter, r *http.Request, co ...kcookie.Modifier) (oauth.AuthData, error) {
	encodedToken := r.URL.Query().Get("token")
	if encodedToken == "" {
		return oauth.AuthData{}, fmt.Errorf("token parameter is required")
	}

	authData, err := a.ValidateEmailToken(encodedToken)
	if err != nil {
		return oauth.AuthData{}, fmt.Errorf("invalid email token - %w", err)
	}
	return a.extractor.SetCredentialsOnResponse(authData, w, co...)
}

func (a *Authenticator) PrepareCredentialsCookie(ad oauth.AuthData, co ...kcookie.Modifier) (oauth.AuthData, *http.Cookie, error) {
	return a.extractor.PrepareCredentialsCookie(ad, co...)
}

// GetCredentialsFromRequest validates the session cookie.
func (a *Authenticator) GetCredentialsFromRequest(r *http.Request) (*oauth.CredentialsCookie, string, error) {
	return a.extractor.GetCredentialsFromRequest(r)
}
