package kemail

import (
	"fmt"

	"github.com/ccontavalli/enkit/lib/kflags"
	"gopkg.in/gomail.v2"
)

// Dialer establishes a connection for sending mail.
type Dialer interface {
	Dial() (gomail.SendCloser, error)
}

// DialerFlags defines SMTP configuration flags.
type DialerFlags struct {
	SmtpHost     string
	SmtpPort     int
	SmtpUser     string
	SmtpPassword string
	LocalName    string
}

// DefaultDialerFlags returns defaults for SMTP dialer flags.
func DefaultDialerFlags() *DialerFlags {
	return &DialerFlags{
		SmtpPort: 587,
	}
}

// Register registers the dialer flags.
func (f *DialerFlags) Register(fs kflags.FlagSet, prefix string) *DialerFlags {
	fs.StringVar(&f.SmtpHost, prefix+"smtp-host", f.SmtpHost, "SMTP host for sending emails. Mandatory.")
	fs.IntVar(&f.SmtpPort, prefix+"smtp-port", f.SmtpPort, "SMTP port for sending emails.")
	fs.StringVar(&f.SmtpUser, prefix+"smtp-user", f.SmtpUser, "SMTP user for sending emails.")
	fs.StringVar(&f.SmtpPassword, prefix+"smtp-password", f.SmtpPassword, "SMTP password for sending emails.")
	fs.StringVar(&f.LocalName, prefix+"smtp-local-name", f.LocalName, "Local hostname to present during SMTP handshake.")
	return f
}

// DialerOptions configures the SMTP dialer.
type DialerOptions struct {
	SmtpHost     string
	SmtpPort     int
	SmtpUser     string
	SmtpPassword string
	LocalName    string
}

// DialerModifier updates dialer options.
type DialerModifier func(*DialerOptions) error

// DialerModifiers is a slice of DialerModifier values.
type DialerModifiers []DialerModifier

// Apply applies all modifiers to the provided options.
func (mods DialerModifiers) Apply(o *DialerOptions) error {
	for _, m := range mods {
		if err := m(o); err != nil {
			return err
		}
	}
	return nil
}

// FromDialerFlags applies configuration from flags.
func FromDialerFlags(f *DialerFlags) DialerModifier {
	return func(o *DialerOptions) error {
		if f == nil {
			return nil
		}
		if f.SmtpHost == "" {
			return kflags.NewUsageErrorf("smtp-host flag is mandatory")
		}
		if f.SmtpPort <= 0 || f.SmtpPort > 65535 {
			return kflags.NewUsageErrorf("smtp-port must be a valid port number (1-65535)")
		}
		o.SmtpHost = f.SmtpHost
		o.SmtpPort = f.SmtpPort
		o.SmtpUser = f.SmtpUser
		o.SmtpPassword = f.SmtpPassword
		o.LocalName = f.LocalName
		return nil
	}
}

// WithLocalName overrides the local name used by the SMTP dialer.
func WithLocalName(name string) DialerModifier {
	return func(o *DialerOptions) error {
		o.LocalName = name
		return nil
	}
}

// NewDialer creates a gomail dialer from modifiers.
func NewDialer(mods ...DialerModifier) (*gomail.Dialer, error) {
	opts := &DialerOptions{}
	if err := DialerModifiers(mods).Apply(opts); err != nil {
		return nil, err
	}
	if opts.SmtpHost == "" {
		return nil, fmt.Errorf("smtp host is required")
	}
	if opts.SmtpPort <= 0 || opts.SmtpPort > 65535 {
		return nil, fmt.Errorf("smtp port must be a valid port number (1-65535)")
	}
	dialer := gomail.NewPlainDialer(opts.SmtpHost, opts.SmtpPort, opts.SmtpUser, opts.SmtpPassword)
	if opts.LocalName != "" {
		dialer.LocalName = opts.LocalName
	}
	return dialer, nil
}
