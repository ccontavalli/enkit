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

// SingleSender sends a single message and can release any resources.
type SingleSender interface {
	Send(message *gomail.Message) error
	Close() error
}

// SingleSenderFactory creates senders for single-message delivery.
type SingleSenderFactory interface {
	Open() (SingleSender, error)
}

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

// ProgressAction controls how the sender continues.
type ProgressAction string

const (
	// ProgressContinue keeps sending normally.
	ProgressContinue ProgressAction = "continue"
	// ProgressSkip skips the current recipient without sending.
	ProgressSkip ProgressAction = "skip"
	// ProgressPause stops sending after the current recipient.
	ProgressPause ProgressAction = "pause"
	// ProgressCancel stops sending immediately.
	ProgressCancel ProgressAction = "cancel"
)

// ErrPaused indicates sending was paused via progress callback.
var ErrPaused = fmt.Errorf("sending paused")

// ErrCanceled indicates sending was canceled via progress callback.
var ErrCanceled = fmt.Errorf("sending canceled")

// Progress contains metadata about the current sending progress.
type Progress struct {
	Index     int
	Total     int
	Attempt   int
	Label     string
	Recipient string
	Status    ProgressStatus
	Err       error
	Sent      int
	Remaining int
}

// ProgressCallback is invoked to report sending progress.
type ProgressCallback func(Progress) ProgressAction

// Flags configures the sender behavior.
type Flags struct {
	Wait        time.Duration
	MaxAttempts int
	Shuffle     bool
	Sender      string
	FakeDelay   time.Duration
}

// DefaultFlags returns default sender flags.
func DefaultFlags() *Flags {
	return &Flags{
		Wait:        10 * time.Second,
		MaxAttempts: 0,
		Shuffle:     true,
		Sender:      "smtp",
		FakeDelay:   0,
	}
}

// Register registers sender flags.
func (f *Flags) Register(fs kflags.FlagSet, prefix string) *Flags {
	fs.DurationVar(&f.Wait, prefix+"email-retry-wait", f.Wait, "How long to wait between connection attempts.")
	fs.IntVar(&f.MaxAttempts, prefix+"email-max-attempts", f.MaxAttempts, "Max attempts per recipient (0 means unlimited).")
	fs.BoolVar(&f.Shuffle, prefix+"email-shuffle", f.Shuffle, "Shuffle recipient list before sending.")
	fs.StringVar(&f.Sender, prefix+"email-sender", f.Sender, "Email sender backend (smtp or fake).")
	fs.DurationVar(&f.FakeDelay, prefix+"email-fake-delay", f.FakeDelay, "Delay between fake email sends.")
	return f
}

