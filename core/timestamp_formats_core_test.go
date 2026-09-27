package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dimonomid/clock"
	"github.com/dimonomid/nerdlog/core/testutils"
	"github.com/dimonomid/nerdlog/log"
	"github.com/stretchr/testify/assert"
)

type timestampFormatCoreTestCase struct {
	name     string
	filename string
	format   TimestampFormat
	// skipTimezoneTest is true for formats without a timezone, whose wall-clock
	// timestamps cannot be shifted while keeping the server timezone at UTC.
	skipTimezoneTest bool
	transformTime    func(string) string
	want             *timestampFormatCoreResult
}

// timestampFormatCoreTimezone describes a timezone to use when running test
// cases. The location is used only when formatting inputTimes into log lines.
// For example, with UTC-3, the input line "2025-09-08T08:09:10.123456Z" is
// written into log files as "2025-09-08T05:09:10.123456-03:00".
//
// The simulated server timezone remains explicitly set to UTC regardless -
// this way we can assert that the timezone in the log lines is parsed
// correctly.
type timestampFormatCoreTimezone struct {
	name     string
	location *time.Location
}

type timestampFormatCoreResult struct {
	NumMsgsTotal int
	Errs         []string
	NumWarnings  int
	Warnings     []string
	MinuteStats  map[string]int
	IndexEntries []timestampFormatCoreIndexEntry
	Logs         []timestampFormatCoreLog
}

type timestampFormatCoreIndexEntry struct {
	Time string
	Line int
}

type timestampFormatCoreLog struct {
	Time string
	Msg  string
}

const timestampFormatTestTimeLayout = "2006-01-02T15:04:05.000000000Z07:00"
const timestampFormatTestMinuteLayout = "2006-01-02T15:04Z07:00"

