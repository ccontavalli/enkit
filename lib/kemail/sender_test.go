package kemail

import (
	"errors"
	"io"
	"math/rand"
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
	results []dialResult
	calls   int
}

func (d *fakeDialer) Dial() (gomail.SendCloser, error) {
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

func TestWaitForRetrySleeps(t *testing.T) {
	now := time.Unix(100, 0)
	slept := time.Duration(0)
	current := func() time.Time {
		return now
	}
	sleep := func(d time.Duration) {
		slept = d
		now = now.Add(d)
	}
	last := now.Add(-5 * time.Second)
	returned := waitForRetry(last, 10*time.Second, current, sleep, logger.Nil)
	assert.Equal(t, 5*time.Second, slept)
	assert.True(t, returned.Equal(time.Unix(105, 0)), "expected time to advance")
}

func TestWaitForRetryImmediate(t *testing.T) {
	now := time.Unix(200, 0)
	current := func() time.Time {
		return now
	}
	returned := waitForRetry(now.Add(-15*time.Second), 10*time.Second, current, func(time.Duration) {}, logger.Nil)
	assert.True(t, returned.Equal(now), "expected no sleep")
}
