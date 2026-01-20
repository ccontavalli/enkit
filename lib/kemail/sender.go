package kemail

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/ccontavalli/enkit/lib/kflags"
	"github.com/ccontavalli/enkit/lib/logger"
	"github.com/ccontavalli/enkit/lib/srand"
	"gopkg.in/gomail.v2"
)

// TimeSource provides the current time.
type TimeSource func() time.Time

// Sleeper pauses execution for a duration.
type Sleeper func(time.Duration)

// ProgressStatus describes the progress callback state.
type ProgressStatus string

const (
	// ProgressSending reports an attempt to send a message.
	ProgressSending ProgressStatus = "sending"
	// ProgressSent reports a successfully sent message.
	ProgressSent ProgressStatus = "sent"
	// ProgressError reports a failed attempt for a recipient.
	ProgressError ProgressStatus = "error"
	// ProgressGiveUp reports when the sender gives up on a recipient.
	ProgressGiveUp ProgressStatus = "give_up"
)

// Progress contains metadata about the current sending progress.
type Progress struct {
	Index     int
	Total     int
	Attempt   int
	Label     string
	Status    ProgressStatus
	Err       error
	Sent      int
	Remaining int
}

// ProgressCallback is invoked to report sending progress.
type ProgressCallback func(Progress)

// Flags configures the sender behavior.
type Flags struct {
	Wait        time.Duration
	MaxAttempts int
	Shuffle     bool
}

// DefaultFlags returns default sender flags.
func DefaultFlags() *Flags {
	return &Flags{
		Wait:        10 * time.Second,
		MaxAttempts: 0,
		Shuffle:     true,
	}
}

// Register registers sender flags.
func (f *Flags) Register(fs kflags.FlagSet, prefix string) *Flags {
	fs.DurationVar(&f.Wait, prefix+"email-retry-wait", f.Wait, "How long to wait between connection attempts.")
	fs.IntVar(&f.MaxAttempts, prefix+"email-max-attempts", f.MaxAttempts, "Max attempts per recipient (0 means unlimited).")
	fs.BoolVar(&f.Shuffle, prefix+"email-shuffle", f.Shuffle, "Shuffle recipient list before sending.")
	return f
}

// Options controls the send behavior.
type Options struct {
	log      logger.Logger
	Now      TimeSource
	Sleep    Sleeper
	Rng      *rand.Rand
	Progress ProgressCallback

	Flags
}

// Modifier mutates Options.
type Modifier func(*Options)

// Modifiers is a slice of Modifier values.
type Modifiers []Modifier

// Apply applies all modifiers to the Options.
func (mods Modifiers) Apply(o *Options) *Options {
	for _, m := range mods {
		m(o)
	}
	return o
}

// FromFlags returns a modifier applying flag values.
func FromFlags(f *Flags) Modifier {
	return func(o *Options) {
		if f == nil {
			return
		}
		o.Flags = *f
	}
}

// WithLogger sets the logger.
func WithLogger(log logger.Logger) Modifier {
	return func(o *Options) {
		o.log = log
	}
}

// WithTimeSource overrides the time source.
func WithTimeSource(now TimeSource) Modifier {
	return func(o *Options) {
		o.Now = now
	}
}

// WithSleep overrides the sleep function.
func WithSleep(sleep Sleeper) Modifier {
	return func(o *Options) {
		o.Sleep = sleep
	}
}

// WithRng overrides the random number generator used for shuffling.
func WithRng(rng *rand.Rand) Modifier {
	return func(o *Options) {
		o.Rng = rng
	}
}

// WithWait overrides the retry wait duration.
func WithWait(wait time.Duration) Modifier {
	return func(o *Options) {
		o.Wait = wait
	}
}

// WithMaxAttempts overrides the maximum number of attempts per recipient.
func WithMaxAttempts(attempts int) Modifier {
	return func(o *Options) {
		o.MaxAttempts = attempts
	}
}

// WithShuffle enables or disables recipient shuffling.
func WithShuffle(shuffle bool) Modifier {
	return func(o *Options) {
		o.Shuffle = shuffle
	}
}

// WithProgress sets a callback to report progress.
func WithProgress(cb ProgressCallback) Modifier {
	return func(o *Options) {
		o.Progress = cb
	}
}

