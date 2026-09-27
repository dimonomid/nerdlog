package core

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type detectTimeTestCase struct {
	name        string
	logFilename string
	logLine     string
	wantFormat  *TimestampFormat
	wantErr     string
}

func TestDetectTimestampFormat(t *testing.T) {
	testCases := []detectTimeTestCase{
		{
			name:        "rsyslog no year, 1-digit",
			logFilename: "/var/log/syslog",
			logLine:     "Apr  8 01:02:03 somehost systemd[1]: Started something.",
			wantFormat:  &TimestampFormat{Layout: "Jan _2 15:04:05"},
		},
		{
			name:        "rsyslog no year, 2-digit",
			logFilename: "/var/log/syslog",
			logLine:     "Apr 18 01:02:03 somehost systemd[1]: Started something.",
			wantFormat:  &TimestampFormat{Layout: "Jan _2 15:04:05"},
		},
		{
			name:        "rsyslog no year, month and hour >=10",
			logFilename: "/var/log/syslog",
			logLine:     "Oct 18 11:22:33 somehost systemd[1]: Started something.",
			wantFormat:  &TimestampFormat{Layout: "Jan _2 15:04:05"},
		},
		{
			name:        "ISO8601 non-UTC full with microseconds",
			logFilename: "/var/log/syslog",
			logLine:     "2024-04-19T14:23:45.123456+02:00 INFO something happened",
			wantFormat:  &TimestampFormat{Layout: "2006-01-02T15:04:05.000000Z07:00"},
		},
		{
			name:        "ISO8601 non-UTC full with comma microseconds",
			logFilename: "/var/log/syslog",
			logLine:     "2024-04-19T14:23:45,123456+02:00 INFO something happened",
			wantFormat:  &TimestampFormat{Layout: "2006-01-02T15:04:05.000000Z07:00"},
		},
		{
			name:        "bracketed weekday with comma microseconds",
			logFilename: "/var/log/syslog",
			logLine:     "[Wed Oct 08 09:33:13,123456 2025] Starting server",
			wantFormat:  &TimestampFormat{Layout: "[Mon Jan 2 15:04:05.000000 2006]"},
		},
		{
			name:        "ISO8601 UTC full with microseconds",
			logFilename: "/var/log/syslog",
			logLine:     "2024-04-19T14:23:45.123456Z INFO something happened",
			wantFormat:  &TimestampFormat{Layout: "2006-01-02T15:04:05.000000Z07:00"},
		},
		{
			name:        "ISO8601 full with microseconds, low values",
			logFilename: "/var/log/syslog",
			logLine:     "2024-10-08T09:23:45.123456+02:00 INFO something happened",
			wantFormat:  &TimestampFormat{Layout: "2006-01-02T15:04:05.000000Z07:00"},
		},
		{
			name:        "RFC3339",
			logFilename: "/var/log/syslog",
			logLine:     "2024-04-19T14:23:45+02:00 Starting server",
			wantFormat:  &TimestampFormat{Layout: "2006-01-02T15:04:05Z07:00"},
		},
		{
			name:        "older journalctl with --output=short-iso-precise",
			logFilename: "/var/log/syslog",
			logLine:     "2025-05-11T21:33:13.924352+0200 Starting server",
			wantFormat:  &TimestampFormat{Layout: "2006-01-02T15:04:05.000000-0700"},
		},
		{
			name:        "older journalctl with low values",
			logFilename: "/var/log/syslog",
			logLine:     "2025-10-08T09:33:13.924352+0200 Starting server",
			wantFormat:  &TimestampFormat{Layout: "2006-01-02T15:04:05.000000-0700"},
		},
		{
			name:        "space-separated ISO8601, low values",
			logFilename: "/var/log/syslog",
			logLine:     "2025-10-08 09:33:13 Starting server",
			wantFormat:  &TimestampFormat{Layout: "2006-01-02 15:04:05"},
		},
		{
			name:        "space-separated ISO8601, high values",
			logFilename: "/var/log/syslog",
			logLine:     "2025-11-18 11:33:13 Starting server",
			wantFormat:  &TimestampFormat{Layout: "2006-01-02 15:04:05"},
		},
		{
			name:        "RFC3339 low values",
			logFilename: "/var/log/syslog",
			logLine:     "2025-10-08T09:33:13+02:00 Starting server",
			wantFormat:  &TimestampFormat{Layout: "2006-01-02T15:04:05Z07:00"},
		},
		{
			name:        "RFC3339 high values",
			logFilename: "/var/log/syslog",
			logLine:     "2025-11-18T11:33:13+02:00 Starting server",
			wantFormat:  &TimestampFormat{Layout: "2006-01-02T15:04:05Z07:00"},
		},
		{
			name:        "RFC3339 with directly adjacent metadata",
			logFilename: "/var/log/syslog",
			logLine:     "2025-10-08T09:33:13+02:00[INFO] message",
			wantFormat:  &TimestampFormat{Layout: "2006-01-02T15:04:05Z07:00", HasTrailingChars: true},
		},
		{
			name:        "RFC3339 milliseconds low values",
			logFilename: "/var/log/syslog",
			logLine:     "2025-10-08T09:33:13.123+02:00 Starting server",
			wantFormat:  &TimestampFormat{Layout: "2006-01-02T15:04:05.000Z07:00"},
		},
		{
			name:        "RFC3339 milliseconds high values",
			logFilename: "/var/log/syslog",
			logLine:     "2025-11-18T11:33:13.123+02:00 Starting server",
			wantFormat:  &TimestampFormat{Layout: "2006-01-02T15:04:05.000Z07:00"},
		},
		{
			name:        "RFC3339 four fractional digits",
			logFilename: "/var/log/syslog",
			logLine:     "2025-10-08T09:33:13.1234Z Starting server",
			wantFormat:  &TimestampFormat{Layout: "2006-01-02T15:04:05.0000Z07:00"},
		},
		{
			name:        "Apache slash format low values",
			logFilename: "/var/log/syslog",
			logLine:     "08/Oct/2025:09:33:13 +0200 GET /",
			wantFormat:  &TimestampFormat{Layout: "02/Jan/2006:15:04:05 -0700"},
		},
		{
			name:        "Apache slash format high values",
			logFilename: "/var/log/syslog",
			logLine:     "18/Nov/2025:11:33:13 +0200 GET /",
			wantFormat:  &TimestampFormat{Layout: "02/Jan/2006:15:04:05 -0700"},
		},
		{
			name:        "slash-separated date low values",
			logFilename: "/var/log/syslog",
			logLine:     "2025/10/08 09:33:13 Starting server",
			wantFormat:  &TimestampFormat{Layout: "2006/01/02 15:04:05"},
		},
		{
			name:        "slash-separated date high values",
			logFilename: "/var/log/syslog",
			logLine:     "2025/11/18 11:33:13 Starting server",
			wantFormat:  &TimestampFormat{Layout: "2006/01/02 15:04:05"},
		},
		{
			name:        "weekday date low values",
			logFilename: "/var/log/syslog",
			logLine:     "Wed Oct 8 09:33:13 2025 Starting server",
			wantFormat:  &TimestampFormat{Layout: "Mon Jan 2 15:04:05 2006"},
		},
		{
			name:        "weekday date high values",
			logFilename: "/var/log/syslog",
			logLine:     "Tue Nov 18 11:33:13 2025 Starting server",
			wantFormat:  &TimestampFormat{Layout: "Mon Jan 2 15:04:05 2006"},
		},
		{
			name:        "hyphenated month date low values",
			logFilename: "/var/log/syslog",
			logLine:     "08-Oct-2025 09:33:13 Starting server",
			wantFormat:  &TimestampFormat{Layout: "02-Jan-2006 15:04:05"},
		},
		{
			name:        "hyphenated month date high values",
			logFilename: "/var/log/syslog",
			logLine:     "18-Nov-2025 11:33:13 Starting server",
			wantFormat:  &TimestampFormat{Layout: "02-Jan-2006 15:04:05"},
		},
		{
			name:        "month day format low values",
			logFilename: "/var/log/syslog",
			logLine:     "Oct 08 09:33:13 Starting server",
			wantFormat:  &TimestampFormat{Layout: "Jan _2 15:04:05"},
		},
		{
			name:        "month day format high values",
			logFilename: "/var/log/syslog",
			logLine:     "Nov 18 11:33:13 Starting server",
			wantFormat:  &TimestampFormat{Layout: "Jan _2 15:04:05"},
		},
		{
			name:        "ambiguous numeric date keeps first matching layout",
			logFilename: "/var/log/syslog",
			logLine:     "10/08/25 09:33:13 Starting server",
			wantFormat:  &TimestampFormat{Layout: "02/01/06 15:04:05"},
		},
		{
			name:        "12-hour format is detected before descriptor rejection",
			logFilename: "/var/log/syslog",
			logLine:     "Oct 08, 2025 03:04:05 PM Starting server",
			wantFormat:  &TimestampFormat{Layout: "Jan _2, 2006 3:04:05 PM"},
		},
		{
			name:        "Apache bracketed microseconds low values",
			logFilename: "/var/log/syslog",
			logLine:     "[Wed Oct 08 09:33:13.123456 2025] Starting server",
			wantFormat:  &TimestampFormat{Layout: "[Mon Jan 2 15:04:05.000000 2006]"},
		},
		{
			name:        "Apache bracketed microseconds high values",
			logFilename: "/var/log/syslog",
			logLine:     "[Tue Nov 18 11:33:13.123456 2025] Starting server",
			wantFormat:  &TimestampFormat{Layout: "[Mon Jan 2 15:04:05.000000 2006]"},
		},
		{
			name:        "Apache bracketed seconds low values",
			logFilename: "/var/log/syslog",
			logLine:     "[Wed Oct 08 09:33:13 2025] Starting server",
			wantFormat:  &TimestampFormat{Layout: "[Mon Jan 2 15:04:05 2006]"},
		},
		{
			name:        "Apache bracketed seconds high values",
			logFilename: "/var/log/syslog",
			logLine:     "[Tue Nov 18 11:33:13 2025] Starting server",
			wantFormat:  &TimestampFormat{Layout: "[Mon Jan 2 15:04:05 2006]"},
		},
		{
			name:        "No timestamp in line",
			logFilename: "/var/log/syslog",
			logLine:     "This is a log line without a timestamp.",
			wantFormat:  nil,
			wantErr:     "unable to detect timestamp format",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			format, err := DetectTimestampFormat(tc.logLine)
			if tc.wantErr != "" {
				assert.EqualError(t, err, tc.wantErr)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.wantFormat, format)
		})
	}
}

