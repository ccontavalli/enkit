package kemail

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"time"

	"gopkg.in/gomail.v2"
)

// DefaultSharedSenderIdleTimeout controls how long an SMTP session stays open
// after the last shared sender activity.
const DefaultSharedSenderIdleTimeout = 3 * time.Minute

// SharedProviderFunc returns the shared provider to use for smtp-shared mode.
type SharedProviderFunc func() *SharedSenderProvider

// SharedSenderIdentityProvider can override how shared SMTP workers are keyed.
type SharedSenderIdentityProvider interface {
	SharedSenderIdentity() string
}

// SharedSenderProvider manages shared SMTP sessions keyed by transport identity.
//
// Each identity owns one worker goroutine that serializes all access to the
// underlying SMTP session. The worker keeps the session open while it is in use,
// closes it after an idle timeout, and enforces a minimum time between re-dials.
type SharedSenderProvider struct {
	idleTimeout time.Duration

	mu         sync.Mutex
	workers    map[string]*sharedSenderWorker
	generation uint64
}

// NewSharedSenderProvider creates a provider that keeps idle SMTP sessions open
// for the requested timeout.
func NewSharedSenderProvider(idleTimeout time.Duration) *SharedSenderProvider {
	if idleTimeout <= 0 {
		idleTimeout = DefaultSharedSenderIdleTimeout
	}
	return &SharedSenderProvider{
		idleTimeout: idleTimeout,
		workers:     map[string]*sharedSenderWorker{},
	}
}

