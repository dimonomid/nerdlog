package core

import (
	"testing"

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
			wantFormat:  nil,
			wantErr:     "unable to detect timestamp format",
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
			name:        "Apache bracketed microseconds low values",
			logFilename: "/var/log/syslog",
			logLine:     "[Wed Oct 08 09:33:13.123456 2025] Starting server",
			wantFormat:  &TimestampFormat{Layout: "[Mon Jan 02 15:04:05.999999 2006]"},
		},
		{
			name:        "Apache bracketed microseconds high values",
			logFilename: "/var/log/syslog",
			logLine:     "[Tue Nov 18 11:33:13.123456 2025] Starting server",
			wantFormat:  &TimestampFormat{Layout: "[Mon Jan 02 15:04:05.999999 2006]"},
		},
		{
			name:        "Apache bracketed seconds low values",
			logFilename: "/var/log/syslog",
			logLine:     "[Wed Oct 08 09:33:13 2025] Starting server",
			wantFormat:  &TimestampFormat{Layout: "[Mon Jan 02 15:04:05.999999 2006]"},
		},
		{
			name:        "Apache bracketed seconds high values",
			logFilename: "/var/log/syslog",
			logLine:     "[Tue Nov 18 11:33:13 2025] Starting server",
			wantFormat:  &TimestampFormat{Layout: "[Mon Jan 02 15:04:05.999999 2006]"},
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
			format, err := DetectTimestampFormat(tc.logFilename, tc.logLine)
			if tc.wantErr != "" {
				assert.EqualError(t, err, tc.wantErr)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.wantFormat, format)
		})
	}
}

type timeDescrTestCase struct {
	name      string
	format    TimestampFormat
	expected  *TimeFormatDescr
	expectErr string
}

