package kemail

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/smtp"
	"sync"
	"testing"
	"time"

	"github.com/ccontavalli/enkit/lib/logger"
	"github.com/stretchr/testify/assert"
	"gopkg.in/gomail.v2"
)

type fakeSender struct {
	sendErrors []error
	sendCalls  int
	closed     bool
}

func (s *fakeSender) Send(from string, to []string, msg io.WriterTo) error {
	if s.sendCalls < len(s.sendErrors) {
		err := s.sendErrors[s.sendCalls]
		s.sendCalls++
		return err
	}
	s.sendCalls++
	return nil
}

func (s *fakeSender) Close() error {
	s.closed = true
	return nil
}

type dialResult struct {
	sender gomail.SendCloser
	err    error
}

type fakeDialer struct {
	identity string
	results  []dialResult
	calls    int
	mu       sync.Mutex
}

func (d *fakeDialer) SharedSenderIdentity() string {
	if d.identity != "" {
		return d.identity
	}
	return fmt.Sprintf("fake:%p", d)
}

func (d *fakeDialer) Dial() (gomail.SendCloser, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.calls >= len(d.results) {
		return nil, fmt.Errorf("unexpected dial %d", d.calls+1)
	}
	res := d.results[d.calls]
	d.calls++
	return res.sender, res.err
}

func buildMessage(to string) *gomail.Message {
	m := gomail.NewMessage()
	m.SetHeader("From", "sender@example.com")
	m.SetHeader("To", to)
	m.SetHeader("Subject", "Test")
	m.SetBody("text/plain", "Hello")
	return m
}

func TestSendRetriesDial(t *testing.T) {
	fail := errors.New("dial failed")
	sender := &fakeSender{}
	dialer := &fakeDialer{
		results: []dialResult{
			{err: fail},
			{sender: sender},
		},
	}

	recipients := []string{"a@example.com"}
	err := Send(dialer, recipients, func(r string) (*gomail.Message, error) {
		return buildMessage(r), nil
	}, nil,
		WithLogger(logger.Nil),
		WithWait(0),
		WithSleep(func(time.Duration) {}),
		WithMaxAttempts(3),
	)
	assert.NoError(t, err)
	assert.Equal(t, 2, dialer.calls, "dial attempts")
	assert.Equal(t, 1, sender.sendCalls, "send attempts")
}

func TestSendRetriesSend(t *testing.T) {
	fail := errors.New("send failed")
	sender1 := &fakeSender{sendErrors: []error{fail}}
	sender2 := &fakeSender{}
	dialer := &fakeDialer{
		results: []dialResult{
			{sender: sender1},
			{sender: sender2},
		},
	}

	recipients := []string{"a@example.com"}
	err := Send(dialer, recipients, func(r string) (*gomail.Message, error) {
		return buildMessage(r), nil
	}, func(r string) string {
		return r
	},
		WithLogger(logger.Nil),
		WithWait(0),
		WithSleep(func(time.Duration) {}),
		WithMaxAttempts(3),
	)
	assert.NoError(t, err)
	assert.Equal(t, 2, dialer.calls, "dial attempts")
	assert.True(t, sender1.closed, "sender1 closed")
	assert.Equal(t, 1, sender2.sendCalls, "sender2 send calls")
}

func TestSendShuffle(t *testing.T) {
	sender := &fakeSender{}
	dialer := &fakeDialer{results: []dialResult{{sender: sender}}}

	recipients := []int{1, 2, 3}
	order := []int{}
	err := Send(dialer, recipients, func(r int) (*gomail.Message, error) {
		order = append(order, r)
		return buildMessage("test@example.com"), nil
	}, nil,
		WithLogger(logger.Nil),
		WithWait(0),
		WithSleep(func(time.Duration) {}),
		WithRng(rand.New(rand.NewSource(1))),
		WithShuffle(true),
	)
	assert.NoError(t, err)
	assert.Len(t, order, 3)
	assert.Equal(t, []int{1, 3, 2}, order)
}