// Close closes any shared SMTP sessions managed by this provider and
// invalidates previously created factories.
func (p *SharedSenderProvider) Close() error {
	if p == nil {
		return nil
	}

	p.mu.Lock()
	workers := make([]*sharedSenderWorker, 0, len(p.workers))
	for _, worker := range p.workers {
		workers = append(workers, worker)
	}
	p.generation++
	p.workers = map[string]*sharedSenderWorker{}
	p.mu.Unlock()

	var firstErr error
	for _, worker := range workers {
		if err := worker.shutdown(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (p *SharedSenderProvider) factoryForDialer(dialer Dialer, overrideRecipient string, wait time.Duration, sleep Sleeper) (SingleSenderFactory, error) {
	return p.factoryForDialerWithClock(dialer, overrideRecipient, wait, time.Now, sleep)
}

func (p *SharedSenderProvider) factoryForDialerWithClock(dialer Dialer, overrideRecipient string, wait time.Duration, now TimeSource, sleep Sleeper) (SingleSenderFactory, error) {
	if dialer == nil {
		return nil, fmt.Errorf("dialer is required for smtp sender")
	}

	identity, err := sharedSenderIdentity(dialer)
	if err != nil {
		return nil, err
	}
	frozen := freezeSharedDialer(dialer)

	p.mu.Lock()
	generation := p.generation
	p.mu.Unlock()

	return &sharedSenderFactory{
		provider:          p,
		generation:        generation,
		identity:          fmt.Sprintf("%s|wait=%d", identity, wait),
		dialer:            frozen,
		wait:              wait,
		now:               now,
		sleep:             sleep,
		overrideRecipient: overrideRecipient,
	}, nil
}

type sharedSenderFactory struct {
	provider          *SharedSenderProvider
	generation        uint64
	identity          string
	dialer            Dialer
	wait              time.Duration
	now               TimeSource
	sleep             Sleeper
	overrideRecipient string
}

func (f *sharedSenderFactory) Open() (SingleSender, error) {
	if f.provider == nil {
		return nil, fmt.Errorf("shared sender provider is not configured")
	}

	worker, err := f.provider.workerFor(f.generation, f.identity, f.dialer, f.wait, f.now, f.sleep)
	if err != nil {
		return nil, err
	}
	return &sharedSingleSender{
		worker:            worker,
		overrideRecipient: f.overrideRecipient,
	}, nil
}

type sharedSingleSender struct {
	worker            *sharedSenderWorker
	overrideRecipient string
}

func (s *sharedSingleSender) Send(message *gomail.Message) error {
	if s.worker == nil {
		return fmt.Errorf("shared sender provider is not configured")
	}
	return s.worker.send(message, s.overrideRecipient)
}

func (s *sharedSingleSender) Close() error {
	return nil
}

type sharedSenderWorker struct {
	dialer      Dialer
	idleTimeout time.Duration
	wait        time.Duration
	now         TimeSource
	sleep       Sleeper

	requests chan sharedSendRequest
	done     chan struct{}
	exited   chan struct{}

	once     sync.Once
	closeErr error
}

type sharedSendRequest struct {
	message           *gomail.Message
	overrideRecipient string
	reply             chan error
}

func newSharedSenderWorker(dialer Dialer, idleTimeout, wait time.Duration, now TimeSource, sleep Sleeper) *sharedSenderWorker {
	if now == nil {
		now = time.Now
	}
	if sleep == nil {
		sleep = time.Sleep
	}
	worker := &sharedSenderWorker{
		dialer:      dialer,
		idleTimeout: idleTimeout,
		wait:        wait,
		now:         now,
		sleep:       sleep,
		requests:    make(chan sharedSendRequest),
		done:        make(chan struct{}),
		exited:      make(chan struct{}),
	}
	go worker.run()
	return worker
}

var errSharedSenderProviderClosed = errors.New("shared sender provider is closed")

func (w *sharedSenderWorker) send(message *gomail.Message, overrideRecipient string) error {
	reply := make(chan error, 1)
	req := sharedSendRequest{
		message:           message,
		overrideRecipient: overrideRecipient,
		reply:             reply,
	}
	select {
	case w.requests <- req:
	case <-w.done:
		return errSharedSenderProviderClosed
	}
	return <-reply
}

func (w *sharedSenderWorker) shutdown() error {
	w.once.Do(func() {
		close(w.done)
	})
	<-w.exited
	return w.closeErr
}

func (w *sharedSenderWorker) run() {
	defer close(w.exited)

	var sender gomail.SendCloser
	var timer *time.Timer
	var idle <-chan time.Time
	var lastDialAttempt time.Time

	stopIdle := func() {
		if timer == nil {
			idle = nil
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		idle = nil
	}

	resetIdle := func() {
		if w.idleTimeout <= 0 || sender == nil {
			idle = nil
			return
		}
		if timer == nil {
			timer = time.NewTimer(w.idleTimeout)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(w.idleTimeout)
		}
		idle = timer.C
	}

	closeSender := func() error {
		if sender == nil {
			return nil
		}
		err := sender.Close()
		sender = nil
		return err
	}

	for {
		select {
		case <-w.done:
			stopIdle()
			if timer != nil {
				timer.Stop()
			}
			w.closeErr = closeSender()
			return

		case <-idle:
			idle = nil
			_ = closeSender()

		case req := <-w.requests:
			stopIdle()

			if sender == nil {
				if w.wait > 0 && !lastDialAttempt.IsZero() {
					elapsed := w.now().Sub(lastDialAttempt)
					if elapsed < w.wait {
						w.sleep(w.wait - elapsed)
					}
				}
				lastDialAttempt = w.now()

				var err error
				sender, err = w.dialer.Dial()
				if err != nil {
					req.reply <- err
					continue
				}
			}

			if req.overrideRecipient != "" {
				req.message.SetHeader("To", req.overrideRecipient)
				req.message.SetHeader("Cc")
				req.message.SetHeader("Bcc")
			}

			err := gomail.Send(sender, req.message)
			if err != nil {
				_ = closeSender()
				req.reply <- err
				continue
			}

			resetIdle()
			req.reply <- nil
		}
	}
}

func sharedSenderIdentity(dialer Dialer) (string, error) {
	if dialer == nil {
		return "", nil
	}
	if provider, ok := dialer.(SharedSenderIdentityProvider); ok {
		identity := provider.SharedSenderIdentity()
		if identity != "" {
			return identity, nil
		}
	}
	if smtpDialer, ok := dialer.(*gomail.Dialer); ok {
		return fmt.Sprintf(
			"smtp:%s:%d:%s:%s:%t:%x:%x:%x",
			smtpDialer.Host,
			smtpDialer.Port,
			smtpDialer.Username,
			smtpDialer.LocalName,
			smtpDialer.SSL,
			sha256.Sum256([]byte(smtpDialer.Password)),
			sha256.Sum256([]byte(fmt.Sprintf("%#v", smtpDialer.TLSConfig))),
			sha256.Sum256([]byte(sharedSenderAuthFingerprint(smtpDialer))),
		), nil
	}
	return "", fmt.Errorf("smtp-shared requires *gomail.Dialer or SharedSenderIdentityProvider, got %T", dialer)
}

func sharedSenderAuthFingerprint(dialer *gomail.Dialer) string {
	if dialer == nil || dialer.Auth == nil {
		return ""
	}
	return fmt.Sprintf("%T:%#v", dialer.Auth, dialer.Auth)
}

func freezeSharedDialer(dialer Dialer) Dialer {
	smtpDialer, ok := dialer.(*gomail.Dialer)
	if !ok || smtpDialer == nil {
		return dialer
	}

	clone := *smtpDialer
	if smtpDialer.TLSConfig != nil {
		clone.TLSConfig = smtpDialer.TLSConfig.Clone()
	}
	return &clone
}

var (
	defaultSharedSenderProviderOnce sync.Once
	defaultSharedSenderProvider     *SharedSenderProvider
)

func DefaultSharedSenderProviderInstance() *SharedSenderProvider {
	defaultSharedSenderProviderOnce.Do(func() {
		defaultSharedSenderProvider = NewSharedSenderProvider(DefaultSharedSenderIdleTimeout)
	})
	return defaultSharedSenderProvider
}

func (p *SharedSenderProvider) workerFor(generation uint64, identity string, dialer Dialer, wait time.Duration, now TimeSource, sleep Sleeper) (*sharedSenderWorker, error) {
	if p == nil {
		return nil, fmt.Errorf("shared sender provider is not configured")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if generation != p.generation {
		return nil, errSharedSenderProviderClosed
	}
	worker := p.workers[identity]
	if worker == nil {
		worker = newSharedSenderWorker(dialer, p.idleTimeout, wait, now, sleep)
		p.workers[identity] = worker
	}
	return worker, nil
}
