package retry

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDelaySinceUsesConfiguredTimeSource(t *testing.T) {
	now := time.Unix(100, 0)
	r := New(
		WithWait(10*time.Second),
		WithFuzzy(0),
		WithTimeSource(func() time.Time { return now }),
	)

	delay := r.DelaySince(now.Add(-4 * time.Second))
	assert.Equal(t, 6*time.Second, delay)
}

func TestRunAttemptUsesConfiguredSleep(t *testing.T) {
	now := time.Unix(200, 0)
	sleeps := []time.Duration{}
	attempts := 0
	r := New(
		WithAttempts(2),
		WithWait(5*time.Second),
		WithFuzzy(0),
		WithTimeSource(func() time.Time { return now }),
		WithSleep(func(d time.Duration) {
			sleeps = append(sleeps, d)
			now = now.Add(d)
		}),
	)

	err := r.RunAttempt(func(attempt int) error {
		attempts++
		if attempt == 0 {
			return errors.New("try again")
		}
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, 2, attempts)
	assert.Equal(t, []time.Duration{5 * time.Second}, sleeps)
}