func TestTimestampFormatsThroughAgent(t *testing.T) {
	commonSuffixes := []string{
		" myhost app[123]: message one",
		" myhost app[123]: message two",
		" myhost app[123]: message three",
		" myhost app[123]: message four",
		" myhost app[123]: message five",
		" myhost app[123]: message six",
		" myhost app[123]: message seven",
		" myhost app[123]: message eight",
		" myhost app[123]: message nine",
		" myhost app[123]: message ten",
		" myhost app[123]: message eleven",
		" myhost app[123]: message twelve",
	}

	inputTimes := []time.Time{
		time.Date(2025, 9, 8, 8, 9, 10, 123456000, time.UTC),
		time.Date(2025, 9, 8, 8, 9, 20, 223456000, time.UTC),
		time.Date(2025, 9, 18, 9, 10, 11, 234567000, time.UTC),
		time.Date(2025, 10, 8, 10, 11, 12, 345678000, time.UTC),
		time.Date(2025, 10, 8, 10, 11, 22, 445678000, time.UTC),
		time.Date(2025, 10, 18, 11, 12, 13, 456789000, time.UTC),
		time.Date(2025, 11, 18, 12, 13, 14, 567890000, time.UTC),
		time.Date(2025, 11, 18, 22, 45, 36, 678901000, time.UTC),
		time.Date(2025, 11, 18, 22, 45, 46, 800123000, time.UTC),
		time.Date(2025, 11, 18, 22, 45, 56, 789012000, time.UTC),
		time.Date(2025, 11, 18, 23, 57, 36, 890123000, time.UTC),
		time.Date(2025, 11, 18, 23, 57, 56, 901234000, time.UTC),
	}

	identityTime := func(s string) string { return s }
	truncateToSecond := func(s string) string {
		t, err := time.Parse(timestampFormatTestTimeLayout, s)
		if err != nil {
			panic(err)
		}
		return t.Truncate(time.Second).Format(timestampFormatTestTimeLayout)
	}
	truncateToMillisecond := func(s string) string {
		t, err := time.Parse(timestampFormatTestTimeLayout, s)
		if err != nil {
			panic(err)
		}
		return t.Truncate(time.Millisecond).Format(timestampFormatTestTimeLayout)
	}

	tests := []timestampFormatCoreTestCase{
		{
			name:             "syslog",
			filename:         "syslog",
			format:           TimestampFormat{Layout: "Jan _2 15:04:05"},
			skipTimezoneTest: true,
			transformTime:    truncateToSecond,
		},
		{
			name:          "iso8601 microseconds utc",
			filename:      "syslog",
			format:        TimestampFormat{Layout: "2006-01-02T15:04:05.000000Z07:00"},
			transformTime: identityTime,
		},
		{
			name:          "iso8601 microseconds numeric timezone",
			filename:      "syslog",
			format:        TimestampFormat{Layout: "2006-01-02T15:04:05.000000-0700"},
			transformTime: identityTime,
		},
		{
			name:             "space separated iso8601",
			filename:         "syslog",
			format:           TimestampFormat{Layout: "2006-01-02 15:04:05"},
			skipTimezoneTest: true,
			transformTime:    truncateToSecond,
		},
		{
			name:          "rfc3339",
			filename:      "syslog",
			format:        TimestampFormat{Layout: "2006-01-02T15:04:05Z07:00"},
			transformTime: truncateToSecond,
		},
		{
			name:          "rfc3339 milliseconds",
			filename:      "syslog",
			format:        TimestampFormat{Layout: "2006-01-02T15:04:05.000Z07:00"},
			transformTime: truncateToMillisecond,
		},
		{
			name:          "apache access",
			filename:      "syslog",
			format:        TimestampFormat{Layout: "02/Jan/2006:15:04:05 -0700"},
			transformTime: truncateToSecond,
		},
		{
			name:             "slash separated date",
			filename:         "syslog",
			format:           TimestampFormat{Layout: "2006/01/02 15:04:05"},
			skipTimezoneTest: true,
			transformTime:    truncateToSecond,
		},
		{
			name:             "hyphenated month",
			filename:         "syslog",
			format:           TimestampFormat{Layout: "02-Jan-2006 15:04:05"},
			skipTimezoneTest: true,
			transformTime:    truncateToSecond,
		},
		{
			name:             "month day",
			filename:         "syslog",
			format:           TimestampFormat{Layout: "Jan 02 15:04:05"},
			skipTimezoneTest: true,
			transformTime:    truncateToSecond,
		},
		{
			name:             "apache microseconds",
			filename:         "syslog",
			format:           TimestampFormat{Layout: "[Mon Jan 02 15:04:05.999999 2006]"},
			skipTimezoneTest: true,
			transformTime:    identityTime,
		},
		{
			name:             "weekday date is not generated yet",
			filename:         "syslog",
			format:           TimestampFormat{Layout: "Mon Jan 2 15:04:05 2006"},
			skipTimezoneTest: true,
			transformTime:    identityTime,
			want: &timestampFormatCoreResult{
				Errs:         []string{"in \"syslog\": unable to detect timestamp format"},
				Warnings:     []string{},
				MinuteStats:  map[string]int{},
				IndexEntries: []timestampFormatCoreIndexEntry{},
				Logs:         []timestampFormatCoreLog{},
			},
		},
		{
			name:             "apache seconds is shadowed by microseconds format",
			filename:         "syslog",
			format:           TimestampFormat{Layout: "[Mon Jan 02 15:04:05 2006]"},
			skipTimezoneTest: true,
			transformTime:    identityTime,
			want: &timestampFormatCoreResult{
				Errs:        []string{},
				NumWarnings: 1,
				Warnings: []string{
					"timestamps: log index ignored 12 malformed timestamp candidates; first malformed line: syslog:1",
				},
				MinuteStats:  map[string]int{},
				IndexEntries: []timestampFormatCoreIndexEntry{},
				Logs:         []timestampFormatCoreLog{},
			},
		},
	}

	timezones := []timestampFormatCoreTimezone{
		{name: "UTC", location: time.UTC},
		{name: "UTC-3", location: time.FixedZone("UTC-3", -3*60*60)},
		{name: "UTC+2", location: time.FixedZone("UTC+2", 2*60*60)},
	}

	for _, timezone := range timezones {
		t.Run(timezone.name, func(t *testing.T) {
			for _, tc := range tests {
				if tc.skipTimezoneTest && timezone.location != time.UTC {
					continue
				}
				t.Run(tc.name, func(t *testing.T) {
					runTimestampFormatTest(t, timezone, tc, inputTimes, commonSuffixes)
				})
			}
		})
	}
}