// TestDetectTimestampFormatFractionalSecondWidths exhaustively covers the
// detector's supported fractional widths. The shared descriptor/agent matrix
// uses only no fraction, milliseconds, and microseconds to keep that expensive
// matrix representative; this compact test ensures widths 1 through 9 still
// select the exact corresponding layout.
func TestDetectTimestampFormatFractionalSecondWidths(t *testing.T) {
	for digits := 1; digits <= 9; digits++ {
		t.Run(fmt.Sprintf("%d digits", digits), func(t *testing.T) {
			fraction := strings.Repeat("1", digits)
			line := "2025-10-08T09:33:13." + fraction + "+02:00 Starting server"
			wantLayout := "2006-01-02T15:04:05." + strings.Repeat("0", digits) + "Z07:00"

			got, err := DetectTimestampFormat(line)
			assert.NoError(t, err)
			assert.Equal(t, &TimestampFormat{Layout: wantLayout}, got)
		})
	}
}

// TestDetectTimestampFormatLongerLayoutIsNotShadowedByShorterPrefix guards the
// longest-first ordering of knownLayouts. Each case first proves that the
// shorter prefix is itself a valid match, then checks that detection returns
// the longer, more specific layout instead.
func TestDetectTimestampFormatLongerLayoutIsNotShadowedByShorterPrefix(t *testing.T) {
	tests := []struct {
		name                  string
		line                  string
		shortLayout           string
		shortHasTrailingChars bool
		wantLayout            string
	}{
		{
			name:        "timezone in separate field",
			line:        "2025-10-08 09:33:13 +0200 Starting server",
			shortLayout: "2006-01-02 15:04:05",
			wantLayout:  "2006-01-02 15:04:05 -0700",
		},
		{
			name:                  "timezone attached to seconds",
			line:                  "2025-10-08 09:33:13+0200 Starting server",
			shortLayout:           "2006-01-02 15:04:05",
			shortHasTrailingChars: true,
			wantLayout:            "2006-01-02 15:04:05-0700",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			shortFormat, err := (TimestampFormat{
				Layout:           tc.shortLayout,
				HasTrailingChars: tc.shortHasTrailingChars,
			}).Compile()
			assert.NoError(t, err)
			if assert.NotNil(t, shortFormat) {
				_, err = shortFormat.Parse(tc.line)
				assert.NoError(t, err, "test case must demonstrate that the shorter layout also matches")
			}

			format, err := DetectTimestampFormat(tc.line)
			assert.NoError(t, err)
			if assert.NotNil(t, format) {
				assert.Equal(t, tc.wantLayout, format.Layout)
			}
		})
	}
}

