package kemail

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/ccontavalli/enkit/lib/kflags"
	"gopkg.in/gomail.v2"
)

// Dialer establishes SMTP connections for sending mail.
//
// Implementations are not required to be concurrency-safe. Callers must
// serialize access to a given Dialer unless a higher-level abstraction such as
// smtp-shared already does so.
//
// Implementations are expected to be stable for their lifetime. In particular,
// when used with smtp-shared:
//   - Identity() must remain stable and equal for dialers that should reuse the
//     same shared SMTP worker/session.
//   - LogID() should remain stable and human-readable so operators can identify
//     the specific dialer configuration in logs.
type Dialer interface {
	// Dial establishes an SMTP session for sending mail.
	Dial() (gomail.SendCloser, error)
	// Identity returns the stable transport identity for this dialer.
	// Dialers that return the same Identity are treated as the same shared SMTP
	// transport and may reuse one shared worker/session.
	Identity() string
	// LogID returns a human-readable identifier for this specific dialer
	// configuration. It is intended for logs and debugging, not for sharing
	// decisions.
	LogID() string
}

// DialerFlags defines SMTP configuration flags.
type DialerFlags struct {
	SmtpHost         string
	SmtpPort         int
	SmtpUser         string
	SmtpPassword     string
	SmtpPasswordFile []byte
	LocalName        string
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
	fs.ByteFileVar(&f.SmtpPasswordFile, prefix+"smtp-password-file", "", "Path to a file containing the SMTP password.", kflags.WithContent(f.SmtpPasswordFile))
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
		if f.SmtpPassword == "" && len(f.SmtpPasswordFile) > 0 {
			f.SmtpPassword = strings.TrimSpace(string(f.SmtpPasswordFile))
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

type smtpDialer struct {
	dialer   *gomail.Dialer
	identity string
	logID    string
}

func (d *smtpDialer) Dial() (gomail.SendCloser, error) {
	return d.dialer.Dial()
}

func (d *smtpDialer) Identity() string {
	return d.identity
}

func (d *smtpDialer) LogID() string {
	return d.logID
}

// NewDialer creates an immutable SMTP dialer from modifiers.
func NewDialer(mods ...DialerModifier) (Dialer, error) {
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

	identity := fmt.Sprintf(
		"smtp:%s:%d:%s:%s:%x",
		opts.SmtpHost,
		opts.SmtpPort,
		opts.SmtpUser,
		opts.LocalName,
		sha256.Sum256([]byte(opts.SmtpPassword)),
	)
	logID := fmt.Sprintf("smtp %s@%s:%d", opts.SmtpUser, opts.SmtpHost, opts.SmtpPort)
	if opts.SmtpUser == "" {
		logID = fmt.Sprintf("smtp %s:%d", opts.SmtpHost, opts.SmtpPort)
	}
	if opts.LocalName != "" {
		logID = fmt.Sprintf("%s local=%s", logID, opts.LocalName)
	}
	fingerprint := sha256.Sum256([]byte(identity))
	logID = fmt.Sprintf("%s [%x]", logID, fingerprint[:4])

	dialer := gomail.NewPlainDialer(opts.SmtpHost, opts.SmtpPort, opts.SmtpUser, opts.SmtpPassword)
	if opts.LocalName != "" {
		dialer.LocalName = opts.LocalName
	}

	return &smtpDialer{
		dialer:   dialer,
		identity: identity,
		logID:    logID,
	}, nil
}
