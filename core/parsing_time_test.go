package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type detectTimeTestCase struct {
	name       string
	logLine    string
	wantLayout string
}

func TestDetectTimeFormat(t *testing.T) {
	testCases := []detectTimeTestCase{
		{
			name:       "rsyslog no year, 1-digit",
			logLine:    "Apr  8 01:02:03 somehost systemd[1]: Started something.",
			wantLayout: "Jan _2 15:04:05",
		},
		{
			name:       "rsyslog no year, 2-digit",
			logLine:    "Apr 18 01:02:03 somehost systemd[1]: Started something.",
			wantLayout: "Jan _2 15:04:05",
		},
		{
			name:       "ISO8601 non-UTC full with microseconds",
			logLine:    "2024-04-19T14:23:45.123456+02:00 INFO something happened",
			wantLayout: "2006-01-02T15:04:05.000000Z07:00",
		},
		{
			name:       "ISO8601 UTC full with microseconds",
			logLine:    "2024-04-19T14:23:45.123456Z INFO something happened",
			wantLayout: "2006-01-02T15:04:05.000000Z07:00",
		},
		{
			name:       "RFC3339",
			logLine:    "2024-04-19T14:23:45+02:00 Starting server",
			wantLayout: "2006-01-02T15:04:05Z07:00",
		},
		{
			name:       "older journalctl with --output=short-iso-precise",
			logLine:    "2025-05-11T21:33:13.924352+0200 Starting server",
			wantLayout: "2006-01-02T15:04:05.000000-0700",
		},
		{
			name:       "No timestamp in line",
			logLine:    "This is a log line without a timestamp.",
			wantLayout: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			layout := DetectTimeLayout(tc.logLine)
			assert.Equal(t, tc.wantLayout, layout)
		})
	}
}

type timeDescrTestCase struct {
	name      string
	layout    string
	expected  *TimeFormatDescr
	expectErr string
}

func TestGenerateTimeDescr(t *testing.T) {
	tests := []timeDescrTestCase{
		{
			name:   "Traditional syslog",
			layout: "Jan _2 15:04:05",
			expected: &TimeFormatDescr{
				TimestampLayout: "Jan _2 15:04:05",
				AWKExpr: TimeFormatAWKExpr{
					Month: "monthByName[substr($0, 1, 3)]",
					Year:  "yearByMonth[month]",
					Day:   `(substr($0, 5, 1) == " ") ? "0" substr($0, 6, 1) : substr($0, 5, 2)`,
					HHMM:  "substr($0, 8, 5)",
				},
			},
		},
		{
			name:   "ISO8601 with microseconds and timezone",
			layout: "2006-01-02T15:04:05.000000Z07:00",
			expected: &TimeFormatDescr{
				TimestampLayout: "2006-01-02T15:04:05.000000Z07:00",
				AWKExpr: TimeFormatAWKExpr{
					Month: "substr($0, 6, 2)",
					Year:  "substr($0, 1, 4)",
					Day:   "substr($0, 9, 2)",
					HHMM:  "substr($0, 12, 5)",
				},
			},
		},
		{
			name:   "Custom 24-hour format",
			layout: "2006-01-02 15:04:05",
			expected: &TimeFormatDescr{
				TimestampLayout: "2006-01-02 15:04:05",
				AWKExpr: TimeFormatAWKExpr{
					Month: "substr($0, 6, 2)",
					Year:  "substr($0, 1, 4)",
					Day:   "substr($0, 9, 2)",
					HHMM:  "substr($0, 12, 5)",
				},
			},
		},
		{
			name:   "ISO8601 without timezone",
			layout: "2006-01-02T15:04:05",
			expected: &TimeFormatDescr{
				TimestampLayout: "2006-01-02T15:04:05",
				AWKExpr: TimeFormatAWKExpr{
					Month: "substr($0, 6, 2)",
					Year:  "substr($0, 1, 4)",
					Day:   "substr($0, 9, 2)",
					HHMM:  "substr($0, 12, 5)",
				},
			},
		},
		{
			name:      "Non-fixed length (the date Jan 2 can also be Jan 12), unsupported",
			layout:    "Jan 2 15:04:05",
			expectErr: "unsupported layout: required components not found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := GenerateTimeDescr(tc.layout)

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