func TestInferTimeFormatDescrRejectsMixedTrailingChars(t *testing.T) {
	_, err := InferTimeFormatDescr("/var/log/syslog", []string{
		"2025-10-08T09:33:13Z host app: one",
		"2025-10-08T09:33:14Z[INFO] host app: two",
	})
	assert.EqualError(t, err, `log file "/var/log/syslog" has lines with different formats: 2006-01-02T15:04:05Z07:00 (trailing chars: false) and 2006-01-02T15:04:05Z07:00 (trailing chars: true)`)
}

func TestTimestampFieldsAndRemoval(t *testing.T) {
	format := TimestampFormat{
		Layout:            "[Mon Jan 02 15:04:05.999999 2006]",
		StartFieldIdx:     0,
		StartSubstrOffset: len("prefix="),
	}
	compiledFormat, err := format.Compile()
	assert.NoError(t, err)

	line := "prefix=[Wed Oct 08 09:33:13.123456 2025] suffix"
	result, err := compiledFormat.timestampPrefixText(line)
	assert.NoError(t, err)
	assert.Equal(t, "[Wed Oct 08 09:33:13.123456 2025]", result.text)
	assert.Equal(t, len("prefix="), result.origStart)
	assert.Equal(t, len("prefix=[Wed Oct 08 09:33:13.123456 2025]"), result.origEnd)

	lsc := &LStreamClient{
		location:   time.UTC,
		timeFormat: &TimeFormatDescr{TimestampFormat: *compiledFormat},
	}
	msg := &LogMsg{Msg: line}
	assert.NoError(t, lsc.parseLogMsgTimestamp(msg))
	assert.Equal(t, "prefix= suffix", msg.Msg)

	// Go permits a comma in input parsed by a dot-based fractional layout. The
	// lexical prefix matcher must preserve that behavior for 9-based layouts.
	commaLine := "prefix=[Wed Oct 08 09:33:13,123456 2025] suffix"
	commaResult, err := compiledFormat.timestampPrefixText(commaLine)
	assert.NoError(t, err)
	assert.Equal(t, "[Wed Oct 08 09:33:13,123456 2025]", commaResult.text)
	commaMsg := &LogMsg{Msg: commaLine}
	assert.NoError(t, lsc.parseLogMsgTimestamp(commaMsg))
	assert.Equal(t, "prefix= suffix", commaMsg.Msg)

	format.StartFieldIdx = 1
	format.StartSubstrOffset = 0
	compiledFormat, err = format.Compile()
	assert.NoError(t, err)
	lsc.timeFormat.TimestampFormat = *compiledFormat
	msg = &LogMsg{Msg: "before   [Wed Oct 08 09:33:13.123456 2025]   suffix"}
	assert.NoError(t, lsc.parseLogMsgTimestamp(msg))
	assert.Equal(t, "before suffix", msg.Msg)

	format = TimestampFormat{Layout: "2006-01-02T15:04:05Z07:00", HasTrailingChars: true}
	line = "2025-10-08T09:33:13+02:00[INFO] message"
	compiledFormat, err = format.Compile()
	assert.NoError(t, err)
	result, err = compiledFormat.timestampPrefixText(line)
	assert.NoError(t, err)
	assert.Equal(t, "2025-10-08T09:33:13+02:00", result.text)
	assert.Equal(t, 0, result.origStart)
	assert.Equal(t, len(result.text), result.origEnd)

	lsc2 := &LStreamClient{
		location:   time.UTC,
		timeFormat: &TimeFormatDescr{TimestampFormat: *compiledFormat},
	}
	msg = &LogMsg{Msg: line}
	assert.NoError(t, lsc2.parseLogMsgTimestamp(msg))
	assert.Equal(t, "[INFO] message", msg.Msg)
}

