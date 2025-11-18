package omail

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"math/rand"
	"net/url"
	"time"
	"strings"

	"github.com/ccontavalli/enkit/lib/kflags"
	"github.com/ccontavalli/enkit/lib/logger"
	"github.com/ccontavalli/enkit/lib/oauth"
	"github.com/ccontavalli/enkit/lib/token"
	"gopkg.in/gomail.v2"
)

// Dialer is an interface for sending emails, allowing for mock implementations in tests.
type Dialer interface {
	DialAndSend(m ...*gomail.Message) error
}

// Emailer handles the sending of authentication emails.
type Emailer struct {
	log             logger.Logger
	bodyTemplate    *template.Template
	subjectTemplate *template.Template
	tokenEncoder    *token.TypeEncoder
	dialer          Dialer
	fromAddress     string
	callbackURL     *url.URL
}

// EmailTokenPayload is the data encoded in the secure email token.
type EmailTokenPayload struct {
	Email  string
	Target string
	State  interface{}
}

// emailerOptions holds the internal configuration for the email authenticator.
type emailerOptions struct {
	rng             *rand.Rand
	log             logger.Logger
	SmtpHost        string
	SmtpPort        int
	SmtpUser        string
	SmtpPassword    string
	FromAddress     string
	SubjectTemplate *template.Template
	BodyTemplate    *template.Template
	TokenLifetime   time.Duration
	SymmetricKey    []byte
	CallbackURL     *url.URL
}

// EmailerModifier is a function that applies a configuration change to the authenticator options.
type EmailerModifier func(*emailerOptions) error

// EmailerModifiers is a slice of EmailerModifier functions.
type EmailerModifiers []EmailerModifier

// Apply applies all modifiers to the given options.
func (mods EmailerModifiers) Apply(o *emailerOptions) error {
	for _, m := range mods {
		if err := m(o); err != nil {
			return err
		}
	}
	return nil
}

// EmailerFlags defines the command-line flags for the email authenticator.
type EmailerFlags struct {
	SmtpHost        string
	SmtpPort        int
	SmtpUser        string
	SmtpPassword    string
	FromAddress     string
	SubjectTemplate []byte
	BodyTemplate    []byte
	TokenLifetime   time.Duration
	SymmetricKey    []byte
}

const kDefaultTemplateSubject = "Your login link"
const kDefaultTemplateBody = "Click here to login: {{.URL}}"

func EmailerDefaultFlags() *EmailerFlags {
	return  &EmailerFlags {
		SmtpPort: 587,
		SubjectTemplate: []byte(kDefaultTemplateSubject),
		BodyTemplate: []byte(kDefaultTemplateBody),
		TokenLifetime: 30 * time.Minute,
	}
}

func (f *EmailerFlags) Register(fs kflags.FlagSet, prefix string) {
	fs.StringVar(&f.SmtpHost, prefix+"smtp-host", f.SmtpHost, "SMTP host for sending emails. Mandatory.")
	fs.IntVar(&f.SmtpPort, prefix+"smtp-port", f.SmtpPort, "SMTP port for sending emails.")
	fs.StringVar(&f.SmtpUser, prefix+"smtp-user", f.SmtpUser, "SMTP user for sending emails.")
	fs.StringVar(&f.SmtpPassword, prefix+"smtp-password", f.SmtpPassword, "SMTP password for sending emails.")
	fs.StringVar(&f.FromAddress, prefix+"from-address", f.FromAddress, "From address for sending emails. Mandatory.")
	fs.DurationVar(&f.TokenLifetime, prefix+"token-lifetime", f.TokenLifetime, "How long the login token is valid for.")

	fs.ByteFileVar(&f.SubjectTemplate, prefix+"subject-template-file", "", "Path to a Go template file for the login email subject. If not set, a default subject is used.", kflags.WithContent(f.SubjectTemplate))
	fs.ByteFileVar(&f.BodyTemplate, prefix+"body-template-file", "", "Path to a Go template file for the login email body. Must contain {{.URL}}. If not set, a default email body is used.", kflags.WithContent(f.BodyTemplate))
	fs.ByteFileVar(&f.SymmetricKey, prefix+"symmetric-key-file", "", "Path to a file containing the symmetric key for token encryption. If not set, a new key is generated.", kflags.WithContent(f.SymmetricKey))
}

