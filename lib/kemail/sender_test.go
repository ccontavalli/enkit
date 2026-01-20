package kemail

import (
	"errors"
	"io"
	"math/rand"
	"testing"
	"time"

	"github.com/ccontavalli/enkit/lib/logger"
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
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if dialer.calls != 2 {
		t.Fatalf("expected 2 dial attempts, got %d", dialer.calls)
	}
	if sender.sendCalls != 1 {
		t.Fatalf("expected 1 send attempt, got %d", sender.sendCalls)
	}
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
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if dialer.calls != 2 {
		t.Fatalf("expected 2 dial attempts, got %d", dialer.calls)
	}
	if !sender1.closed {
		t.Fatalf("expected sender1 to be closed")
	}
	if sender2.sendCalls != 1 {
		t.Fatalf("expected sender2 to send once, got %d", sender2.sendCalls)
	}
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
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if len(order) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(order))
	}
	if order[0] != 1 || order[1] != 3 || order[2] != 2 {
		t.Fatalf("expected shuffled order [1 3 2], got %v", order)
	}
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
	if slept != 5*time.Second {
		t.Fatalf("expected sleep of 5s, got %s", slept)
	}
	if !returned.Equal(time.Unix(105, 0)) {
		t.Fatalf("expected time to advance to 105, got %s", returned)
	}
}

func TestWaitForRetryImmediate(t *testing.T) {
	now := time.Unix(200, 0)
	current := func() time.Time {
		return now
	}
	returned := waitForRetry(now.Add(-15*time.Second), 10*time.Second, current, func(time.Duration) {}, logger.Nil)
	if !returned.Equal(now) {
		t.Fatalf("expected no sleep, got %s", returned)
	}
}