func TestTimestampFormatParse(t *testing.T) {
	format := TimestampFormat{Layout: "2006-01-02 15:04:05"}
	line := "2025-10-08 09:33:13 message"

	compiled, err := format.Compile()
	assert.NoError(t, err)
	tm, err := compiled.Parse(line)
	assert.NoError(t, err)
	assert.Equal(t, time.Date(2025, 10, 8, 9, 33, 13, 0, time.UTC), tm)

	location := time.FixedZone("test", 2*60*60)
	tm, err = compiled.ParseInLocation(line, location)
	assert.NoError(t, err)
	assert.Equal(t, time.Date(2025, 10, 8, 9, 33, 13, 0, location), tm)

	// Go accepts this implicit fractional part, but the layout does not
	// declare it. Rejecting it keeps Go parsing and generated AWK parsing in
	// agreement.
	_, err = compiled.Parse("2025-10-08 09:33:13.1234 message")
	assert.EqualError(t, err, `timestamp does not match layout "2006-01-02 15:04:05"`)

	_, err = compiled.Parse("2025-10-08 9:33:13 message")
	assert.EqualError(t, err, `timestamp does not match layout "2006-01-02 15:04:05"`)
}

func TestTimestampFormatCompileRejectsEmptyLayout(t *testing.T) {
	_, err := (TimestampFormat{}).Compile()
	assert.EqualError(t, err, "timestamp layout must not be empty")
}

