package omail

import (
	"fmt"
	"math/rand"
	"net/http"

	"github.com/ccontavalli/enkit/lib/kflags"
	"github.com/ccontavalli/enkit/lib/khttp"
	"github.com/ccontavalli/enkit/lib/khttp/kcookie"
	"github.com/ccontavalli/enkit/lib/logger"
	"github.com/ccontavalli/enkit/lib/oauth"
)

// Authenticator implements the full IAuthenticator interface for email-based authentication.
type Authenticator struct {
	log logger.Logger
	*Emailer
	extractor            *oauth.Extractor
}

// AuthenticatorFlags combines flags for the Emailer and the oauth.Extractor.
type AuthenticatorFlags struct {
	EmailerFlags
	oauth.SigningExtractorFlags
}

// Register registers the flags for the Authenticator on the given FlagSet.
func (f *AuthenticatorFlags) Register(fs kflags.FlagSet, prefix string) *AuthenticatorFlags {
	f.EmailerFlags.Register(fs, prefix+"email-auth-")
	f.SigningExtractorFlags.Register(fs, prefix+"email-auth-")
	return f
}

type authenticatorOptions struct {
	flags            *AuthenticatorFlags
	log              logger.Logger
	oauthOptions     oauth.Options
	emailerModifiers []EmailerModifier
}

func newAuthenticatorOptions(rng *rand.Rand) *authenticatorOptions {
	return &authenticatorOptions{
		log:          logger.Go,
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

// WithAuthenticatorLogger sets the logger for the authenticator.
func WithAuthenticatorLogger(log logger.Logger) AuthenticatorModifier {
	return func(o *authenticatorOptions) error {
		o.log = log
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

	opts.emailerModifiers = append(opts.emailerModifiers, WithEmailerLogger(opts.log))
	emailer, err := NewEmailer(rng, opts.emailerModifiers...)
	if err != nil {
		return nil, fmt.Errorf("failed to create emailer: %w", err)
	}

	return &Authenticator{
		log:                  opts.log,
		Emailer:              emailer,
		extractor:            extractor,
	}, nil
}

// PerformLogin sends a login email to the user.
func (a *Authenticator) PerformLogin(w http.ResponseWriter, r *http.Request, lm ...oauth.LoginModifier) error {
	if err := r.ParseForm(); err != nil {
		return err
	}

	if err := a.SendLoginEmail(r.Form, khttp.RemoteIP(r), lm...); err != nil {
		return err
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "Login email sent. Please check your inbox.")
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
	a.log.Infof("Issuing credential cookie to %s from %s", authData.Creds.Identity.GlobalName(), khttp.RemoteIP(r))
	return a.extractor.SetCredentialsOnResponse(authData, w, co...)
}

func (a *Authenticator) PrepareCredentialsCookie(ad oauth.AuthData, co ...kcookie.Modifier) (oauth.AuthData, *http.Cookie, error) {
	return a.extractor.PrepareCredentialsCookie(ad, co...)
}

// GetCredentialsFromRequest validates the session cookie.
func (a *Authenticator) GetCredentialsFromRequest(r *http.Request) (*oauth.CredentialsCookie, string, error) {
	return a.extractor.GetCredentialsFromRequest(r)
}
