package kemail

import (
	"bytes"
	"fmt"
	"html/template"
	texttemplate "text/template"

	"github.com/ccontavalli/enkit/lib/kflags"
	"github.com/ccontavalli/enkit/lib/logger"
	"gopkg.in/gomail.v2"
)

// SendDialer sends a message via SMTP (gomail.Dialer implements this).
type SendDialer interface {
	DialAndSend(m ...*gomail.Message) error
}

// TemplateFlags defines template file flags for transactional emails.
type TemplateFlags struct {
	SubjectTemplate  []byte
	BodyHTMLTemplate []byte
	BodyTextTemplate []byte
}

// Register registers template flags.
func (f *TemplateFlags) Register(fs kflags.FlagSet, prefix string) *TemplateFlags {
	return f.RegisterWithHelp(
		fs,
		prefix,
		"Path to a Go template file for the email subject.",
		"Path to a Go template file for the email body (HTML).",
		"Path to a Go template file for the email body (Text).",
	)
}

// RegisterWithHelp registers template flags with custom help text.
func (f *TemplateFlags) RegisterWithHelp(fs kflags.FlagSet, prefix, subjectHelp, bodyHTMLHelp, bodyTextHelp string) *TemplateFlags {
	fs.ByteFileVar(&f.SubjectTemplate, prefix+"subject-template-file", "", subjectHelp, kflags.WithContent(f.SubjectTemplate))
	fs.ByteFileVar(&f.BodyHTMLTemplate, prefix+"body-html-template-file", "", bodyHTMLHelp, kflags.WithContent(f.BodyHTMLTemplate))
	fs.ByteFileVar(&f.BodyTextTemplate, prefix+"body-text-template-file", "", bodyTextHelp, kflags.WithContent(f.BodyTextTemplate))
	return f
}

// Templates contains parsed subject and body templates.
type Templates struct {
	Subject  *template.Template
	BodyHTML *template.Template
	BodyText *texttemplate.Template
}

// ParseTemplates parses subject and body templates.
func ParseTemplates(subject, bodyHTML, bodyText []byte) (*Templates, error) {
	if len(subject) == 0 {
		return nil, fmt.Errorf("subject template is required")
	}
	if len(bodyHTML) == 0 {
		return nil, fmt.Errorf("body html template is required")
	}
	if len(bodyText) == 0 {
		return nil, fmt.Errorf("body text template is required")
	}

	subjectTemplate, err := template.New("subject").Parse(string(subject))
	if err != nil {
		return nil, err
	}

	bodyHTMLTemplate, err := template.New("body_html").Parse(string(bodyHTML))
	if err != nil {
		return nil, err
	}

	bodyTextTemplate, err := texttemplate.New("body_text").Parse(string(bodyText))
	if err != nil {
		return nil, err
	}

	return &Templates{
		Subject:  subjectTemplate,
		BodyHTML: bodyHTMLTemplate,
		BodyText: bodyTextTemplate,
	}, nil
}

// TransactionalEmailer builds and sends templated emails.
type TransactionalEmailer struct {
	log           logger.Logger
	dialer        SendDialer
	senderFactory SingleSenderFactory
	fromAddress   string
	templates     *Templates
}

type transactionalOptions struct {
	log           logger.Logger
	dialer        SendDialer
	senderFactory SingleSenderFactory
	fromAddress   string
	templates     *Templates
}

// TransactionalModifier applies configuration to a TransactionalEmailer.
type TransactionalModifier func(*transactionalOptions) error

// TransactionalModifiers is a slice of TransactionalModifier values.
type TransactionalModifiers []TransactionalModifier

// Apply applies all modifiers to the options.
func (mods TransactionalModifiers) Apply(o *transactionalOptions) error {
	for _, m := range mods {
		if err := m(o); err != nil {
			return err
		}
	}
	return nil
}

// WithDialer sets the dialer used to send emails.
func WithDialer(dialer SendDialer) TransactionalModifier {
	return func(o *transactionalOptions) error {
		o.dialer = dialer
		return nil
	}
}

// WithTransactionalSenderFactory sets a sender factory for transactional emails.
func WithTransactionalSenderFactory(factory SingleSenderFactory) TransactionalModifier {
	return func(o *transactionalOptions) error {
		o.senderFactory = factory
		return nil
	}
}