func TestTimestampFormatParseShortensMalformedInputInError(t *testing.T) {
	tests := []struct {
		format TimestampFormat
		input  string
		want   string
	}{
		{
			format: TimestampFormat{Layout: "2006-01-02 15:04:05"},
			input:  strings.Repeat("\x00", 100) + " " + strings.Repeat("\x00", 100),
			want:   strings.Repeat("\x00", 28) + "...",
		},
		{
			format: TimestampFormat{Layout: "Jan _2 15:04:05"},
			input:  strings.Repeat("\x00", 100) + " x y",
			want:   strings.Repeat("\x00", 22) + "...",
		},
	}

	for _, tc := range tests {
		compiled, err := tc.format.Compile()
		if !assert.NoError(t, err) {
			continue
		}
		_, err = compiled.Parse(tc.input)
		parseErr, ok := err.(*time.ParseError)
		if !ok {
			t.Fatalf("expected *time.ParseError, got %T: %v", err, err)
		}

		assert.Equal(t, tc.want, parseErr.Value)
	}
}

func TestTimestampFormatParseShortensTooFewFieldsError(t *testing.T) {
	format := TimestampFormat{Layout: "Jan 02 15:04:05"}
	compiled, err := format.Compile()
	assert.NoError(t, err)
	_, err = compiled.Parse(strings.Repeat("\x00", 1000))

	assert.Error(t, err)
	maxRunes := len(format.Layout) * 3 / 2
	assert.Contains(t, err.Error(), strings.Repeat("\\x00", maxRunes)+"...")
	assert.NotContains(t, err.Error(), strings.Repeat("\\x00", maxRunes+1))
}

type timeDescrTestCase struct {
	name      string
	format    TimestampFormat
	expected  *TimeFormatDescr
	expectErr string
}

func compileTS(format TimestampFormat) CompiledTimestampFormat {
	compiled, err := format.Compile()
	if err != nil {
		panic(err)
	}
	return *compiled
}

