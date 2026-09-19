package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/pflag"
)

const customDataPrefix = "customdata:"

type LogEntry struct {
	Timestamp time.Time
	Text      string
}

// Parses the timestamp from the start of the line and returns it along with the rest of the message.
func parseLogLine(line string) (*LogEntry, error) {
	if strings.HasPrefix(line, customDataPrefix) {
		return parseCustomLogLine(strings.TrimPrefix(line, customDataPrefix))
	}

	splitIndex := strings.Index(line, " ")
	if splitIndex == -1 {
		return nil, errors.New("invalid log line: no space found")
	}

	timestampStr := line[:splitIndex]
	//rest := line[splitIndex+1:]

	timestamp, err := time.Parse(time.RFC3339Nano, timestampStr)
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp format: %w", err)
	}

	return &LogEntry{Timestamp: timestamp, Text: line}, nil
}

// parseCustomLogLine parses fixture-only JSON that separates the timestamp
// used by the mock from the text emitted as journalctl output.
func parseCustomLogLine(data string) (*LogEntry, error) {
	var customData struct {
		Time   *time.Time `json:"time"`
		Output *string    `json:"output"`
	}
	if err := json.Unmarshal([]byte(data), &customData); err != nil {
		return nil, fmt.Errorf("invalid custom journal entry: %w", err)
	}
	if customData.Time == nil {
		return nil, errors.New("invalid custom journal entry: missing time")
	}
	if customData.Output == nil {
		return nil, errors.New("invalid custom journal entry: missing output")
	}

	return &LogEntry{Timestamp: *customData.Time, Text: *customData.Output}, nil
}

func loadLogEntries(path string) ([]LogEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var entries []LogEntry
	var current *LogEntry

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, customDataPrefix) ||
			(len(line) >= 20 && line[4] == '-' && line[7] == '-' && line[10] == 'T') {
			// New entry
			entry, err := parseLogLine(line)
			if err != nil {
				return nil, err
			}
			if current != nil {
				entries = append(entries, *current)
			}
			current = entry
		} else if current != nil {
			// Multiline continuation
			current.Text += "\n" + line
		}
	}
	if current != nil {
		entries = append(entries, *current)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

func main() {
	sigs := make(chan os.Signal, 1)

	// Notify for SIGPIPE
	signal.Notify(sigs, syscall.SIGPIPE)

	go func() {
		for sig := range sigs {
			if sig == syscall.SIGPIPE {
				// Exit silently with 141
				os.Exit(128 + int(syscall.SIGPIPE))
			}
		}
	}()

	var (
		output   string
		quiet    bool
		since    string
		until    string
		reverse  bool
		numLines int
	)

	pflag.StringVar(&output, "output", "", "Set output format")
	pflag.BoolVar(&quiet, "quiet", false, "Suppress extra output")
	pflag.StringVar(&since, "since", "", "Show entries not older than the specified time")
	pflag.StringVar(&until, "until", "", "Show entries not newer than the specified time")
	pflag.BoolVar(&reverse, "reverse", false, "Show newest entries first")
	pflag.IntVarP(&numLines, "lines", "n", -1, "Max number of lines to print")
	pflag.Parse()

	if output != "short-iso-precise" {
		fmt.Fprintln(os.Stderr, "Error: --output=short-iso-precise is required")
		os.Exit(1)
	}

	if !quiet {
		fmt.Fprintln(os.Stderr, "Error: --quiet is required")
		os.Exit(1)
	}

	logPath := os.Getenv("NERDLOG_JOURNALCTL_MOCK_DATA")
	if logPath == "" {
		fmt.Fprintln(os.Stderr, "Error: NERDLOG_JOURNALCTL_MOCK_DATA not set")
		os.Exit(1)
	}

	entries, err := loadLogEntries(logPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading log data: %v\n", err)
		os.Exit(1)
	}

	var sinceTime, untilTime time.Time
	if since != "" {
		sinceTime, err = parseJournalctlTimeArg(since)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid --since time: %v\n", err)
			os.Exit(1)
		}
	}
	if until != "" {
		untilTime, err = parseJournalctlTimeArg(until)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid --until time: %v\n", err)
			os.Exit(1)
		}
	}

	// Filter by time
	var filtered []LogEntry
	for _, e := range entries {
		if !sinceTime.IsZero() && e.Timestamp.Before(sinceTime) {
			continue
		}
		if !untilTime.IsZero() && e.Timestamp.After(untilTime) {
			continue
		}
		filtered = append(filtered, e)
	}

	// Reverse if needed
	if reverse {
		for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
			filtered[i], filtered[j] = filtered[j], filtered[i]
		}
	}

	// Print
	for i, e := range filtered {
		if numLines >= 0 && i >= numLines {
			break
		}

		fmt.Println(e.Text)
	}
}

// parseJournalctlTimeArg parses one of the two possible time formats:
//
//	"2006-01-02 15:04:00"
//	"2006-01-02 15:04:00.000000"
func parseJournalctlTimeArg(s string) (time.Time, error) {
	const layout1 = "2006-01-02 15:04:05"

	if t, err := time.Parse(layout1, s); err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("invalid time format: %q", s)
}
