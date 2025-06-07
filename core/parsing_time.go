package core

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/juju/errors"
)

// TimeFormatDescr contains all data necessary for Nerdlog to parse timestamps
// in the particular logstream. It contains info for both the awk script and Go
// client.
type TimeFormatDescr struct {
	// TimestampLayout is a Go-style time layout which should parse the entire
	// timestamp in the log line, e.g. "Jan _2 15:04:05" or
	// "2006-01-02T15:04:05.000000Z07:00".
	TimestampLayout string

	// AWKExpr contains all the awk expressions which will be used by the
	// nerdlog_agent.sh script to get the time components from logs.
	AWKExpr TimeFormatAWKExpr
}

// TimeFormatAWKExpr contains all the awk expressions which will be used by
// the nerdlog_agent.sh script to get the time components from logs.
type TimeFormatAWKExpr struct {
	// Month is an AWK expression to get month number as a string, from "01" to
	// "12". It may use `monthByName`, which is a map from a 3-char string like
	// "Jan" to the corresponding string like "01".
	//
	// So e.g. for the traditional syslog format "Jan _2 15:04:05", it should be
	// "monthByName[$1]".
	//
	// For the format "2006-01-02T15:04:05.000000Z07:00", it should rather be
	// "substr($0, 6, 2)".
	Month string

	// Year is an AWK expression to get year string like "2024". If the format
	// doesn't contain the year, it may use the already-computed `month` and
	// `yearByMonth`, which is a mapping from the month (from "01" to "12") to
	// the corresponding inferred year.
	//
	// So e.g. for the traditional syslog format "Jan _2 15:04:05", it should be
	// "yearByMonth[month]".
	//
	// For the format "2006-01-02T15:04:05.000000Z07:00", it should rather be
	// "substr($0, 1, 4)".
	Year string

	// Day is an AWK expression to get the day string like "05"; note the leading
	// zero, it's important (TODO: make it possible to support spaces; it'd mean
	// having spaces in the index and in the --from and --to args, which has issues)
	//
	// So e.g. for the traditional syslog format "Jan _2 15:04:05", it should be
	// `(length($2) == 1) ? "0" $2 : $2`.
	//
	// For the format "2006-01-02T15:04:05.000000Z07:00", it should rather be
	// "substr($0, 9, 2)".
	Day string

	// HHMM is an AWK expression to get the hours and minutes string like "14:38".
	//
	// So e.g. for the traditional syslog format "Jan _2 15:04:05", it should be
	// "substr($3, 1, 5)".
	//
	// For the format "2006-01-02T15:04:05.000000Z07:00", it should rather be
	// "substr($0, 12, 5)".
	HHMM string
}

func GetTimeFormatDescrFromLogLines(logLines []string) (*TimeFormatDescr, error) {
	if len(logLines) == 0 {
		return nil, errors.Errorf("no logs, can't detect time format")
	}

	descrs := make([]*TimeFormatDescr, 0, len(logLines))

	for i, line := range logLines {
		layout := DetectTimeLayout(line)
		if layout == "" {
			return nil, errors.Errorf("unable to detect time format from %q", line)
		}

		timeDescr, err := GenerateTimeDescr(layout)
		if err != nil {
			return nil, errors.Trace(err)
		}

		if i > 0 {
			if descrs[0].TimestampLayout != timeDescr.TimestampLayout {
				return nil, errors.Errorf(
					"log lines have different formats: %s and %s",
					descrs[0].TimestampLayout,
					timeDescr.TimestampLayout,
				)
			}
		}

		descrs = append(descrs, timeDescr)
	}

	return descrs[0], nil
}

// DetectTimeLayout tries to detect a time format from a log line.
//
// TODO: it's pretty simplistic and could be improved, even to avoid having
// a predefined set of known formats, but good enough for now.
func DetectTimeLayout(logLine string) string {
	var knownFormats = []string{
		"Jan _2 15:04:05",                  // Traditional rsyslog format without year
		"2006-01-02T15:04:05.000000Z07:00", // ISO8601, used in modern rsyslog by default
		"2006-01-02T15:04:05.000000-0700",  // Used by older versions of journalctl with --output=short-iso-precise
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05.000Z07:00",
		"02/Jan/2006:15:04:05 -0700",
		"2006/01/02 15:04:05",
		"Mon Jan 2 15:04:05 2006",
		"02-Jan-2006 15:04:05",
		"Jan 02 15:04:05",

		// Some variations of Apache HTTP Server logs
		"[Mon Jan 02 15:04:05.999999 2006]",
		"[Mon Jan 02 15:04:05 2006]",
	}

	for _, layout := range knownFormats {
		for curLen := 5; curLen <= len(layout) && curLen <= len(logLine); curLen++ {
			sub := logLine[:curLen]
			_, err := time.Parse(layout, sub)
			if err == nil {
				return layout
			}
		}
	}
	return ""
}

