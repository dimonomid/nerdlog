package main

import (
	"testing"
	"time"
)

// TestParseLogLineCustomData verifies that custom fixture metadata controls
// filtering time independently of the literal journalctl output.
func TestParseLogLineCustomData(t *testing.T) {
	entry, err := parseLogLine(`customdata:{"time":"2026-09-20T10:00:00Z","output":"\u0000malformed output"}`)
	if err != nil {
		t.Fatalf("parseLogLine returned an error: %v", err)
	}

	wantTime := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	if !entry.Timestamp.Equal(wantTime) {
		t.Errorf("Timestamp = %v, want %v", entry.Timestamp, wantTime)
	}
	if entry.Text != "\x00malformed output" {
		t.Errorf("Text = %q, want a leading NUL followed by malformed output", entry.Text)
	}
}

// TestParseLogLineCustomDataRequiresFields verifies that malformed custom
// fixture entries fail instead of silently acquiring zero values.
func TestParseLogLineCustomDataRequiresFields(t *testing.T) {
	for _, line := range []string{
		`customdata:{"output":"missing time"}`,
		`customdata:{"time":"2026-09-20T10:00:00Z"}`,
	} {
		if _, err := parseLogLine(line); err == nil {
			t.Errorf("parseLogLine(%q) unexpectedly succeeded", line)
		}
	}
}