func TestGenerateTimeDescrGeneratorOnlyCases(t *testing.T) {
	tests := []timeDescrTestCase{

		{
			name:      "Seconds are in between, unsupported",
			format:    TimestampFormat{Layout: "15:04:05 Jan _2 2006"},
			expectErr: "seconds are in between of month, day, hour and min; can't extract MinuteKey",
		},
		{
			name:   "Non-fixed length numeric day is zero-padded for indexing",
			format: TimestampFormat{Layout: "Jan 2 15:04:05"},
			expected: &TimeFormatDescr{
				TimestampFormat: compileTS(TimestampFormat{Layout: "Jan 2 15:04:05"}),
				MinuteKeyLayout: "Jan 2 15:04",
				AWKExpr: TimeFormatAWKExpr{
					Month:     "monthByName[$1]",
					Year:      "yearByMonth[month]",
					Day:       `((length($2) == 1) ? "0" $2 : $2)`,
					HHMM:      "substr($3, 1, 5)",
					MinuteKey: `$1 " " $2 " " substr($3, 1, 5)`,
				},
			},
		},
		{
			name:   "Non-fixed length numeric month and day are zero-padded for indexing",
			format: TimestampFormat{Layout: "1 2 15:04:05"},
			expected: &TimeFormatDescr{
				TimestampFormat: compileTS(TimestampFormat{Layout: "1 2 15:04:05"}),
				MinuteKeyLayout: "1 2 15:04",
				AWKExpr: TimeFormatAWKExpr{
					Month:     `((length($1) == 1) ? "0" $1 : $1)`,
					Year:      "yearByMonth[month]",
					Day:       `((length($2) == 1) ? "0" $2 : $2)`,
					HHMM:      "substr($3, 1, 5)",
					MinuteKey: `$1 " " $2 " " substr($3, 1, 5)`,
				},
			},
		},
		{
			name:   "Minute precision fields",
			format: TimestampFormat{Layout: "Jan 02 15:04"},
			expected: &TimeFormatDescr{
				TimestampFormat: compileTS(TimestampFormat{Layout: "Jan 02 15:04"}),
				MinuteKeyLayout: "Jan 02 15:04",
				AWKExpr: TimeFormatAWKExpr{
					Month:     "monthByName[$1]",
					Year:      "yearByMonth[month]",
					Day:       "$2",
					HHMM:      "$3",
					MinuteKey: `$1 " " $2 " " $3`,
				},
			},
		},
		{
			name:   "Variable day with fixed suffix",
			format: TimestampFormat{Layout: "Jan 2foo 15:04:05"},
			expected: &TimeFormatDescr{
				TimestampFormat: compileTS(TimestampFormat{Layout: "Jan 2foo 15:04:05"}),
				MinuteKeyLayout: "Jan 2foo 15:04",
				AWKExpr: TimeFormatAWKExpr{
					Month:     "monthByName[$1]",
					Year:      "yearByMonth[month]",
					Day:       "substr($2, 1, length($2) - 3)",
					HHMM:      "substr($3, 1, 5)",
					MinuteKey: `$1 " " $2 " " substr($3, 1, 5)`,
				},
			},
		},
		{
			name:   "Variable day with fixed prefix",
			format: TimestampFormat{Layout: "Jan foo2 15:04:05"},
			expected: &TimeFormatDescr{
				TimestampFormat: compileTS(TimestampFormat{Layout: "Jan foo2 15:04:05"}),
				MinuteKeyLayout: "Jan foo2 15:04",
				AWKExpr: TimeFormatAWKExpr{
					Month:     "monthByName[$1]",
					Year:      "yearByMonth[month]",
					Day:       "substr($2, 4)",
					HHMM:      "substr($3, 1, 5)",
					MinuteKey: `$1 " " $2 " " substr($3, 1, 5)`,
				},
			},
		},
		{
			name:   "Variable day with fixed suffix in minute key",
			format: TimestampFormat{Layout: "foo2-Jan 15:04:05"},
			expected: &TimeFormatDescr{
				TimestampFormat: compileTS(TimestampFormat{Layout: "foo2-Jan 15:04:05"}),
				MinuteKeyLayout: "2-Jan 15:04",
				AWKExpr: TimeFormatAWKExpr{
					Month:     "monthByName[substr($1, 6, 3)]",
					Year:      "yearByMonth[month]",
					Day:       "substr($1, 4, length($1) - 3 - 4)",
					HHMM:      "substr($2, 1, 5)",
					MinuteKey: `substr($1, 4) " " substr($2, 1, 5)`,
				},
			},
		},
		{
			name:      "Multiple variable components in one field",
			format:    TimestampFormat{Layout: "Jan 2foo4 15:04:05"},
			expectErr: `unsupported layout: multiple variable-length components in field "2foo4"`,
		},
		{
			name:      "Variable component at end of final field",
			format:    TimestampFormat{Layout: "Jan 02 15:04:05 2", HasTrailingChars: true},
			expectErr: `unsupported layout: variable-length component at end of final field "2"`,
		},
		{
			name:   "Variable component at end of final field without trailing characters",
			format: TimestampFormat{Layout: "Jan 02 15:04:05 2"},
			expected: &TimeFormatDescr{
				TimestampFormat: compileTS(TimestampFormat{Layout: "Jan 02 15:04:05 2"}),
				MinuteKeyLayout: "Jan 02 15:04",
				AWKExpr: TimeFormatAWKExpr{
					Month:     "monthByName[$1]",
					Year:      "yearByMonth[month]",
					Day:       "$2",
					HHMM:      "substr($3, 1, 5)",
					MinuteKey: `$1 " " $2 " " substr($3, 1, 5)`,
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := GenerateTimeDescr(tc.format)

			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected, result)
			}
		})
	}

}