// FromEmailerFlags returns a Modifier that applies the configuration from the Flags struct.
func FromEmailerFlags(f *EmailerFlags) EmailerModifier {
	return func(o *emailerOptions) error {
		if f.SmtpHost == "" {
			return kflags.NewUsageErrorf("smtp-host flag is mandatory")
		}
		if f.FromAddress == "" {
			return kflags.NewUsageErrorf("from-address flag is mandatory")
		}
		if f.SmtpPort <= 0 || f.SmtpPort > 65535 {
			return kflags.NewUsageErrorf("smtp-port must be a valid port number (1-65535)")
		}

		bodyTemplateStr := string(f.BodyTemplate)
		if bodyTemplateStr == "" {
			bodyTemplateStr = kDefaultTemplateBody
		}
		if !strings.Contains(bodyTemplateStr, "{{.URL}}") {
			return fmt.Errorf("body template must contain {{.URL}}")
		}
		bodyTemplate, err := template.New("body").Parse(bodyTemplateStr)
		if err != nil {
		  return err
		}

		subjectTemplateStr := string(f.SubjectTemplate)
		if subjectTemplateStr == "" {
			subjectTemplateStr = kDefaultTemplateSubject
		}
		subjectTemplate, err := template.New("subject").Parse(subjectTemplateStr)
		if err != nil {
			return err
		}


		key := f.SymmetricKey
		if len(key) == 0 {
			key, err = token.GenerateSymmetricKey(o.rng, 256)
			if err != nil {
				return fmt.Errorf("failed to generate symmetric key: %w", err)
			}
		}

		o.SmtpHost = f.SmtpHost
		o.SmtpPort = f.SmtpPort
		o.SmtpUser = f.SmtpUser
		o.SmtpPassword = f.SmtpPassword
		o.FromAddress = f.FromAddress
		o.TokenLifetime = f.TokenLifetime
		o.SubjectTemplate = subjectTemplate
		o.BodyTemplate = bodyTemplate
		o.SymmetricKey = key
		return nil
	}
}

// WithSymmetricKey sets the symmetric key for token encryption.
func WithSymmetricKey(key []byte) EmailerModifier {
	return func(o *emailerOptions) error {
		o.SymmetricKey = key
		return nil
	}
}

// WithCallbackURL sets the callback URL for the authenticator. This is a mandatory option.
func WithCallbackURL(u *url.URL) EmailerModifier {
	return func(o *emailerOptions) error {
		o.CallbackURL = u
		return nil
	}
}

// WithEmailerLogger sets the logger for the emailer.
func WithEmailerLogger(log logger.Logger) EmailerModifier {
	return func(o *emailerOptions) error {
		o.log = log
		return nil
	}
}

func defaultEmailerOptions(rng *rand.Rand) *emailerOptions {
	return &emailerOptions{
		rng: rng,
		log: logger.Go,
	}
}

func NewEmailer(rng *rand.Rand, mods ...EmailerModifier) (*Emailer, error) {
	opts := defaultEmailerOptions(rng)
	if err := EmailerModifiers(mods).Apply(opts); err != nil {
		return nil, err
	}

	if opts.CallbackURL == nil {
		return nil, fmt.Errorf("CallbackURL must be configured with WithCallbackURL")
	}

	if len(opts.SymmetricKey) == 0 {
		return nil, fmt.Errorf("symmetric key must be provided")
	}

	symmetricEncoder, err := token.NewSymmetricEncoder(opts.rng, token.UseSymmetricKey(opts.SymmetricKey))
	if err != nil {
		return nil, fmt.Errorf("error creating symmetric encoder: %w", err)
	}

	tokenEncoder := token.NewTypeEncoder(token.NewChainedEncoder(
		token.NewTimeEncoder(nil, opts.TokenLifetime),
		symmetricEncoder,
		token.NewBase64UrlEncoder(),
	))

	return &Emailer{
		log:             opts.log,
		fromAddress:     opts.FromAddress,
		subjectTemplate: opts.SubjectTemplate,
		bodyTemplate:    opts.BodyTemplate,
		tokenEncoder:    tokenEncoder,
		dialer:          gomail.NewDialer(opts.SmtpHost, opts.SmtpPort, opts.SmtpUser, opts.SmtpPassword),
		callbackURL:     opts.CallbackURL,
	}, nil
}

