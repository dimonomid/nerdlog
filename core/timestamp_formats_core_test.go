package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dimonomid/clock"
	"github.com/dimonomid/nerdlog/core/testutils"
	"github.com/dimonomid/nerdlog/log"
	"github.com/stretchr/testify/assert"
)

type timestampFormatCoreTestCase struct {
	name          string
	filename      string
	format        TimestampFormat
	transformTime func(string) string
	want          *timestampFormatCoreResult
}

type timestampFormatCoreResult struct {
	NumMsgsTotal int
	Errs         []string
	NumWarnings  int
	Warnings     []string
	MinuteStats  map[string]int
	Logs         []timestampFormatCoreLog
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
	}

	inputTimes := []time.Time{
		time.Date(2025, 9, 8, 8, 9, 10, 123456000, time.UTC),
		time.Date(2025, 9, 8, 8, 9, 20, 223456000, time.UTC),
		time.Date(2025, 9, 18, 9, 10, 11, 234567000, time.UTC),
		time.Date(2025, 10, 8, 10, 11, 12, 345678000, time.UTC),
		time.Date(2025, 10, 8, 10, 11, 22, 445678000, time.UTC),
		time.Date(2025, 10, 18, 11, 12, 13, 456789000, time.UTC),
		time.Date(2025, 11, 18, 12, 13, 14, 567890000, time.UTC),
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
			name:          "syslog",
			filename:      "syslog",
			format:        TimestampFormat{Layout: "Jan _2 15:04:05"},
			transformTime: truncateToSecond,
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
			name:          "space separated iso8601",
			filename:      "syslog",
			format:        TimestampFormat{Layout: "2006-01-02 15:04:05"},
			transformTime: truncateToSecond,
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
			name:          "slash separated date",
			filename:      "syslog",
			format:        TimestampFormat{Layout: "2006/01/02 15:04:05"},
			transformTime: truncateToSecond,
		},
		{
			name:          "hyphenated month",
			filename:      "syslog",
			format:        TimestampFormat{Layout: "02-Jan-2006 15:04:05"},
			transformTime: truncateToSecond,
		},
		{
			name:          "month day",
			filename:      "syslog",
			format:        TimestampFormat{Layout: "Jan 02 15:04:05"},
			transformTime: truncateToSecond,
		},
		{
			name:          "apache microseconds",
			filename:      "syslog",
			format:        TimestampFormat{Layout: "[Mon Jan 02 15:04:05.999999 2006]"},
			transformTime: identityTime,
		},
		{
			name:          "weekday date is not generated yet",
			filename:      "syslog",
			format:        TimestampFormat{Layout: "Mon Jan 2 15:04:05 2006"},
			transformTime: identityTime,
			want: &timestampFormatCoreResult{
				Errs:        []string{"in \"syslog\": unable to detect timestamp format"},
				Warnings:    []string{},
				MinuteStats: map[string]int{},
				Logs:        []timestampFormatCoreLog{},
			},
		},
		{
			name:          "apache seconds is shadowed by microseconds format",
			filename:      "syslog",
			format:        TimestampFormat{Layout: "[Mon Jan 02 15:04:05 2006]"},
			transformTime: identityTime,
			want: &timestampFormatCoreResult{
				Errs:        []string{},
				NumWarnings: 1,
				Warnings: []string{
					"timestamps: log index ignored 7 malformed timestamp candidates; first malformed line: syslog:1",
				},
				MinuteStats: map[string]int{},
				Logs:        []timestampFormatCoreLog{},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logFilename := filepath.Join(t.TempDir(), tc.filename)
			data := ""
			formatLayout := strings.ReplaceAll(tc.format.Layout, ".999999", ".000000")
			for i, inputTime := range inputTimes {
				line := inputTime.Format(formatLayout) + commonSuffixes[i]
				data += line + "\n"
			}
			_ = os.WriteFile(logFilename, []byte(data), 0644)

			clockMock := clock.NewMock()
			clockMock.Set(time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC))
			updatesCh := make(chan LStreamsManagerUpdate, 100)
			manager := NewLStreamsManager(LStreamsManagerParams{
				ConfigLogStreams: ConfigLogStreams{
					"timestamps": {
						Hostname: "localhost",
						LogFiles: []string{logFilename},
						Options:  ConfigLogStreamOptions{ShellInit: []string{"export TZ=UTC"}},
					},
				},
				InitialLStreams:             "timestamps",
				ClientID:                    fmt.Sprintf("timestamp-format-%s", testutils.Slug(tc.name)),
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
				Errs:        []string{},
				Warnings:    []string{},
				MinuteStats: map[string]int{},
				Logs:        []timestampFormatCoreLog{},
			}
			if !connected {
				got.Errs = append(got.Errs, bootstrapErr)
				if err := th.CloseAndWait(); err != nil {
					got.Errs = append(got.Errs, err.Error())
				}
			} else {
				resp, err := th.QueryLogs(CoreTestStepQueryParams{
					MaxNumLines:  10,
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

			want := timestampFormatCoreResult{
				NumMsgsTotal: 7,
				Errs:         []string{},
				Warnings:     []string{},
				MinuteStats: map[string]int{
					"2025-09-08T08:09Z": 2,
					"2025-09-18T09:10Z": 1,
					"2025-10-08T10:11Z": 2,
					"2025-10-18T11:12Z": 1,
					"2025-11-18T12:13Z": 1,
				},
				Logs: []timestampFormatCoreLog{
					{Time: tc.transformTime("2025-09-08T08:09:10.123456000Z"), Msg: "message one"},
					{Time: tc.transformTime("2025-09-08T08:09:20.223456000Z"), Msg: "message two"},
					{Time: tc.transformTime("2025-09-18T09:10:11.234567000Z"), Msg: "message three"},
					{Time: tc.transformTime("2025-10-08T10:11:12.345678000Z"), Msg: "message four"},
					{Time: tc.transformTime("2025-10-08T10:11:22.445678000Z"), Msg: "message five"},
					{Time: tc.transformTime("2025-10-18T11:12:13.456789000Z"), Msg: "message six"},
					{Time: tc.transformTime("2025-11-18T12:13:14.567890000Z"), Msg: "message seven"},
				},
			}
			if tc.want != nil {
				want = *tc.want
			}

			assert.Equal(t, want, got)
		})
	}
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
