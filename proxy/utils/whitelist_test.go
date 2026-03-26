package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReplaceableWhitelist(t *testing.T) {
	rw := NewReplaceableWhitelist()
	assert.Equal(t, VerdictDrop, rw.Allow("tcp", "10.0.0.1:22", nil))

	initial, err := NewPatternList([]string{"tcp|10.0.0.1:22"})
	assert.NoError(t, err)
	rw.Set(initial)
	assert.Equal(t, VerdictAllow, rw.Allow("tcp", "10.0.0.1:22", nil))
	assert.Equal(t, VerdictDrop, rw.Allow("tcp", "10.0.0.2:22", nil))

	updated, err := NewPatternList([]string{"tcp|10.0.0.2:22"})
	assert.NoError(t, err)
	rw.Set(updated)
	assert.Equal(t, VerdictDrop, rw.Allow("tcp", "10.0.0.1:22", nil))
	assert.Equal(t, VerdictAllow, rw.Allow("tcp", "10.0.0.2:22", nil))
}