func runTimestampFormatTest(
	t *testing.T,
	timezone timestampFormatCoreTimezone,
	tc timestampFormatCoreTestCase,
	inputTimes []time.Time,
	commonSuffixes []string,
) {
	logFilename := filepath.Join(t.TempDir(), tc.filename)
	data := ""
	formatLayout := strings.ReplaceAll(tc.format.Layout, ".999999", ".000000")
	for i, inputTime := range inputTimes {
		line := inputTime.In(timezone.location).Format(formatLayout) + commonSuffixes[i]
		data += line + "\n"
	}
	_ = os.WriteFile(logFilename, []byte(data), 0644)

	clockMock := clock.NewMock()
	clockMock.Set(time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC))
	updatesCh := make(chan LStreamsManagerUpdate, 100)
	clientID := fmt.Sprintf("timestamp-format-%s", testutils.Slug(tc.name))
	manager := NewLStreamsManager(LStreamsManagerParams{
		ConfigLogStreams: ConfigLogStreams{
			"timestamps": {
				Hostname: "localhost",
				LogFiles: []string{logFilename},
				Options:  ConfigLogStreamOptions{ShellInit: []string{"export TZ=UTC"}},
			},
		},
		InitialLStreams:             "timestamps",
		ClientID:                    clientID,
		UpdatesCh:                   updatesCh,
		Clock:                       clockMock,
		InitialDefaultTransportMode: mustParseTransportModeForTest(),
		Logger:                      log.NewLogger(log.Verbose1).WithStdout(true),
	})

	th := &LStreamsManagerTestHelper{manager: manager, updatesCh: updatesCh, clock: clockMock}
	bootstrapIssues := make(chan string, 1)
	go func() {
		for upd := range updatesCh {
			if upd.BootstrapIssue != nil {
				bootstrapIssues <- strings.ReplaceAll(upd.BootstrapIssue.Err, logFilename, tc.filename)
			}
			th.applyUpdate(upd)
		}
	}()

	var bootstrapErr string
	connected := false
	deadline := time.NewTimer(5 * time.Second)
	poll := time.NewTicker(100 * time.Millisecond)
