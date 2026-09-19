package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestAnchorUnresolvedMalformedPrefix verifies that re-anchoring touches only
// the unresolved malformed records at the existing page boundary.
func TestAnchorUnresolvedMalformedPrefix(t *testing.T) {
	t1 := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Minute)
	logs := []LogMsg{
		{Malformed: true, Msg: "malformed 1"},
		{Malformed: true, Msg: "malformed 2"},
		{Time: t2, Msg: "valid"},
		{Time: t2, Malformed: true, Msg: "already anchored"},
	}

	anchorUnresolvedMalformedPrefix(logs, t1)

	assert.Equal(t, []time.Time{t1, t1, t2, t2}, []time.Time{
		logs[0].Time, logs[1].Time, logs[2].Time, logs[3].Time,
	})
	assert.Equal(t, []string{"malformed 1", "malformed 2", "valid", "already anchored"}, []string{
		logs[0].Msg, logs[1].Msg, logs[2].Msg, logs[3].Msg,
	})
}

// TestAnchorUnresolvedMalformedPrefixAfterLoadingEarlier verifies that unresolved
// records are re-anchored when an earlier page supplies their preceding time.
func TestAnchorUnresolvedMalformedPrefixAfterLoadingEarlier(t *testing.T) {
	validTime := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	logs := []LogMsg{
		{Malformed: true, Msg: "malformed 1", CombinedLinenumber: 2},
		{Malformed: true, Msg: "malformed 2", CombinedLinenumber: 3},
	}

	assert.True(t, logs[0].Time.IsZero())
	assert.True(t, logs[1].Time.IsZero())

	anchorUnresolvedMalformedPrefix(logs, validTime)
	logs = append([]LogMsg{{Time: validTime, Msg: "valid", CombinedLinenumber: 1}}, logs...)

	assert.Equal(t, []time.Time{validTime, validTime, validTime}, []time.Time{
		logs[0].Time, logs[1].Time, logs[2].Time,
	})
	assert.Equal(t, []int{1, 2, 3}, []int{
		logs[0].CombinedLinenumber, logs[1].CombinedLinenumber, logs[2].CombinedLinenumber,
	})
}

// TestAnchorUnresolvedMalformedPrefixWithoutAnchor verifies that an entirely
// malformed earlier page leaves the unresolved boundary untouched.
func TestAnchorUnresolvedMalformedPrefixWithoutAnchor(t *testing.T) {
	logs := []LogMsg{{Malformed: true, Msg: "still unresolved"}}

	anchorUnresolvedMalformedPrefix(logs, time.Time{})

	assert.True(t, logs[0].Time.IsZero())
}

// TestRetainCoveredLogsKeepsUnresolvedMalformedRecords verifies that coverage
// trimming cannot hide raw records merely because their time is unavailable.
func TestRetainCoveredLogsKeepsUnresolvedMalformedRecords(t *testing.T) {
	coveredSince := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	logs := []LogMsg{
		{Malformed: true, Msg: "unresolved"},
		{Time: coveredSince.Add(-time.Minute), Msg: "outside coverage"},
		{Time: coveredSince, Msg: "covered"},
	}

	got := retainCoveredLogs(logs, coveredSince)

	if assert.Len(t, got, 2) {
		assert.Equal(t, "unresolved", got[0].Msg)
		assert.Equal(t, "covered", got[1].Msg)
	}
}

// TestRetainCoveredLogsUsesNormalSlicePath verifies that clean logs retain the
// original binary-search result without allocating or copying another slice.
func TestRetainCoveredLogsUsesNormalSlicePath(t *testing.T) {
	t1 := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	logs := []LogMsg{
		{Time: t1, Msg: "old"},
		{Time: t1.Add(time.Minute), Msg: "covered"},
		{Time: t1.Add(2 * time.Minute), Msg: "new"},
	}

	got := retainCoveredLogs(logs, t1.Add(time.Minute))

	if assert.Len(t, got, 2) {
		assert.Equal(t, "covered", got[0].Msg)
		assert.Same(t, &logs[1], &got[0])
	}
}

// TestFirstResolvedLogTimeIgnoresLeadingMalformedRecords verifies that an
// unresolved raw record does not become a bogus coverage boundary.
func TestFirstResolvedLogTimeIgnoresLeadingMalformedRecords(t *testing.T) {
	want := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)

	got, ok := firstResolvedLogTime([]LogMsg{{Malformed: true}, {Time: want}})

	assert.True(t, ok)
	assert.Equal(t, want, got)
}

// TestJournalPaginationBoundaryRefusesUnresolvedOldestRecord verifies that
// journal pagination never substitutes a later timestamp or sends year 1 when
// the oldest loaded record has no usable timestamp.
func TestJournalPaginationBoundaryRefusesUnresolvedOldestRecord(t *testing.T) {
	resolvedTime := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	logs := []LogMsg{
		{LogFilename: SpecialFilenameJournalctl, Malformed: true, Msg: "unresolved"},
		{LogFilename: SpecialFilenameJournalctl, Time: resolvedTime, Msg: "resolved"},
	}

	boundary, err := getJournalPaginationBoundary(logs)

	assert.Nil(t, boundary)
	assert.EqualError(t, err, "cannot load earlier journal records: the oldest loaded record has no valid timestamp for pagination")
}