// Options controls the send behavior.
type Options struct {
	log           logger.Logger
	Now           TimeSource
	Sleep         Sleeper
	Rng           *rand.Rand
	Progress      ProgressCallback
	SenderFactory SingleSenderFactory

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

// WithSenderFactory overrides the sender factory.
func WithSenderFactory(factory SingleSenderFactory) Modifier {
	return func(o *Options) {
		o.SenderFactory = factory
	}
}

// WithSingleSender uses a fixed single sender instance.
func WithSingleSender(sender SingleSender) Modifier {
	return func(o *Options) {
		if sender == nil {
			o.SenderFactory = nil
			return
		}
		o.SenderFactory = singleSenderFactoryFunc(func() (SingleSender, error) {
			return sender, nil
		})
	}
}

// WithSenderType overrides the sender backend.
func WithSenderType(sender string) Modifier {
	return func(o *Options) {
		o.Sender = sender
	}
}

// WithFakeDelay overrides the delay for fake sends.
func WithFakeDelay(delay time.Duration) Modifier {
	return func(o *Options) {
		o.FakeDelay = delay
	}
}

// MessageBuilder builds a gomail message for a recipient.
type MessageBuilder[T any] func(T) (*gomail.Message, error)

// RecipientLabeler returns a label for logging a recipient.
type RecipientLabeler[T any] func(T) string

// Send sends email messages built for each recipient with retry logic.
func Send[T any](dialer Dialer, recipients []T, build MessageBuilder[T], labeler RecipientLabeler[T], mods ...Modifier) error {
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
	senderFactory := opts.SenderFactory
	if senderFactory == nil {
		var err error
		senderFactory, err = SenderFactoryFromFlags(dialer, &opts.Flags, opts.log, opts.Sleep)
		if err != nil {
			return err
		}
	}
	var sender SingleSender
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

		report := func(status ProgressStatus, err error) ProgressAction {
			if opts.Progress == nil {
				return ProgressContinue
			}
			action := opts.Progress(Progress{
				Index:     idx,
				Total:     total,
				Attempt:   attempts + 1,
				Label:     label,
				Recipient: label,
				Status:    status,
				Err:       err,
				Sent:      sentCount,
				Remaining: total - sentCount,
			})
			if action == "" {
				return ProgressContinue
			}
			return action
		}

		for {
			if opts.MaxAttempts > 0 && attempts >= opts.MaxAttempts {
				report(ProgressGiveUp, fmt.Errorf("exceeded %d attempts for %s", opts.MaxAttempts, label))
				return fmt.Errorf("exceeded %d attempts for %s", opts.MaxAttempts, label)
			}

			if sender == nil {
				lastAttempt = waitForRetry(lastAttempt, opts.Wait, opts.Now, opts.Sleep, opts.log)
				var err error
				sender, err = senderFactory.Open()
				if err != nil {
					opts.log.Warnf("attempt %d - sender open failed - %v", attempts, err)
					report(ProgressError, err)
					attempts++
					continue
				}
				opts.log.Infof("sender ready")
			}

			action := report(ProgressSending, nil)
			if action == ProgressSkip {
				break
			}
			if action == ProgressPause {
				return ErrPaused
			}
			if action == ProgressCancel {
				return ErrCanceled
			}

			message, err := build(recipient)
			if err != nil {
				return err
			}

			opts.log.Infof("attempt %d - sending %s", attempts, label)
			if err := sender.Send(message); err != nil {
				opts.log.Warnf("attempt %d - sending %s failed - %v", attempts, label, err)
				action = report(ProgressError, err)
				if action == ProgressPause {
					return ErrPaused
				}
				if action == ProgressCancel {
					return ErrCanceled
				}
				_ = sender.Close()
				sender = nil
				attempts++
				continue
			}
			sentCount++
			action = report(ProgressSent, nil)
			if action == ProgressPause {
				return ErrPaused
			}
			if action == ProgressCancel {
				return ErrCanceled
			}
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

type singleSenderFactoryFunc func() (SingleSender, error)

func (f singleSenderFactoryFunc) Open() (SingleSender, error) {
	return f()
}

// FakeSender logs emails instead of sending them.
type FakeSender struct {
	Delay time.Duration
	Log   logger.Logger
	Sleep Sleeper
}

// NewFakeSender returns a fake sender that logs and sleeps between sends.
func NewFakeSender(delay time.Duration, log logger.Logger) *FakeSender {
	return &FakeSender{Delay: delay, Log: log}
}

func (s *FakeSender) Send(message *gomail.Message) error {
	log := s.Log
	if log == nil {
		log = logger.Go
	}
	to := message.GetHeader("To")
	subject := message.GetHeader("Subject")
	log.Infof("fake send to %v subject %v", to, subject)
	if s.Delay > 0 {
		sleep := s.Sleep
		if sleep == nil {
			sleep = time.Sleep
		}
		sleep(s.Delay)
	}
	return nil
}

func (s *FakeSender) Close() error {
	return nil
}

// dialerSenderFactory uses a Dialer to open SMTP senders.
type dialerSenderFactory struct {
	dialer Dialer
}

func (f *dialerSenderFactory) Open() (SingleSender, error) {
	sender, err := f.dialer.Dial()
	if err != nil {
		return nil, err
	}
	return &gomailSingleSender{sender: sender}, nil
}

// fakeSenderFactory builds FakeSender instances.
type fakeSenderFactory struct {
	delay time.Duration
	log   logger.Logger
	sleep Sleeper
}

func (f *fakeSenderFactory) Open() (SingleSender, error) {
	return &FakeSender{Delay: f.delay, Log: f.log, Sleep: f.sleep}, nil
}

// gomailSingleSender adapts a gomail.SendCloser to SingleSender.
type gomailSingleSender struct {
	sender gomail.SendCloser
}

func (s *gomailSingleSender) Send(message *gomail.Message) error {
	return gomail.Send(s.sender, message)
}

func (s *gomailSingleSender) Close() error {
	return s.sender.Close()
}

// SenderFactoryFromFlags returns a sender factory based on flags.
func SenderFactoryFromFlags(dialer Dialer, flags *Flags, log logger.Logger, sleep Sleeper) (SingleSenderFactory, error) {
	sender := "smtp"
	if flags != nil && flags.Sender != "" {
		sender = flags.Sender
	}
	switch sender {
	case "smtp":
		if dialer == nil {
			return nil, fmt.Errorf("dialer is required for smtp sender")
		}
		return &dialerSenderFactory{dialer: dialer}, nil
	case "fake":
		delay := time.Duration(0)
		if flags != nil {
			delay = flags.FakeDelay
		}
		return &fakeSenderFactory{delay: delay, log: log, sleep: sleep}, nil
	default:
		return nil, fmt.Errorf("unknown email sender: %s", sender)
	}
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
