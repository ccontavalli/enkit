package kemail

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ccontavalli/enkit/lib/logger"
	"gopkg.in/gomail.v2"
)

// DefaultSharedSenderIdleTimeout controls how long an SMTP session stays open
// after the last shared sender activity.
const DefaultSharedSenderIdleTimeout = 3 * time.Minute

// SharedProviderFunc returns the shared provider to use for smtp-shared mode.
type SharedProviderFunc func() *SharedSenderProvider

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
	nextID     uint64
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

func (p *SharedSenderProvider) factoryForDialer(dialer Dialer, overrideRecipient string, wait, idleTimeout time.Duration, sleep Sleeper, log logger.Logger) (SingleSenderFactory, error) {
	return p.factoryForDialerWithClockAndLogger(dialer, overrideRecipient, wait, idleTimeout, time.Now, sleep, log)
}

func (p *SharedSenderProvider) factoryForDialerWithClock(dialer Dialer, overrideRecipient string, wait time.Duration, now TimeSource, sleep Sleeper) (SingleSenderFactory, error) {
	return p.factoryForDialerWithClockAndLogger(dialer, overrideRecipient, wait, 0, now, sleep, logger.Nil)
}

func (p *SharedSenderProvider) factoryForDialerWithClockAndLogger(dialer Dialer, overrideRecipient string, wait, idleTimeout time.Duration, now TimeSource, sleep Sleeper, log logger.Logger) (SingleSenderFactory, error) {
	if dialer == nil {
		return nil, fmt.Errorf("dialer is required for smtp sender")
	}
	if log == nil {
		log = logger.Nil
	}

	identity, err := sharedSenderIdentity(dialer)
	if err != nil {
		return nil, err
	}
	logID := dialer.LogID()
	if logID == "" {
		return nil, fmt.Errorf("smtp-shared requires Dialer.LogID() to return a non-empty value")
	}
	effectiveIdleTimeout := p.effectiveIdleTimeout(idleTimeout)

	p.mu.Lock()
	generation := p.generation
	p.mu.Unlock()

	return &sharedSenderFactory{
		provider:          p,
		generation:        generation,
		identity:          fmt.Sprintf("%s|wait=%d|idle-timeout=%d", identity, wait, effectiveIdleTimeout),
		dialer:            dialer,
		wait:              wait,
		idleTimeout:       effectiveIdleTimeout,
		now:               now,
		sleep:             sleep,
		log:               log,
		overrideRecipient: overrideRecipient,
	}, nil
}

type sharedSenderFactory struct {
	provider          *SharedSenderProvider
	generation        uint64
	identity          string
	dialer            Dialer
	wait              time.Duration
	idleTimeout       time.Duration
	now               TimeSource
	sleep             Sleeper
	log               logger.Logger
	overrideRecipient string
}

