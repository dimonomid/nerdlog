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

// TestMakeMalformedLogMsgPreservesParsedTimestamp verifies that a failure
// after timestamp parsing keeps that timestamp while restoring the raw text.
func TestMakeMalformedLogMsgPreservesParsedTimestamp(t *testing.T) {
	parsedTime := time.Date(2026, 9, 19, 10, 2, 3, 0, time.UTC)
	raw := LogMsg{
		LogFilename:        SpecialFilenameJournalctl,
		LogLinenumber:      7,
		CombinedLinenumber: 7,
		Msg:                "raw journal record",
		OrigLine:           "raw journal record",
	}

	got := makeMalformedLogMsg(raw, parsedTime, parsedTime.Add(-time.Second), "localhost")

	assert.True(t, got.Malformed)
	assert.Equal(t, parsedTime, got.Time)
	assert.Equal(t, "raw journal record", got.Msg)
	assert.Equal(t, "raw journal record", got.OrigLine)
	assert.Equal(t, map[string]string{"lstream": "localhost"}, got.Context)
}