func TestSendProgress(t *testing.T) {
	fail := errors.New("send failed")
	sender1 := &fakeSender{sendErrors: []error{fail}}
	sender2 := &fakeSender{}
	dialer := &fakeDialer{
		results: []dialResult{
			{sender: sender1},
			{sender: sender2},
		},
	}

	var reports []Progress
	recipients := []string{"a@example.com"}
	err := Send(dialer, recipients, func(r string) (*gomail.Message, error) {
		return buildMessage(r), nil
	}, func(r string) string {
		return r
	},
		WithLogger(logger.Nil),
		WithWait(0),
		WithSleep(func(time.Duration) {}),
		WithMaxAttempts(3),
		WithProgress(func(p Progress) ProgressAction {
			reports = append(reports, p)
			return ProgressContinue
		}),
	)
	assert.NoError(t, err)
	assert.Len(t, reports, 4)
	assert.Equal(t, ProgressSending, reports[0].Status)
	assert.Equal(t, 1, reports[0].Attempt)
	assert.Equal(t, ProgressError, reports[1].Status)
	assert.Error(t, reports[1].Err)
	assert.Equal(t, ProgressSending, reports[2].Status)
	assert.Equal(t, 2, reports[2].Attempt)
	assert.Equal(t, ProgressSent, reports[3].Status)
	assert.Equal(t, 1, reports[3].Sent)
	assert.Equal(t, 0, reports[3].Remaining)
}

func TestSendProgressSkip(t *testing.T) {
	sender := &fakeSender{}
	dialer := &fakeDialer{results: []dialResult{{sender: sender}}}

	recipients := []string{"a@example.com"}
	err := Send(dialer, recipients, func(r string) (*gomail.Message, error) {
		return buildMessage(r), nil
	}, func(r string) string {
		return r
	},
		WithLogger(logger.Nil),
		WithWait(0),
		WithSleep(func(time.Duration) {}),
		WithProgress(func(p Progress) ProgressAction {
			if p.Status == ProgressSending {
				return ProgressSkip
			}
			return ProgressContinue
		}),
	)
	assert.NoError(t, err)
	assert.Equal(t, 0, sender.sendCalls)
}

func TestSendProgressPause(t *testing.T) {
	sender := &fakeSender{}
	dialer := &fakeDialer{results: []dialResult{{sender: sender}}}

	recipients := []string{"a@example.com"}
	err := Send(dialer, recipients, func(r string) (*gomail.Message, error) {
		return buildMessage(r), nil
	}, func(r string) string {
		return r
	},
		WithLogger(logger.Nil),
		WithWait(0),
		WithSleep(func(time.Duration) {}),
		WithProgress(func(p Progress) ProgressAction {
			if p.Status == ProgressSending {
				return ProgressPause
			}
			return ProgressContinue
		}),
	)
	assert.ErrorIs(t, err, ErrPaused)
	assert.Equal(t, 0, sender.sendCalls)
}

func TestSendProgressCancel(t *testing.T) {
	sender := &fakeSender{}
	dialer := &fakeDialer{results: []dialResult{{sender: sender}}}

	recipients := []string{"a@example.com"}
	err := Send(dialer, recipients, func(r string) (*gomail.Message, error) {
		return buildMessage(r), nil
	}, func(r string) string {
		return r
	},
		WithLogger(logger.Nil),
		WithWait(0),
		WithSleep(func(time.Duration) {}),
		WithProgress(func(p Progress) ProgressAction {
			if p.Status == ProgressSending {
				return ProgressCancel
			}
			return ProgressContinue
		}),
	)
	assert.ErrorIs(t, err, ErrCanceled)
	assert.Equal(t, 0, sender.sendCalls)
}

func TestSenderFactoryFromFlagsFake(t *testing.T) {
	flags := DefaultFlags()
	flags.Sender = "fake"
	flags.FakeDelay = 10 * time.Millisecond

	slept := time.Duration(0)
	factory, err := SenderFactoryFromFlags(nil, flags, logger.Nil, func(d time.Duration) {
		slept = d
	})
	assert.NoError(t, err)

	sender, err := factory.Open()
	assert.NoError(t, err)

	err = sender.Send(buildMessage("test@example.com"))
	assert.NoError(t, err)
	assert.Equal(t, flags.FakeDelay, slept)
	assert.NoError(t, sender.Close())
}