func (f *sharedSenderFactory) Open() (SingleSender, error) {
	if f.provider == nil {
		return nil, fmt.Errorf("shared sender provider is not configured")
	}

	worker, err := f.provider.workerFor(f.generation, f.identity, f.dialer, f.wait, f.idleTimeout, f.now, f.sleep, f.log)
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
	id          uint64
	dialer      Dialer
	idleTimeout time.Duration
	wait        time.Duration
	now         TimeSource
	sleep       Sleeper
	log         logger.Logger

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

func newSharedSenderWorker(id uint64, dialer Dialer, idleTimeout, wait time.Duration, now TimeSource, sleep Sleeper, log logger.Logger) *sharedSenderWorker {
	if now == nil {
		now = time.Now
	}
	if sleep == nil {
		sleep = time.Sleep
	}
	if log == nil {
		log = logger.Nil
	}
	worker := &sharedSenderWorker{
		id:          id,
		dialer:      dialer,
		idleTimeout: idleTimeout,
		wait:        wait,
		now:         now,
		sleep:       sleep,
		log:         log,
		requests:    make(chan sharedSendRequest),
		done:        make(chan struct{}),
		exited:      make(chan struct{}),
	}
	go worker.run()
	return worker
}

var errSharedSenderProviderClosed = errors.New("shared sender provider is closed")

func (w *sharedSenderWorker) infof(format string, args ...interface{}) {
	args = append([]interface{}{w.id}, args...)
	w.log.Infof("smtp-shared worker[%d] "+format, args...)
}

func (w *sharedSenderWorker) debugf(format string, args ...interface{}) {
	args = append([]interface{}{w.id}, args...)
	w.log.Debugf("smtp-shared worker[%d] "+format, args...)
}

func (w *sharedSenderWorker) warnf(format string, args ...interface{}) {
	args = append([]interface{}{w.id}, args...)
	w.log.Warnf("smtp-shared worker[%d] "+format, args...)
}

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
	w.infof("started - idle-timeout=%s wait=%s dialer=%s", w.idleTimeout, w.wait, w.dialer.LogID())
	defer w.infof("stopped")

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
			w.infof("shutdown requested")
			stopIdle()
			if timer != nil {
				timer.Stop()
			}
			w.closeErr = closeSender()
			if w.closeErr != nil {
				w.warnf("failed closing SMTP session during shutdown: %v", w.closeErr)
			}
			return

		case <-idle:
			idle = nil
			w.infof("idle timeout reached, closing SMTP session")
			if err := closeSender(); err != nil {
				w.warnf("failed closing idle SMTP session: %v", err)
			}

		case req := <-w.requests:
			stopIdle()
			w.infof("send requested")

			if sender == nil {
				if w.wait > 0 && !lastDialAttempt.IsZero() {
					elapsed := w.now().Sub(lastDialAttempt)
					if elapsed < w.wait {
						w.infof("waiting %s before redial", w.wait-elapsed)
						w.sleep(w.wait - elapsed)
					}
				}
				lastDialAttempt = w.now()
				w.infof("dialing SMTP server")

				var err error
				sender, err = w.dialer.Dial()
				if err != nil {
					w.warnf("dial failed: %v", err)
					req.reply <- err
					continue
				}
				w.infof("dial succeeded")
			}

			if req.overrideRecipient != "" {
				w.debugf("overriding recipients for SMTP send")
				req.message.SetHeader("To", req.overrideRecipient)
				req.message.SetHeader("Cc")
				req.message.SetHeader("Bcc")
			}

			w.infof("sending message")
			err := gomail.Send(sender, req.message)
			if err != nil {
				w.warnf("send failed: %v", err)
				if closeErr := closeSender(); closeErr != nil {
					w.warnf("failed closing SMTP session after send error: %v", closeErr)
				}
				req.reply <- err
				continue
			}

			resetIdle()
			w.infof("send completed")
			req.reply <- nil
		}
	}
}

func sharedSenderIdentity(dialer Dialer) (string, error) {
	if dialer == nil {
		return "", fmt.Errorf("dialer is required for smtp sender")
	}
	identity := dialer.Identity()
	if identity == "" {
		return "", fmt.Errorf("smtp-shared requires Dialer.Identity() to return a non-empty value")
	}
	return identity, nil
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

func (p *SharedSenderProvider) effectiveIdleTimeout(idleTimeout time.Duration) time.Duration {
	if idleTimeout > 0 {
		return idleTimeout
	}
	if p == nil || p.idleTimeout <= 0 {
		return DefaultSharedSenderIdleTimeout
	}
	return p.idleTimeout
}

func (p *SharedSenderProvider) workerFor(generation uint64, identity string, dialer Dialer, wait, idleTimeout time.Duration, now TimeSource, sleep Sleeper, log logger.Logger) (*sharedSenderWorker, error) {
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
		p.nextID++
		worker = newSharedSenderWorker(p.nextID, dialer, idleTimeout, wait, now, sleep, log)
		p.workers[identity] = worker
	}
	return worker, nil
}
