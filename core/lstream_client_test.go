package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestClampDecreasedTimestampPreservesOriginal verifies that ordering uses the
// preceding timestamp without losing the timestamp that appeared in the log.
func TestClampDecreasedTimestampPreservesOriginal(t *testing.T) {
	lastTime := time.Date(2026, 9, 19, 12, 5, 0, 0, time.UTC)
	origTime := lastTime.Add(-20 * time.Minute)
	msg := LogMsg{Time: origTime}

	clampDecreasedTimestamp(&msg, lastTime)

	assert.Equal(t, lastTime, msg.Time)
	assert.Equal(t, origTime, msg.OrigDecreasedTime)
}

// TestClampDecreasedTimestampLeavesOrderedTimeAlone verifies that ordinary
// records do not acquire an original-decreased timestamp.
func TestClampDecreasedTimestampLeavesOrderedTimeAlone(t *testing.T) {
	lastTime := time.Date(2026, 9, 19, 12, 5, 0, 0, time.UTC)
	wantTime := lastTime.Add(time.Minute)
	msg := LogMsg{Time: wantTime}

	clampDecreasedTimestamp(&msg, lastTime)

	assert.Equal(t, wantTime, msg.Time)
	assert.True(t, msg.OrigDecreasedTime.IsZero())
}