// GenerateTimeDescr takes a Go-style time layout, and returns the full time
// format descriptor to be used for parsing all logs.
func GenerateTimeDescr(layout string) (*TimeFormatDescr, error) {
	// Find index positions of time components
	partInfo := map[string]*indexAndLength{
		"year":   indexAndLengthOfTimeComponent(layout, "2006"),
		"month":  indexAndLengthOfTimeComponent(layout, "Jan", "01", "_1"),
		"day":    indexAndLengthOfTimeComponent(layout, "02", "_2"),
		"hhmm":   indexAndLengthOfTimeComponent(layout, "15:04"),
		"second": indexAndLengthOfTimeComponent(layout, "05"),
	}

	// Validate that required fields exist
	if partInfo["hhmm"] == nil || partInfo["day"] == nil || partInfo["month"] == nil {
		return nil, errors.New("unsupported layout: required components not found")
	}

	// Helper to generate substr($0, x, y)
	substr := func(start, length int) string {
		return "substr($0, " + itoa(start+1) + ", " + itoa(length) + ")"
	}

	// Like substr for 2-digit numbers like month or day, but replaces the first
	// space with "0".
	//
	// TODO: ideally support spaces in day and month (which would mean supporting
	// spaces in index as well as --from and --to), and remove this.
	substrCheckingForSpace := func(start, length int) string {
		if length == 2 && layout[start] == '_' {
			return fmt.Sprintf(
				`(%s == " ") ? "0" %s : %s`,
				substr(start, 1),
				substr(start+1, 1),
				substr(start, 2),
			)
		}

		return substr(start, length)
	}

	// Extract AWK expressions
	awk := TimeFormatAWKExpr{
		// Year will be set later
		Month: substrCheckingForSpace(partInfo["month"].index, partInfo["month"].length),
		Day:   substrCheckingForSpace(partInfo["day"].index, partInfo["day"].length),
		HHMM:  substr(partInfo["hhmm"].index, partInfo["hhmm"].length),
	}

	if partInfo["year"] != nil {
		awk.Year = substr(partInfo["year"].index, partInfo["year"].length)
	} else {
		awk.Year = "yearByMonth[month]"
	}

	// If the month is not a number but a string like "Jan", use the mapping.
	if partInfo["month"].length == 3 {
		awk.Month = fmt.Sprintf("monthByName[%s]", awk.Month)
	}

	return &TimeFormatDescr{
		TimestampLayout: layout,
		AWKExpr:         awk,
	}, nil
}

// indexAndLengthOfTimeComponent takes one or more timestamp components, such as "2006",
// "01", "_1", "1" etc, and returns the index of the given component in the
// given string s, not preceded or followed by any number or "_".
func indexAndLengthOfTimeComponent(s string, components ...string) *indexAndLength {
	// Loop over each component provided
	for _, comp := range components {
		// Create a regular expression that matches the component, ensuring it is not
		// preceded or followed by digits or underscores
		re := fmt.Sprintf(`[^0-9_]?%s[^0-9_]?`, regexp.QuoteMeta(comp))

		// Compile the regex
		rgx := regexp.MustCompile(re)

		// Find the first occurrence of the component
		loc := rgx.FindStringIndex(s)
		if loc != nil {
			// Return the index of the first match, also removing the potentially
			// preceding non-digit rune.
			return &indexAndLength{
				index:  loc[0] + strings.Index(s[loc[0]:], comp),
				length: len(comp),
			}
		}
	}
	// Return -1 if no component is found
	return nil
}

type indexAndLength struct {
	index  int
	length int
}

func itoa(i int) string {
	return strconv.Itoa(i)
}

func minIndex(vals ...*indexAndLength) *indexAndLength {
	curMin := 999
	curMinIdx := -1
	for i, v := range vals {
		if v.index < curMin {
			curMin = v.index
			curMinIdx = i
		}
	}
	return vals[curMinIdx]
}

func maxIndex(vals ...*indexAndLength) *indexAndLength {
	curMax := -1
	curMaxIdx := -1
	for i, v := range vals {
		if v.index > curMax {
			curMax = v.index
			curMaxIdx = i
		}
	}
	return vals[curMaxIdx]
}