// TestNumericTimezoneLayouts covers every numeric timezone layout supported by
// Go, checking both Go parsing and the AWK timezone expression. It also
// documents that named MST-style zones are intentionally rejected because AWK
// cannot reliably convert arbitrary abbreviations to absolute offsets.
func TestNumericTimezoneLayouts(t *testing.T) {
	tests := []struct {
		layout         string
		input          string
		awk            string
		wantErr        string
		wantCompileErr string
	}{
		{layout: "2006-01-02T15:04:05Z07:00", input: "2025-10-08T09:33:13+02:00", awk: "substr($1, 20)"},
		{layout: "2006-01-02T15:04:05Z0700", input: "2025-10-08T09:33:13+0200", awk: "substr($1, 20)"},
		{layout: "2006-01-02T15:04:05Z07", input: "2025-10-08T09:33:13+02", awk: "substr($1, 20)"},
		{layout: "2006-01-02T15:04:05-07:00", input: "2025-10-08T09:33:13+02:00", awk: "substr($1, 20)"},
		{layout: "2006-01-02T15:04:05-0700", input: "2025-10-08T09:33:13+0200", awk: "substr($1, 20)"},
		{layout: "2006-01-02T15:04:05-07", input: "2025-10-08T09:33:13+02", awk: "substr($1, 20)"},
		{layout: "2006-01-02T15:04:05Z07:00:00", wantCompileErr: `unsupported timezone layout "Z07:00:00": second-resolution offsets are not supported`},
		{layout: "2006-01-02T15:04:05Z070000", wantCompileErr: `unsupported timezone layout "Z070000": second-resolution offsets are not supported`},
		{layout: "2006-01-02T15:04:05-07:00:00", wantCompileErr: `unsupported timezone layout "-07:00:00": second-resolution offsets are not supported`},
		{layout: "2006-01-02T15:04:05-070000", wantCompileErr: `unsupported timezone layout "-070000": second-resolution offsets are not supported`},
		{layout: "2006-01-02T15:04:05 MST", input: "2025-10-08T09:33:13 MST", wantErr: `unsupported layout: named timezone "MST" cannot be parsed reliably by awk`},
	}

	for _, tc := range tests {
		t.Run(tc.layout, func(t *testing.T) {
			format := TimestampFormat{Layout: tc.layout}
			compiled, err := format.Compile()
			if tc.wantCompileErr != "" {
				assert.EqualError(t, err, tc.wantCompileErr)
				return
			}
			assert.NoError(t, err)
			if err == nil {
				_, err = compiled.Parse(tc.input)
				assert.NoError(t, err)
			}

			descr, err := GenerateTimeDescr(format)
			if tc.wantErr != "" {
				assert.EqualError(t, err, tc.wantErr)
				return
			}
			assert.NoError(t, err)
			if err == nil {
				assert.Equal(t, tc.awk, descr.AWKExpr.Timezone)
			}
		})
	}
}

func TestUnsupportedGoLayoutsForNerdlog(t *testing.T) {
	tests := []struct {
		layout  string
		wantErr string
	}{
		{
			layout:  "Jan 02 3:04:05 PM",
			wantErr: "unsupported layout: 12-hour clocks with AM/PM are not supported",
		},
		{
			layout:  "06-01-02T15:04:05-0700",
			wantErr: "unsupported layout: two-digit years are not supported",
		},
		{
			layout:  "060102 15:04:05",
			wantErr: "unsupported layout: two-digit years are not supported",
		},
		{
			layout:  "02 15:04:05",
			wantErr: `unsupported time layout "02 15:04:05": missing required components: month`,
		},
		{
			layout:  "Monday Jan 2 15:04:05 2006",
			wantErr: "unsupported layout: full weekday names are not supported",
		},
		{
			layout:  "2006-01-02 15:04:05 MST",
			wantErr: `unsupported layout: named timezone "MST" cannot be parsed reliably by awk`,
		},
		{
			layout:  "2006-01-02T15:04:05Z07:00:00",
			wantErr: `unsupported timezone layout "Z07:00:00": second-resolution offsets are not supported`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.layout, func(t *testing.T) {
			_, err := GenerateTimeDescr(TimestampFormat{Layout: tc.layout})
			assert.EqualError(t, err, tc.wantErr)
		})
	}
}