// WithTransactionalSender uses a single sender for transactional emails.
func WithTransactionalSender(sender SingleSender) TransactionalModifier {
	return func(o *transactionalOptions) error {
		if sender == nil {
			o.senderFactory = nil
			return nil
		}
		o.senderFactory = singleSenderFactoryFunc(func() (SingleSender, error) {
			return sender, nil
		})
		return nil
	}
}

// WithFromAddress sets the From address for emails.
func WithFromAddress(fromAddress string) TransactionalModifier {
	return func(o *transactionalOptions) error {
		o.fromAddress = fromAddress
		return nil
	}
}

// WithTemplates sets the templates used for emails.
func WithTemplates(templates *Templates) TransactionalModifier {
	return func(o *transactionalOptions) error {
		o.templates = templates
		return nil
	}
}

// WithTransactionalLogger sets the logger for transactional emails.
func WithTransactionalLogger(log logger.Logger) TransactionalModifier {
	return func(o *transactionalOptions) error {
		o.log = log
		return nil
	}
}

func defaultTransactionalOptions() *transactionalOptions {
	return &transactionalOptions{
		log: logger.Go,
	}
}

// NewTransactionalEmailer creates a TransactionalEmailer from modifiers.
func NewTransactionalEmailer(mods ...TransactionalModifier) (*TransactionalEmailer, error) {
	opts := defaultTransactionalOptions()
	if err := TransactionalModifiers(mods).Apply(opts); err != nil {
		return nil, err
	}
	if opts.dialer == nil && opts.senderFactory == nil {
		return nil, fmt.Errorf("dialer or sender factory is required")
	}
	if opts.fromAddress == "" {
		return nil, fmt.Errorf("from address is required")
	}
	if opts.templates == nil || opts.templates.Subject == nil || opts.templates.BodyHTML == nil || opts.templates.BodyText == nil {
		return nil, fmt.Errorf("templates are required")
	}

	return &TransactionalEmailer{
		log:           opts.log,
		dialer:        opts.dialer,
		senderFactory: opts.senderFactory,
		fromAddress:   opts.fromAddress,
		templates:     opts.templates,
	}, nil
}

// BuildMessage constructs a gomail message from templates and data.
func (e *TransactionalEmailer) BuildMessage(to string, data map[string]interface{}) (*gomail.Message, error) {
	if to == "" {
		return nil, fmt.Errorf("recipient address is required")
	}
	if data == nil {
		data = map[string]interface{}{}
	}

	var body bytes.Buffer
	if err := e.templates.BodyHTML.Execute(&body, data); err != nil {
		return nil, fmt.Errorf("error executing body html template: %w", err)
	}

	var textBody bytes.Buffer
	if err := e.templates.BodyText.Execute(&textBody, data); err != nil {
		return nil, fmt.Errorf("error executing body text template: %w", err)
	}

	var subject bytes.Buffer
	if err := e.templates.Subject.Execute(&subject, data); err != nil {
		return nil, fmt.Errorf("error executing subject template: %w", err)
	}

	m := gomail.NewMessage()
	m.SetHeader("From", e.fromAddress)
	m.SetHeader("To", to)
	m.SetHeader("Subject", subject.String())
	m.SetBody("text/plain", textBody.String())
	m.AddAlternative("text/html", body.String())
	return m, nil
}

// Send builds and sends a templated email to a single recipient.
func (e *TransactionalEmailer) Send(to string, data map[string]interface{}) error {
	message, err := e.BuildMessage(to, data)
	if err != nil {
		return err
	}
	if e.senderFactory != nil {
		if err := Send(nil, []string{to}, func(_ string) (*gomail.Message, error) {
			return message, nil
		}, nil, WithSenderFactory(e.senderFactory), WithLogger(e.log)); err != nil {
			e.log.Errorf("Failed to send email to %s: %v", to, err)
			return fmt.Errorf("error sending email: %w", err)
		}
		return nil
	}
	if err := e.dialer.DialAndSend(message); err != nil {
		e.log.Errorf("Failed to send email to %s: %v", to, err)
		return fmt.Errorf("error sending email: %w", err)
	}
	return nil
}
