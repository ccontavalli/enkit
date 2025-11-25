package omail

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"math/rand"
	"net/url"
	"strings"
	texttemplate "text/template"
	"time"

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
	log              logger.Logger
	bodyHTMLTemplate *template.Template
	bodyTextTemplate *texttemplate.Template
	subjectTemplate  *template.Template
	tokenEncoder     *token.TypeEncoder
	dialer           Dialer
	fromAddress      string
	callbackURL      *url.URL
}

// EmailTokenPayload is the data encoded in the secure email token.
type EmailTokenPayload struct {
	Email  string
	Target string
	State  interface{}
}

// emailerOptions holds the internal configuration for the email authenticator.
type emailerOptions struct {
	rng              *rand.Rand
	log              logger.Logger
	SmtpHost         string
	SmtpPort         int
	SmtpUser         string
	SmtpPassword     string
	FromAddress      string
	SubjectTemplate  *template.Template
	BodyHTMLTemplate *template.Template
	BodyTextTemplate *texttemplate.Template
	TokenLifetime    time.Duration
	SymmetricKey     []byte
	CallbackURL      *url.URL
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
	SmtpHost         string
	SmtpPort         int
	SmtpUser         string
	SmtpPassword     string
	FromAddress      string
	SubjectTemplate  []byte
	BodyHTMLTemplate []byte
	BodyTextTemplate []byte
	TokenLifetime    time.Duration
	SymmetricKey     []byte
}

const kDefaultTemplateSubject = "Your login link"
const kDefaultTemplateHTMLBody = `<!DOCTYPE html>
<html>
<body style="font-family: sans-serif; line-height: 1.6; color: #333; background-color: #f4f4f4; padding: 20px;">
  <div style="max-width: 600px; margin: 0 auto; padding: 20px; border: 1px solid #eee; border-radius: 8px; background-color: #ffffff; box-shadow: 0 2px 4px rgba(0,0,0,0.1);">
    <h2 style="color: #333; margin-top: 0;">Login Request</h2>
    <p>Hello,</p>
    <p>We received a request to log in using this email address. To proceed, please click the button below:</p>
    <div style="text-align: center; margin: 30px 0;">
      <a href="{{.URL}}" style="display: inline-block; padding: 12px 24px; background-color: #007bff; color: #ffffff; text-decoration: none; border-radius: 5px; font-weight: bold; font-size: 16px;">Log In</a>
    </div>
    <p style="margin-bottom: 5px;">Or copy and paste this link into your browser:</p>
    <p style="word-break: break-all; margin-top: 0;"><a href="{{.URL}}" style="color: #007bff;">{{.URL}}</a></p>
    <hr style="border: none; border-top: 1px solid #eee; margin: 20px 0;">
    <p style="font-size: 0.85em; color: #777;">If you did not request this login link, please ignore this email.</p>
  </div>
</body>
</html>`
const kDefaultTemplateTextBody = `Login Request

Hello,

We received a request to log in using this email address. To proceed, please open the following link in your browser:

{{.URL}}

If you did not request this login link, please ignore this email.`

func EmailerDefaultFlags() *EmailerFlags {
	return &EmailerFlags{
		SmtpPort:         587,
		SubjectTemplate:  []byte(kDefaultTemplateSubject),
		BodyHTMLTemplate: []byte(kDefaultTemplateHTMLBody),
		BodyTextTemplate: []byte(kDefaultTemplateTextBody),
		TokenLifetime:    1 * time.Hour,
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
	fs.ByteFileVar(&f.BodyHTMLTemplate, prefix+"body-html-template-file", "", "Path to a Go template file for the login email body (HTML). Must contain {{.URL}}. If not set, a default email body is used.", kflags.WithContent(f.BodyHTMLTemplate))
	fs.ByteFileVar(&f.BodyTextTemplate, prefix+"body-text-template-file", "", "Path to a Go template file for the login email body (Text). Must contain {{.URL}}. If not set, a default email body is used.", kflags.WithContent(f.BodyTextTemplate))
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

		bodyTemplateStr := string(f.BodyHTMLTemplate)
		if bodyTemplateStr == "" {
			bodyTemplateStr = kDefaultTemplateHTMLBody
		}
		if !strings.Contains(bodyTemplateStr, "{{.URL}}") {
			return fmt.Errorf("body html template must contain {{.URL}}")
		}
		bodyHTMLTemplate, err := template.New("body_html").Parse(bodyTemplateStr)
		if err != nil {
			return err
		}

		bodyTextTemplateStr := string(f.BodyTextTemplate)
		if bodyTextTemplateStr == "" {
			bodyTextTemplateStr = kDefaultTemplateTextBody
		}
		if !strings.Contains(bodyTextTemplateStr, "{{.URL}}") {
			return fmt.Errorf("body text template must contain {{.URL}}")
		}
		bodyTextTemplate, err := texttemplate.New("body_text").Parse(bodyTextTemplateStr)
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
			o.log.Infof("Emailer symmetric key not provided, generating a new one.")
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
		o.BodyHTMLTemplate = bodyHTMLTemplate
		o.BodyTextTemplate = bodyTextTemplate
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

	smtpPasswordStatus := "(not set)"
	if opts.SmtpPassword != "" {
		smtpPasswordStatus = "(set)"
	}
	opts.log.Infof("NewEmailer configured with: SmtpHost=%s, SmtpPort=%d, SmtpUser=%s, SmtpPassword=%s, FromAddress=%s, TokenLifetime=%s",
		opts.SmtpHost, opts.SmtpPort, opts.SmtpUser, smtpPasswordStatus, opts.FromAddress, opts.TokenLifetime)

	return &Emailer{
		log:              opts.log,
		fromAddress:      opts.FromAddress,
		subjectTemplate:  opts.SubjectTemplate,
		bodyHTMLTemplate: opts.BodyHTMLTemplate,
		bodyTextTemplate: opts.BodyTextTemplate,
		tokenEncoder:     tokenEncoder,
		dialer:           gomail.NewDialer(opts.SmtpHost, opts.SmtpPort, opts.SmtpUser, opts.SmtpPassword),
		callbackURL:      opts.CallbackURL,
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

	loginOptions := oauth.LoginModifiers(lm).Apply(&oauth.LoginOptions{})

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

	for k, v := range loginOptions.TemplateData {
		templateData[k] = v
	}

	var body bytes.Buffer
	if err := e.bodyHTMLTemplate.Execute(&body, templateData); err != nil {
		return fmt.Errorf("error executing body html template: %w", err)
	}

	var textBody bytes.Buffer
	if err := e.bodyTextTemplate.Execute(&textBody, templateData); err != nil {
		return fmt.Errorf("error executing body text template: %w", err)
	}

	var subject bytes.Buffer
	if err := e.subjectTemplate.Execute(&subject, templateData); err != nil {
		return fmt.Errorf("error executing subject template: %w", err)
	}

	m := gomail.NewMessage()
	m.SetHeader("From", e.fromAddress)
	m.SetHeader("To", email)
	m.SetHeader("Subject", subject.String())

	m.SetBody("text/plain", textBody.String())
	m.AddAlternative("text/html", body.String())

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