waitForConnection:
	for {
		if th.isConnected() {
			connected = true
			break
		}
		select {
		case bootstrapErr = <-bootstrapIssues:
			break waitForConnection
		case <-deadline.C:
			bootstrapErr = "timed out waiting for connection"
			break waitForConnection
		case <-poll.C:
		}
	}
	deadline.Stop()
	poll.Stop()

	got := timestampFormatCoreResult{
		Errs:         []string{},
		Warnings:     []string{},
		MinuteStats:  map[string]int{},
		IndexEntries: []timestampFormatCoreIndexEntry{},
		Logs:         []timestampFormatCoreLog{},
	}
	if !connected {
		got.Errs = append(got.Errs, bootstrapErr)
		if err := th.CloseAndWait(); err != nil {
			got.Errs = append(got.Errs, err.Error())
		}
	} else {
		resp, err := th.QueryLogs(CoreTestStepQueryParams{
			MaxNumLines:  20,
			From:         testMyTime(time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC)),
			To:           testMyTime(time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)),
			RefreshIndex: true,
		})
		if err != nil {
			got.Errs = append(got.Errs, err.Error())
		}
		if resp != nil {
			got.NumMsgsTotal = resp.NumMsgsTotal
			got.NumWarnings = resp.NumWarnings
			for _, err := range resp.Errs {
				got.Errs = append(got.Errs, err.Error())
			}
			for _, warning := range resp.Warnings {
				warningString := fmt.Sprintf("%s: %s", warning.LStreamName, warning.Err.Error())
				got.Warnings = append(got.Warnings, strings.ReplaceAll(warningString, logFilename, tc.filename))
			}
			for minute, stats := range resp.MinuteStats {
				got.MinuteStats[time.Unix(minute, 0).UTC().Format(timestampFormatTestMinuteLayout)] = stats.NumMsgs
			}
			for _, logMsg := range resp.Logs {
				got.Logs = append(got.Logs, timestampFormatCoreLog{
					Time: logMsg.Time.Format(timestampFormatTestTimeLayout),
					Msg:  logMsg.Msg,
				})
			}
		}
		if err := th.CloseAndWait(); err != nil {
			got.Errs = append(got.Errs, err.Error())
		}
	}
	if connected {
		indexPath := fmt.Sprintf("/tmp/nerdlog_agent_index_%s_%s", clientID, filepathToId(logFilename))
		indexData, err := os.ReadFile(indexPath)
		if err != nil {
			got.Errs = append(got.Errs, err.Error())
		} else {
			for _, line := range strings.Split(strings.TrimSpace(string(indexData)), "\n") {
				fields := strings.Split(line, "\t")
				if len(fields) >= 4 && fields[0] == "idx" {
					line, lineErr := strconv.Atoi(fields[2])
					if lineErr != nil {
						got.Errs = append(got.Errs, fmt.Sprintf("malformed index entry: %s", strings.Join(fields, "\t")))
						continue
					}
					got.IndexEntries = append(got.IndexEntries, timestampFormatCoreIndexEntry{
						Time: fields[1],
						Line: line,
					})
				}
			}
		}

	}

	want := timestampFormatCoreResult{
		NumMsgsTotal: 12,
		Errs:         []string{},
		Warnings:     []string{},
		MinuteStats: map[string]int{
			"2025-09-08T08:09Z": 2,
			"2025-09-18T09:10Z": 1,
			"2025-10-08T10:11Z": 2,
			"2025-10-18T11:12Z": 1,
			"2025-11-18T12:13Z": 1,
			"2025-11-18T22:45Z": 3,
			"2025-11-18T23:57Z": 2,
		},
		IndexEntries: []timestampFormatCoreIndexEntry{
			{Time: "2025-09-08-08:09", Line: 1},
			{Time: "2025-09-18-09:10", Line: 3},
			{Time: "2025-10-08-10:11", Line: 4},
			{Time: "2025-10-18-11:12", Line: 6},
			{Time: "2025-11-18-12:13", Line: 7},
			{Time: "2025-11-18-22:45", Line: 8},
			{Time: "2025-11-18-23:57", Line: 11},
		},
		Logs: []timestampFormatCoreLog{
			{Time: tc.transformTime("2025-09-08T08:09:10.123456000Z"), Msg: "message one"},
			{Time: tc.transformTime("2025-09-08T08:09:20.223456000Z"), Msg: "message two"},
			{Time: tc.transformTime("2025-09-18T09:10:11.234567000Z"), Msg: "message three"},
			{Time: tc.transformTime("2025-10-08T10:11:12.345678000Z"), Msg: "message four"},
			{Time: tc.transformTime("2025-10-08T10:11:22.445678000Z"), Msg: "message five"},
			{Time: tc.transformTime("2025-10-18T11:12:13.456789000Z"), Msg: "message six"},
			{Time: tc.transformTime("2025-11-18T12:13:14.567890000Z"), Msg: "message seven"},
			{Time: tc.transformTime("2025-11-18T22:45:36.678901000Z"), Msg: "message eight"},
			{Time: tc.transformTime("2025-11-18T22:45:46.800123000Z"), Msg: "message nine"},
			{Time: tc.transformTime("2025-11-18T22:45:56.789012000Z"), Msg: "message ten"},
			{Time: tc.transformTime("2025-11-18T23:57:36.890123000Z"), Msg: "message eleven"},
			{Time: tc.transformTime("2025-11-18T23:57:56.901234000Z"), Msg: "message twelve"},
		},
	}
	if tc.want != nil {
		want = *tc.want
	}
	if timezone.location != time.UTC {
		// TODO: Once index and minute stats honor explicit log-line timezones,
		// remove this adjustment and use the UTC values above.
		_, offset := time.Now().In(timezone.location).Zone()
		shift := time.Duration(offset) * time.Second

		minuteStats := map[string]int{}
		for minute, count := range want.MinuteStats {
			parsedTime, err := time.Parse(timestampFormatTestMinuteLayout, minute)
			if err != nil {
				t.Fatalf("parse expected minute stat time %q: %v", minute, err)
			}
			minuteStats[parsedTime.Add(shift).Format(timestampFormatTestMinuteLayout)] = count
		}
		want.MinuteStats = minuteStats

		indexEntries := make([]timestampFormatCoreIndexEntry, len(want.IndexEntries))
		for i, entry := range want.IndexEntries {
			parsedTime, err := time.Parse("2006-01-02-15:04", entry.Time)
			if err != nil {
				t.Fatalf("parse expected index time %q: %v", entry.Time, err)
			}
			indexEntries[i] = entry
			indexEntries[i].Time = parsedTime.Add(shift).Format("2006-01-02-15:04")
		}
		want.IndexEntries = indexEntries
	}

	assert.Equal(t, want, got)
}

func testMyTime(t time.Time) testutils.MyTime {
	return testutils.MyTime{Time: t}
}

func mustParseTransportModeForTest() *TransportMode {
	mode, err := ParseTransportMode("ssh-lib")
	if err != nil {
		panic(err)
	}
	return mode
}