// CreateEmailToken generates a new encrypted token for the given parameters.
func (e *Emailer) CreateEmailToken(params url.Values, lm ...oauth.LoginModifier) (string, error) {
	email := params.Get("email")
	if email == "" {
		return "", fmt.Errorf("email parameter is required")
	}

	loginOptions := oauth.LoginModifiers(lm).Apply(&oauth.LoginOptions{})

	payload := EmailTokenPayload{
		Email:  email,
		Target: loginOptions.Target,
		State:  loginOptions.State,
	}

	encodedToken, err := e.tokenEncoder.Encode(payload)
	if err != nil {
		return "", fmt.Errorf("error encoding token: %w", err)
	}

	return string(encodedToken), nil
}

// SendLoginEmail generates and sends a login email to the user.
func (e *Emailer) SendLoginEmail(params url.Values, location string, lm ...oauth.LoginModifier) error {
	email := params.Get("email")
	if email == "" {
		return fmt.Errorf("email parameter is required")
	}

	encodedToken, err := e.CreateEmailToken(params, lm...)
	if err != nil {
		return err
	}

	destinationURL := *e.callbackURL
	q := destinationURL.Query()
	q.Set("token", encodedToken)
	destinationURL.RawQuery = q.Encode()

	templateData := make(map[string]interface{})
	templateData["URL"] = destinationURL.String()
	for k, v := range params {
		if len(v) > 0 {
			templateData[k] = v[0]
		}
	}

	var body bytes.Buffer
	if err := e.bodyTemplate.Execute(&body, templateData); err != nil {
		return fmt.Errorf("error executing body template: %w", err)
	}

	var subject bytes.Buffer
	if err := e.subjectTemplate.Execute(&subject, templateData); err != nil {
		return fmt.Errorf("error executing subject template: %w", err)
	}

	m := gomail.NewMessage()
	m.SetHeader("From", e.fromAddress)
	m.SetHeader("To", email)
	m.SetHeader("Subject", subject.String())
	m.SetBody("text/html", body.String())

	if err := e.dialer.DialAndSend(m); err != nil {
		return fmt.Errorf("error sending email: %w", err)
	}

	e.log.Infof("Login email sent to %s from %s", email, location)

	return nil
}

// ValidateEmailToken validates the given token and returns the payload.
func (e *Emailer) DecodeEmailToken(tokenStr string) (*EmailTokenPayload, error) {
	var payload EmailTokenPayload
	_, err := e.tokenEncoder.Decode(context.Background(), []byte(tokenStr), &payload)
	if err != nil {
		return nil, fmt.Errorf("error decoding token: %w", err)
	}
	return &payload, nil
}

func (e *Emailer) ValidateEmailToken(token string) (oauth.AuthData, error) {
	payload, err := e.DecodeEmailToken(token)
	if err != nil {
		return oauth.AuthData{}, err
	}

	if payload.Email == "" {
		return oauth.AuthData{}, fmt.Errorf("invalid token: empty email")
	}

	parts := strings.Split(payload.Email, "@")
	if len(parts) != 2 {
		return oauth.AuthData{}, fmt.Errorf("invalid email address: %s", payload.Email)
	}

	identity := oauth.Identity{
		Id:           "email:" + payload.Email,
		Username:     parts[0],
		Organization: parts[1],
	}

	creds := &oauth.CredentialsCookie{Identity: identity}
	return oauth.AuthData{Creds: creds, Target: payload.Target, State: payload.State}, nil
}