func TestGenerateTimeDescr(t *testing.T) {
	tests := []timeDescrTestCase{
		{
			name:   "Traditional syslog",
			format: TimestampFormat{Layout: "Jan _2 15:04:05"},
			expected: &TimeFormatDescr{
				TimestampFormat: TimestampFormat{Layout: "Jan _2 15:04:05"},
				MinuteKeyLayout: "Jan _2 15:04",
				AWKExpr: TimeFormatAWKExpr{
					Month:     "monthByName[substr($0, 1, 3)]",
					Year:      "yearByMonth[month]",
					Day:       `(substr($0, 5, 1) == " ") ? "0" substr($0, 6, 1) : substr($0, 5, 2)`,
					HHMM:      "substr($0, 8, 5)",
					MinuteKey: "substr($0, 1, 12)",
				},
			},
		},
		{
			name:   "ISO8601 with microseconds and timezone",
			format: TimestampFormat{Layout: "2006-01-02T15:04:05.000000Z07:00"},
			expected: &TimeFormatDescr{
				TimestampFormat: TimestampFormat{Layout: "2006-01-02T15:04:05.000000Z07:00"},
				MinuteKeyLayout: "01-02T15:04Z07:00",
				AWKExpr: TimeFormatAWKExpr{
					Month:     "substr($0, 6, 2)",
					Year:      "substr($0, 1, 4)",
					Day:       "substr($0, 9, 2)",
					HHMM:      "substr($0, 12, 5)",
					MinuteKey: "substr($0, 6, 11)",
					Timezone:  `((substr($0, 27, 1) == "Z") ? "Z" : substr($0, 27, 6))`,
				},
			},
		},
		{
			name:   "Custom 24-hour format",
			format: TimestampFormat{Layout: "2006-01-02 15:04:05"},
			expected: &TimeFormatDescr{
				TimestampFormat: TimestampFormat{Layout: "2006-01-02 15:04:05"},
				MinuteKeyLayout: "01-02 15:04",
				AWKExpr: TimeFormatAWKExpr{
					Month:     "substr($0, 6, 2)",
					Year:      "substr($0, 1, 4)",
					Day:       "substr($0, 9, 2)",
					HHMM:      "substr($0, 12, 5)",
					MinuteKey: "substr($0, 6, 11)",
				},
			},
		},
		{
			name:   "ISO8601 without timezone",
			format: TimestampFormat{Layout: "2006-01-02T15:04:05"},
			expected: &TimeFormatDescr{
				TimestampFormat: TimestampFormat{Layout: "2006-01-02T15:04:05"},
				MinuteKeyLayout: "01-02T15:04",
				AWKExpr: TimeFormatAWKExpr{
					Month:     "substr($0, 6, 2)",
					Year:      "substr($0, 1, 4)",
					Day:       "substr($0, 9, 2)",
					HHMM:      "substr($0, 12, 5)",
					MinuteKey: "substr($0, 6, 11)",
				},
			},
		},
		{
			name:      "Seconds are in between, unsupported",
			format:    TimestampFormat{Layout: "15:04:05 Jan _2 2006"},
			expectErr: "seconds are in between of month, day, hour and min; can't extract MinuteKey",
		},
		{
			name:      "Non-fixed length (the date Jan 2 can also be Jan 12), unsupported",
			format:    TimestampFormat{Layout: "Jan 2 15:04:05"},
			expectErr: "unsupported layout: required components not found",
		},
		{
			name:   "ISO8601 with microseconds and numeric timezone",
			format: TimestampFormat{Layout: "2006-01-02T15:04:05.000000-0700"},
			expected: &TimeFormatDescr{
				TimestampFormat: TimestampFormat{Layout: "2006-01-02T15:04:05.000000-0700"},
				MinuteKeyLayout: "01-02T15:04-0700",
				AWKExpr: TimeFormatAWKExpr{
					Month:     "substr($0, 6, 2)",
					Year:      "substr($0, 1, 4)",
					Day:       "substr($0, 9, 2)",
					HHMM:      "substr($0, 12, 5)",
					MinuteKey: "substr($0, 6, 11)",
					Timezone:  "substr($0, 27, 5)",
				},
			},
		},
		{
			name:   "RFC3339 with milliseconds",
			format: TimestampFormat{Layout: "2006-01-02T15:04:05.000Z07:00"},
			expected: &TimeFormatDescr{
				TimestampFormat: TimestampFormat{Layout: "2006-01-02T15:04:05.000Z07:00"},
				MinuteKeyLayout: "01-02T15:04Z07:00",
				AWKExpr: TimeFormatAWKExpr{
					Month:     "substr($0, 6, 2)",
					Year:      "substr($0, 1, 4)",
					Day:       "substr($0, 9, 2)",
					HHMM:      "substr($0, 12, 5)",
					MinuteKey: "substr($0, 6, 11)",
					Timezone:  `((substr($0, 24, 1) == "Z") ? "Z" : substr($0, 24, 6))`,
				},
			},
		},
		{
			name:   "Apache access log format",
			format: TimestampFormat{Layout: "02/Jan/2006:15:04:05 -0700"},
			expected: &TimeFormatDescr{
				TimestampFormat: TimestampFormat{Layout: "02/Jan/2006:15:04:05 -0700"},
				MinuteKeyLayout: "02/Jan/2006:15:04 -0700",
				AWKExpr: TimeFormatAWKExpr{
					Month:     "monthByName[substr($0, 4, 3)]",
					Year:      "substr($0, 8, 4)",
					Day:       "substr($0, 1, 2)",
					HHMM:      "substr($0, 13, 5)",
					MinuteKey: "substr($0, 1, 17)",
					Timezone:  `" " substr($0, 22, 5)`,
				},
			},
		},
		{
			name:   "Slash-separated date",
			format: TimestampFormat{Layout: "2006/01/02 15:04:05"},
			expected: &TimeFormatDescr{
				TimestampFormat: TimestampFormat{Layout: "2006/01/02 15:04:05"},
				MinuteKeyLayout: "01/02 15:04",
				AWKExpr: TimeFormatAWKExpr{
					Month:     "substr($0, 6, 2)",
					Year:      "substr($0, 1, 4)",
					Day:       "substr($0, 9, 2)",
					HHMM:      "substr($0, 12, 5)",
					MinuteKey: "substr($0, 6, 11)",
				},
			},
		},
		{
			name:      "Weekday date format, unsupported",
			format:    TimestampFormat{Layout: "Mon Jan 2 15:04:05 2006"},
			expectErr: "unsupported layout: required components not found",
		},
		{
			name:   "Hyphenated month date",
			format: TimestampFormat{Layout: "02-Jan-2006 15:04:05"},
			expected: &TimeFormatDescr{
				TimestampFormat: TimestampFormat{Layout: "02-Jan-2006 15:04:05"},
				MinuteKeyLayout: "02-Jan-2006 15:04",
				AWKExpr: TimeFormatAWKExpr{
					Month:     "monthByName[substr($0, 4, 3)]",
					Year:      "substr($0, 8, 4)",
					Day:       "substr($0, 1, 2)",
					HHMM:      "substr($0, 13, 5)",
					MinuteKey: "substr($0, 1, 17)",
				},
			},
		},
		{
			name:   "Month day format",
			format: TimestampFormat{Layout: "Jan 02 15:04:05"},
			expected: &TimeFormatDescr{
				TimestampFormat: TimestampFormat{Layout: "Jan 02 15:04:05"},
				MinuteKeyLayout: "Jan 02 15:04",
				AWKExpr: TimeFormatAWKExpr{
					Month:     "monthByName[substr($0, 1, 3)]",
					Year:      "yearByMonth[month]",
					Day:       "substr($0, 5, 2)",
					HHMM:      "substr($0, 8, 5)",
					MinuteKey: "substr($0, 1, 12)",
				},
			},
		},
		{
			name:   "Apache format with microseconds",
			format: TimestampFormat{Layout: "[Mon Jan 02 15:04:05.999999 2006]"},
			expected: &TimeFormatDescr{
				TimestampFormat: TimestampFormat{Layout: "[Mon Jan 02 15:04:05.999999 2006]"},
				MinuteKeyLayout: "Jan 02 15:04",
				AWKExpr: TimeFormatAWKExpr{
					Month:     "monthByName[substr($0, 6, 3)]",
					Year:      "substr($0, 29, 4)",
					Day:       "substr($0, 10, 2)",
					HHMM:      "substr($0, 13, 5)",
					MinuteKey: "substr($0, 6, 12)",
				},
			},
		},
		{
			name:   "Apache format without microseconds",
			format: TimestampFormat{Layout: "[Mon Jan 02 15:04:05 2006]"},
			expected: &TimeFormatDescr{
				TimestampFormat: TimestampFormat{Layout: "[Mon Jan 02 15:04:05 2006]"},
				MinuteKeyLayout: "Jan 02 15:04",
				AWKExpr: TimeFormatAWKExpr{
					Month:     "monthByName[substr($0, 6, 3)]",
					Year:      "substr($0, 22, 4)",
					Day:       "substr($0, 10, 2)",
					HHMM:      "substr($0, 13, 5)",
					MinuteKey: "substr($0, 6, 12)",
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