func TestSenderFactoryFromFlagsErrors(t *testing.T) {
	flags := DefaultFlags()
	flags.Sender = "smtp"
	_, err := SenderFactoryFromFlags(nil, flags, logger.Nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "dialer is required")

	flags.Sender = "unknown"
	_, err = SenderFactoryFromFlags(nil, flags, logger.Nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown email sender")
}

func TestSendSharedSenderKeepsRetryDelayOnDialFailure(t *testing.T) {
	fail := errors.New("dial failed")
	sender := &fakeSender{}
	dialer := &fakeDialer{
		results: []dialResult{
			{err: fail},
			{sender: sender},
		},
	}

	now := time.Unix(100, 0)
	sleeps := []time.Duration{}
	provider := NewSharedSenderProvider(time.Hour)
	factory, err := provider.factoryForDialerWithClock(dialer, "", 10*time.Second, func() time.Time { return now }, func(d time.Duration) {
		sleeps = append(sleeps, d)
		now = now.Add(d)
	})
	assert.NoError(t, err)
	if err != nil {
		return
	}
	err = Send(
		dialer,
		[]string{"a@example.com"},
		func(r string) (*gomail.Message, error) { return buildMessage(r), nil },
		nil,
		WithLogger(logger.Nil),
		WithSenderFactory(factory),
		WithPreSendSleep(30*time.Second),
		WithMaxAttempts(2),
		WithTimeSource(func() time.Time { return now }),
		WithSleep(func(d time.Duration) {
			sleeps = append(sleeps, d)
			now = now.Add(d)
		}),
	)
	assert.NoError(t, err)
	assert.Equal(t, []time.Duration{30 * time.Second, 30 * time.Second}, sleeps)
	assert.Equal(t, 2, dialer.calls)
	assert.Equal(t, 1, sender.sendCalls)
}

func TestSendSharedSenderReconnectsImmediatelyAfterStaleSessionSendFailure(t *testing.T) {
	sender1 := &fakeSender{sendErrors: []error{nil, errors.New("stale session")}}
	sender2 := &fakeSender{}
	dialer := &fakeDialer{
		results: []dialResult{
			{sender: sender1},
			{sender: sender2},
		},
	}
	now := time.Unix(100, 0)
	provider := NewSharedSenderProvider(time.Hour)
	sleeps := []time.Duration{}

	flags := DefaultFlags()
	flags.Sender = "smtp-shared"
	flags.Wait = 0

	factory, err := provider.factoryForDialerWithClock(dialer, "", 10*time.Second, func() time.Time { return now }, func(d time.Duration) {
		sleeps = append(sleeps, d)
		now = now.Add(d)
	})
	assert.NoError(t, err)
	if err != nil {
		return
	}

	first, err := factory.Open()
	assert.NoError(t, err)
	if err != nil {
		return
	}
	assert.NoError(t, first.Send(buildMessage("warmup@example.com")))
	assert.NoError(t, first.Close())

	now = now.Add(time.Hour)

	err = Send(
		nil,
		[]string{"a@example.com"},
		func(r string) (*gomail.Message, error) { return buildMessage(r), nil },
		nil,
		WithLogger(logger.Nil),
		WithSenderFactory(factory),
		WithWait(10*time.Second),
		WithTimeSource(func() time.Time { return now }),
		WithSleep(func(d time.Duration) {
			sleeps = append(sleeps, d)
			now = now.Add(d)
		}),
		WithMaxAttempts(2),
	)
	assert.NoError(t, err)
	assert.Equal(t, []time.Duration{10 * time.Second}, sleeps)
	assert.Equal(t, 2, dialer.calls)
	assert.Equal(t, 1, sender2.sendCalls)
}

func TestSendSharedSenderDoesNotHideRetryAttempts(t *testing.T) {
	sender1 := &fakeSender{sendErrors: []error{nil, errors.New("stale session")}}
	sender2 := &fakeSender{}
	dialer := &fakeDialer{
		results: []dialResult{
			{sender: sender1},
			{sender: sender2},
		},
	}
	provider := NewSharedSenderProvider(time.Hour)
	flags := DefaultFlags()
	flags.Sender = "smtp-shared"
	flags.Wait = 0

	factory, err := provider.factoryForDialerWithClock(dialer, "", 0, time.Now, nil)
	assert.NoError(t, err)
	if err != nil {
		return
	}

	first, err := factory.Open()
	assert.NoError(t, err)
	if err != nil {
		return
	}
	assert.NoError(t, first.Send(buildMessage("warmup@example.com")))
	assert.NoError(t, first.Close())

	err = Send(
		nil,
		[]string{"a@example.com"},
		func(r string) (*gomail.Message, error) { return buildMessage(r), nil },
		nil,
		WithLogger(logger.Nil),
		WithSenderFactory(factory),
		WithWait(0),
		WithSleep(func(time.Duration) {}),
		WithMaxAttempts(1),
	)
	assert.Error(t, err)
	assert.Equal(t, 1, dialer.calls)
	assert.Equal(t, 0, sender2.sendCalls)
}

func TestSendSharedSenderPreservesRetryDelayAcrossReusedSession(t *testing.T) {
	sender1 := &fakeSender{sendErrors: []error{nil, errors.New("stale session")}}
	sender2 := &fakeSender{}
	dialer := &fakeDialer{
		results: []dialResult{
			{sender: sender1},
			{sender: sender2},
		},
	}
	now := time.Unix(100, 0)
	provider := NewSharedSenderProvider(time.Hour)
	sleeps := []time.Duration{}

	factory, err := provider.factoryForDialerWithClock(dialer, "", 10*time.Second, func() time.Time { return now }, func(d time.Duration) {
		sleeps = append(sleeps, d)
		now = now.Add(d)
	})
	assert.NoError(t, err)
	if err != nil {
		return
	}

	first, err := factory.Open()
	assert.NoError(t, err)
	if err != nil {
		return
	}
	assert.NoError(t, first.Send(buildMessage("warmup@example.com")))
	assert.NoError(t, first.Close())

	now = now.Add(3 * time.Second)

	err = Send(
		nil,
		[]string{"a@example.com"},
		func(r string) (*gomail.Message, error) { return buildMessage(r), nil },
		nil,
		WithLogger(logger.Nil),
		WithSenderFactory(factory),
		WithWait(10*time.Second),
		WithTimeSource(func() time.Time { return now }),
		WithSleep(func(d time.Duration) {
			sleeps = append(sleeps, d)
			now = now.Add(d)
		}),
		WithMaxAttempts(2),
	)
	assert.NoError(t, err)
	assert.Equal(t, []time.Duration{10 * time.Second}, sleeps)
	assert.Equal(t, 2, dialer.calls)
	assert.Equal(t, 1, sender2.sendCalls)
}

func TestSharedSenderFactoryFromFlagsReusesSMTPConnection(t *testing.T) {
	firstSendStarted := make(chan struct{})
	releaseFirstSend := make(chan struct{})
	sender := &blockingFakeSender{
		beforeSend: func(call int) {
			if call != 1 {
				return
			}
			close(firstSendStarted)
			<-releaseFirstSend
		},
	}
	dialer := &fakeDialer{results: []dialResult{{sender: sender}}}
	provider := NewSharedSenderProvider(time.Hour)
	flags := DefaultFlags()
	flags.Sender = "smtp-shared"
	flags.Wait = 0

	factory, err := SharedSenderFactoryFromFlags(func() *SharedSenderProvider { return provider }, dialer, flags, logger.Nil, nil)
	assert.NoError(t, err)

	var wg sync.WaitGroup
	for _, recipient := range []string{"a@example.com", "b@example.com"} {
		wg.Add(1)
		go func(recipient string) {
			defer wg.Done()
			single, err := factory.Open()
			assert.NoError(t, err)
			defer func() {
				assert.NoError(t, single.Close())
			}()
			assert.NoError(t, single.Send(buildMessage(recipient)))
		}(recipient)
	}

	<-firstSendStarted
	close(releaseFirstSend)
	wg.Wait()

	assert.Equal(t, 1, dialer.calls)
	assert.Equal(t, 2, sender.sendCalls)
}

func TestSharedSenderFactoryFromFlagsOpenDoesNotDial(t *testing.T) {
	sender := &fakeSender{}
	dialer := &fakeDialer{results: []dialResult{{sender: sender}}}
	provider := NewSharedSenderProvider(time.Hour)
	flags := DefaultFlags()
	flags.Sender = "smtp-shared"
	flags.Wait = 0

	factory, err := SharedSenderFactoryFromFlags(func() *SharedSenderProvider { return provider }, dialer, flags, logger.Nil, nil)
	assert.NoError(t, err)
	if err != nil {
		return
	}

	single, err := factory.Open()
	assert.NoError(t, err)
	if err != nil {
		return
	}
	assert.Equal(t, 0, dialer.calls)
	assert.NoError(t, single.Send(buildMessage("a@example.com")))
	assert.Equal(t, 1, dialer.calls)
	assert.Equal(t, 1, sender.sendCalls)
	assert.NoError(t, single.Close())
}

func TestSharedSenderFactoryFromFlagsClosesIdleConnection(t *testing.T) {
	sender1 := &fakeSender{}
	sender2 := &fakeSender{}
	dialer := &fakeDialer{
		results: []dialResult{
			{sender: sender1},
			{sender: sender2},
		},
	}
	provider := NewSharedSenderProvider(20 * time.Millisecond)
	flags := DefaultFlags()
	flags.Sender = "smtp-shared"
	flags.Wait = 0

	factory, err := SharedSenderFactoryFromFlags(func() *SharedSenderProvider { return provider }, dialer, flags, logger.Nil, nil)
	assert.NoError(t, err)

	single, err := factory.Open()
	assert.NoError(t, err)
	assert.NoError(t, single.Send(buildMessage("a@example.com")))
	assert.NoError(t, single.Close())
	assert.Eventually(t, func() bool { return sender1.closed }, 500*time.Millisecond, 10*time.Millisecond)

	single, err = factory.Open()
	assert.NoError(t, err)
	assert.NoError(t, single.Send(buildMessage("b@example.com")))
	assert.Equal(t, 2, dialer.calls)
	assert.Equal(t, 1, sender1.sendCalls)
	assert.Equal(t, 1, sender2.sendCalls)
	assert.NoError(t, single.Close())
	assert.Eventually(t, func() bool { return sender2.closed }, 500*time.Millisecond, 10*time.Millisecond)
}

func TestSharedSenderFactoryFromFlagsAllowsIdleCloseWithCheckedOutHandle(t *testing.T) {
	sender1 := &fakeSender{}
	sender2 := &fakeSender{}
	dialer := &fakeDialer{
		results: []dialResult{
			{sender: sender1},
			{sender: sender2},
		},
	}
	provider := NewSharedSenderProvider(20 * time.Millisecond)
	flags := DefaultFlags()
	flags.Sender = "smtp-shared"
	flags.Wait = 0

	factory, err := SharedSenderFactoryFromFlags(func() *SharedSenderProvider { return provider }, dialer, flags, logger.Nil, nil)
	assert.NoError(t, err)

	single, err := factory.Open()
	assert.NoError(t, err)
	assert.NoError(t, single.Send(buildMessage("a@example.com")))

	time.Sleep(50 * time.Millisecond)

	assert.True(t, sender1.closed)
	assert.NoError(t, single.Send(buildMessage("b@example.com")))
	assert.Equal(t, 2, dialer.calls)
	assert.Equal(t, 1, sender1.sendCalls)
	assert.Equal(t, 1, sender2.sendCalls)
	assert.NoError(t, single.Close())
}

func TestSharedSenderFactoryFromFlagsReopenWithoutSendDoesNotAffectIdleTimer(t *testing.T) {
	sender1 := &fakeSender{}
	sender2 := &fakeSender{}
	dialer := &fakeDialer{
		results: []dialResult{
			{sender: sender1},
			{sender: sender2},
		},
	}
	provider := NewSharedSenderProvider(20 * time.Millisecond)
	flags := DefaultFlags()
	flags.Sender = "smtp-shared"
	flags.Wait = 0

	factory, err := SharedSenderFactoryFromFlags(func() *SharedSenderProvider { return provider }, dialer, flags, logger.Nil, nil)
	assert.NoError(t, err)

	first, err := factory.Open()
	assert.NoError(t, err)
	assert.NoError(t, first.Send(buildMessage("a@example.com")))
	assert.NoError(t, first.Close())

	time.Sleep(15 * time.Millisecond)

	second, err := factory.Open()
	assert.NoError(t, err)
	assert.NoError(t, second.Close())

	time.Sleep(10 * time.Millisecond)

	third, err := factory.Open()
	assert.NoError(t, err)
	assert.NoError(t, third.Send(buildMessage("b@example.com")))
	assert.Equal(t, 2, dialer.calls)
	assert.Equal(t, 1, sender1.sendCalls)
	assert.Equal(t, 1, sender2.sendCalls)
	assert.NoError(t, third.Close())
}

func TestSharedSenderFactoryFromFlagsReconnectsCheckedOutSenders(t *testing.T) {
	sender1 := &fakeSender{sendErrors: []error{errors.New("stale session")}}
	sender2 := &fakeSender{}
	dialer := &fakeDialer{
		results: []dialResult{
			{sender: sender1},
			{sender: sender2},
		},
	}
	provider := NewSharedSenderProvider(time.Hour)
	flags := DefaultFlags()
	flags.Sender = "smtp-shared"
	flags.Wait = 0

	factory, err := SharedSenderFactoryFromFlags(func() *SharedSenderProvider { return provider }, dialer, flags, logger.Nil, nil)
	assert.NoError(t, err)

	first, err := factory.Open()
	assert.NoError(t, err)
	second, err := factory.Open()
	assert.NoError(t, err)

	err = first.Send(buildMessage("a@example.com"))
	assert.Error(t, err)

	err = second.Send(buildMessage("b@example.com"))
	assert.NoError(t, err)
	assert.Equal(t, 2, dialer.calls)
	assert.Equal(t, 1, sender2.sendCalls)

	assert.NoError(t, first.Close())
	assert.NoError(t, second.Close())
}

func TestSharedSenderProviderCloseDetachesOldPools(t *testing.T) {
	sender1 := &fakeSender{}
	sender2 := &fakeSender{}
	dialer := &fakeDialer{
		results: []dialResult{
			{sender: sender1},
			{sender: sender2},
		},
	}
	provider := NewSharedSenderProvider(time.Hour)
	flags := DefaultFlags()
	flags.Sender = "smtp-shared"
	flags.Wait = 0

	factory1, err := SharedSenderFactoryFromFlags(func() *SharedSenderProvider { return provider }, dialer, flags, logger.Nil, nil)
	assert.NoError(t, err)

	single1, err := factory1.Open()
	assert.NoError(t, err)
	assert.NoError(t, single1.Send(buildMessage("a@example.com")))

	assert.NoError(t, provider.Close())
	assert.True(t, sender1.closed)

	factory2, err := SharedSenderFactoryFromFlags(func() *SharedSenderProvider { return provider }, dialer, flags, logger.Nil, nil)
	assert.NoError(t, err)
	_, err = factory1.Open()
	assert.Error(t, err)

	single2, err := factory2.Open()
	assert.NoError(t, err)
	assert.NoError(t, single1.Close())
	assert.Error(t, single1.Send(buildMessage("stale@example.com")))
	assert.NoError(t, single2.Send(buildMessage("b@example.com")))
	assert.Equal(t, 2, dialer.calls)
	assert.Equal(t, 1, sender2.sendCalls)
	assert.NoError(t, single2.Close())
}

func TestSharedSenderProviderCloseReturnsQueuedSendResult(t *testing.T) {
	sendStarted := make(chan struct{})
	releaseSend := make(chan struct{})
	sender := &blockingFakeSender{
		beforeSend: func(call int) {
			if call != 1 {
				return
			}
			close(sendStarted)
			<-releaseSend
		},
	}
	dialer := &fakeDialer{results: []dialResult{{sender: sender}}}
	provider := NewSharedSenderProvider(time.Hour)
	flags := DefaultFlags()
	flags.Sender = "smtp-shared"
	flags.Wait = 0

	factory, err := SharedSenderFactoryFromFlags(func() *SharedSenderProvider { return provider }, dialer, flags, logger.Nil, nil)
	assert.NoError(t, err)
	if err != nil {
		return
	}

	single, err := factory.Open()
	assert.NoError(t, err)
	if err != nil {
		return
	}

	sendDone := make(chan error, 1)
	go func() {
		sendDone <- single.Send(buildMessage("queued@example.com"))
	}()

	<-sendStarted

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- provider.Close()
	}()

	close(releaseSend)

	select {
	case err := <-sendDone:
		assert.NoError(t, err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("queued shared send did not return")
	}

	select {
	case err := <-closeDone:
		assert.NoError(t, err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("provider close did not return")
	}
}

func TestSharedSenderProviderCloseMakesStaleHandleFailFast(t *testing.T) {
	sender := &fakeSender{}
	dialer := &fakeDialer{results: []dialResult{{sender: sender}}}
	provider := NewSharedSenderProvider(time.Hour)
	flags := DefaultFlags()
	flags.Sender = "smtp-shared"
	flags.Wait = 0

	factory, err := SharedSenderFactoryFromFlags(func() *SharedSenderProvider { return provider }, dialer, flags, logger.Nil, nil)
	assert.NoError(t, err)
	if err != nil {
		return
	}

	single, err := factory.Open()
	assert.NoError(t, err)
	if err != nil {
		return
	}

	assert.NoError(t, provider.Close())

	done := make(chan error, 1)
	go func() {
		done <- single.Send(buildMessage("after-close@example.com"))
	}()

	select {
	case err := <-done:
		assert.ErrorIs(t, err, errSharedSenderProviderClosed)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("stale shared sender handle blocked after provider close")
	}
}

type unsupportedSharedDialer struct{}

func (d *unsupportedSharedDialer) Dial() (gomail.SendCloser, error) {
	return &fakeSender{}, nil
}

func TestSharedSenderFactoryFromFlagsRequiresStableIdentity(t *testing.T) {
	provider := NewSharedSenderProvider(time.Hour)
	flags := DefaultFlags()
	flags.Sender = "smtp-shared"

	factory, err := SharedSenderFactoryFromFlags(func() *SharedSenderProvider { return provider }, &unsupportedSharedDialer{}, flags, logger.Nil, nil)
	assert.Nil(t, factory)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "SharedSenderIdentityProvider")
}

func TestSharedSenderIdentityIncludesSMTPSettings(t *testing.T) {
	base := gomail.NewPlainDialer("smtp.example.com", 587, "user", "password-one")
	base.LocalName = "local"

	differentPassword := gomail.NewPlainDialer("smtp.example.com", 587, "user", "password-two")
	differentPassword.LocalName = "local"

	differentTLS := gomail.NewPlainDialer("smtp.example.com", 587, "user", "password-one")
	differentTLS.LocalName = "local"
	differentTLS.TLSConfig = &tls.Config{ServerName: "smtp.example.com"}

	differentSSL := gomail.NewPlainDialer("smtp.example.com", 587, "user", "password-one")
	differentSSL.LocalName = "local"
	differentSSL.SSL = true

	withExplicitCram := gomail.NewPlainDialer("smtp.example.com", 587, "user", "password-one")
	withExplicitCram.LocalName = "local"
	withExplicitCram.Auth = smtp.CRAMMD5Auth("user", "password-one")

	withExplicitPlainIdentity := gomail.NewPlainDialer("smtp.example.com", 587, "user", "password-one")
	withExplicitPlainIdentity.LocalName = "local"
	withExplicitPlainIdentity.Auth = smtp.PlainAuth("identity", "user", "password-one", "smtp.example.com")

	baseID, err := sharedSenderIdentity(base)
	assert.NoError(t, err)
	differentPasswordID, err := sharedSenderIdentity(differentPassword)
	assert.NoError(t, err)
	differentTLSID, err := sharedSenderIdentity(differentTLS)
	assert.NoError(t, err)
	differentSSLID, err := sharedSenderIdentity(differentSSL)
	assert.NoError(t, err)
	withExplicitCramID, err := sharedSenderIdentity(withExplicitCram)
	assert.NoError(t, err)
	withExplicitPlainIdentityID, err := sharedSenderIdentity(withExplicitPlainIdentity)
	assert.NoError(t, err)

	assert.NotEqual(t, baseID, differentPasswordID)
	assert.NotEqual(t, baseID, differentTLSID)
	assert.NotEqual(t, baseID, differentSSLID)
	assert.NotEqual(t, baseID, withExplicitCramID)
	assert.NotEqual(t, baseID, withExplicitPlainIdentityID)
}

func TestSharedSenderFactorySeparatesWorkersByWait(t *testing.T) {
	sender1 := &fakeSender{}
	sender2 := &fakeSender{}
	dialer := &fakeDialer{
		results: []dialResult{
			{sender: sender1},
			{sender: sender2},
		},
	}
	provider := NewSharedSenderProvider(time.Hour)

	factory1, err := provider.factoryForDialerWithClock(dialer, "", 5*time.Second, time.Now, nil)
	assert.NoError(t, err)
	if err != nil {
		return
	}
	factory2, err := provider.factoryForDialerWithClock(dialer, "", 10*time.Second, time.Now, nil)
	assert.NoError(t, err)
	if err != nil {
		return
	}

	single1, err := factory1.Open()
	assert.NoError(t, err)
	if err != nil {
		return
	}
	assert.NoError(t, single1.Send(buildMessage("a@example.com")))

	single2, err := factory2.Open()
	assert.NoError(t, err)
	if err != nil {
		return
	}
	assert.NoError(t, single2.Send(buildMessage("b@example.com")))

	assert.Equal(t, 2, dialer.calls)
	assert.Equal(t, 1, sender1.sendCalls)
	assert.Equal(t, 1, sender2.sendCalls)
	assert.NoError(t, single1.Close())
	assert.NoError(t, single2.Close())
}

func TestSharedSenderFactorySnapshotsGomailDialer(t *testing.T) {
	dialer := gomail.NewPlainDialer("smtp.example.com", 587, "user", "password-one")
	dialer.LocalName = "local"
	dialer.TLSConfig = &tls.Config{ServerName: "smtp.example.com"}

	provider := NewSharedSenderProvider(time.Hour)
	factory, err := provider.factoryForDialerWithClock(dialer, "", 0, time.Now, nil)
	assert.NoError(t, err)

	sharedFactory, ok := factory.(*sharedSenderFactory)
	assert.True(t, ok)
	frozen, ok := sharedFactory.dialer.(*gomail.Dialer)
	assert.True(t, ok)
	assert.NotSame(t, dialer, frozen)
	assert.NotSame(t, dialer.TLSConfig, frozen.TLSConfig)

	dialer.Host = "changed.example.com"
	dialer.Password = "password-two"
	dialer.LocalName = "changed"
	dialer.SSL = true
	dialer.TLSConfig.ServerName = "changed.example.com"

	assert.Equal(t, "smtp.example.com", frozen.Host)
	assert.Equal(t, "password-one", frozen.Password)
	assert.Equal(t, "local", frozen.LocalName)
	assert.False(t, frozen.SSL)
	assert.Equal(t, "smtp.example.com", frozen.TLSConfig.ServerName)
}

func TestSharedSenderFactoryFromFlagsDoesNotBlockOtherIdentitiesOnSlowDial(t *testing.T) {
	slowStart := make(chan struct{})
	releaseSlow := make(chan struct{})
	slowDialer := &blockingDialer{
		identity: "slow",
		start:    slowStart,
		release:  releaseSlow,
		sender:   &fakeSender{},
	}
	fastDialer := &blockingDialer{
		identity: "fast",
		sender:   &fakeSender{},
	}
	provider := NewSharedSenderProvider(time.Hour)
	flags := DefaultFlags()
	flags.Sender = "smtp-shared"

	slowFactory, err := SharedSenderFactoryFromFlags(func() *SharedSenderProvider { return provider }, slowDialer, flags, logger.Nil, nil)
	assert.NoError(t, err)
	fastFactory, err := SharedSenderFactoryFromFlags(func() *SharedSenderProvider { return provider }, fastDialer, flags, logger.Nil, nil)
	assert.NoError(t, err)

	slowDone := make(chan struct{})
	go func() {
		defer close(slowDone)
		sender, err := slowFactory.Open()
		assert.NoError(t, err)
		if err == nil {
			assert.NoError(t, sender.Send(buildMessage("slow@example.com")))
			assert.NoError(t, sender.Close())
		}
	}()

	<-slowStart

	fastDone := make(chan struct{})
	go func() {
		defer close(fastDone)
		sender, err := fastFactory.Open()
		assert.NoError(t, err)
		if err == nil {
			assert.NoError(t, sender.Send(buildMessage("fast@example.com")))
			assert.NoError(t, sender.Close())
		}
	}()

	select {
	case <-fastDone:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("fast shared sender send blocked on unrelated slow dial")
	}

	close(releaseSlow)
	<-slowDone
}

type blockingFakeSender struct {
	fakeSender
	beforeSend func(call int)
}

func (s *blockingFakeSender) Send(from string, to []string, msg io.WriterTo) error {
	nextCall := s.sendCalls + 1
	if s.beforeSend != nil {
		s.beforeSend(nextCall)
	}
	return s.fakeSender.Send(from, to, msg)
}

type blockingDialer struct {
	identity string
	start    chan struct{}
	release  chan struct{}
	sender   gomail.SendCloser
}

func (d *blockingDialer) SharedSenderIdentity() string {
	return d.identity
}

func (d *blockingDialer) Dial() (gomail.SendCloser, error) {
	if d.start != nil {
		select {
		case <-d.start:
		default:
			close(d.start)
		}
	}
	if d.release != nil {
		<-d.release
	}
	return d.sender, nil
}