// MessageBuilder builds a gomail message for a recipient.
type MessageBuilder[T any] func(T) (*gomail.Message, error)

// RecipientLabeler returns a label for logging a recipient.
type RecipientLabeler[T any] func(T) string

// Send sends email messages built for each recipient with retry logic.
func Send[T any](dialer Dialer, recipients []T, build MessageBuilder[T], labeler RecipientLabeler[T], mods ...Modifier) error {
	if dialer == nil {
		return fmt.Errorf("dialer is required")
	}
	if build == nil {
		return fmt.Errorf("message builder is required")
	}
	if len(recipients) == 0 {
		return nil
	}

	opts := New(mods...)
	sent := make([]T, len(recipients))
	copy(sent, recipients)
	if opts.Shuffle && opts.Rng != nil {
		opts.Rng.Shuffle(len(sent), func(i, j int) {
			sent[i], sent[j] = sent[j], sent[i]
		})
	}

	lastAttempt := time.Unix(0, 0)
	var sender gomail.SendCloser
	defer func() {
		if sender != nil {
			_ = sender.Close()
		}
	}()

	total := len(sent)
	sentCount := 0

	for idx, recipient := range sent {
		attempts := 0
		label := fmt.Sprintf("recipient %d", idx)
		if labeler != nil {
			label = labeler(recipient)
		}

		report := func(status ProgressStatus, err error) {
			if opts.Progress == nil {
				return
			}
			opts.Progress(Progress{
				Index:     idx,
				Total:     total,
				Attempt:   attempts + 1,
				Label:     label,
				Status:    status,
				Err:       err,
				Sent:      sentCount,
				Remaining: total - sentCount,
			})
		}

		for {
			if opts.MaxAttempts > 0 && attempts >= opts.MaxAttempts {
				report(ProgressGiveUp, fmt.Errorf("exceeded %d attempts for %s", opts.MaxAttempts, label))
				return fmt.Errorf("exceeded %d attempts for %s", opts.MaxAttempts, label)
			}

			if sender == nil {
				lastAttempt = waitForRetry(lastAttempt, opts.Wait, opts.Now, opts.Sleep, opts.log)
				var err error
				sender, err = dialer.Dial()
				if err != nil {
					opts.log.Warnf("attempt %d - connection failed - %v", attempts, err)
					report(ProgressError, err)
					attempts++
					continue
				}
				opts.log.Infof("connected to SMTP server")
			}

			message, err := build(recipient)
			if err != nil {
				return err
			}

			opts.log.Infof("attempt %d - sending %s", attempts, label)
			report(ProgressSending, nil)
			if err := gomail.Send(sender, message); err != nil {
				opts.log.Warnf("attempt %d - sending %s failed - %v", attempts, label, err)
				report(ProgressError, err)
				_ = sender.Close()
				sender = nil
				attempts++
				continue
			}
			sentCount++
			report(ProgressSent, nil)
			break
		}
	}

	return nil
}

// SendMessages sends already-built messages in order (with optional shuffle).
func SendMessages(dialer Dialer, messages []*gomail.Message, labeler func(*gomail.Message) string, mods ...Modifier) error {
	return Send(dialer, messages, func(m *gomail.Message) (*gomail.Message, error) {
		return m, nil
	}, labeler, mods...)
}

// New creates Options with defaults and applies modifiers.
func New(mods ...Modifier) *Options {
	options := &Options{
		log:   logger.Go,
		Now:   time.Now,
		Sleep: time.Sleep,
		Rng:   rand.New(srand.Source),
		Flags: *DefaultFlags(),
	}
	return Modifiers(mods).Apply(options)
}

func waitForRetry(last time.Time, wait time.Duration, now TimeSource, sleep Sleeper, log logger.Logger) time.Time {
	if wait <= 0 {
		return now()
	}
	current := now()
	elapsed := current.Sub(last)
	if elapsed >= wait {
		log.Infof("last attempt was at %s - now %s - wait %s, retrying immediately", last, current, wait)
		return current
	}

	retry := wait - elapsed
	log.Infof("last attempt was at %s - now %s - wait %s, will retry in %s", last, current, wait, retry)
	sleep(retry)
	return now()
}
