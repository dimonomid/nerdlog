package core

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/juju/errors"
)

// TimestampFormat describes a timestamp position and layout in the log file.
//
// Layout uses Go time-layout syntax, but not every Go layout is supported.
// The layout must work for finding the timestamp, parsing it, and building the
// AWK expressions used for indexing. It must include a month, day, and
// 24-hour hour/minute value. It may also include a year, seconds, fractions,
// a numeric timezone, or the fixed-width weekday form "Mon". Full weekday
// names, named timezones, 12-hour clocks, and timezone offsets with seconds
// are not supported.
//
// The agent runs awk in byte mode, so these positions have identical meanings
// in Go and awk.
type TimestampFormat struct {
	// Layout is a Go-style time layout which should parse the entire timestamp
	// in the log line, e.g. "Jan _2 15:04:05" or
	// "2006-01-02T15:04:05.000000Z07:00". The starting position where we should
	// look for this Layout is defined by StartFieldIdx and StartSubstrOffset
	// below.
	Layout string

	// StartFieldIdx identifies index of the first field (as per strings.Fields)
	// where the timestamp starts as per Layout (after StartSubstrOffset though).
	StartFieldIdx int
	// StartSubstrOffset is a byte position within the field at StartFieldIdx at
	// which the timestamp starts as per Layout.
	StartSubstrOffset int

	// HasTrailingChars says that the timestamp may be followed immediately by
	// other characters in the same field, such as "Z[INFO]". When false, the
	// timestamp must end at the field boundary.
	HasTrailingChars bool
}

// CompiledTimestampFormat contains the regexp-derived data needed to parse a
// timestamp format repeatedly. It is normally stored in TimeFormatDescr, so
// each logstream can reuse its own compiled parser without a global cache.
type CompiledTimestampFormat struct {
	TimestampFormat

	// prefixRegexp identifies how much of the normalized input belongs to the
	// timestamp. We need this because time.Parse requires the whole input to
	// match, while log lines may have trailing metadata directly attached to
	// the timestamp, such as "2025-10-08T09:33:13+02:00[INFO]".
	prefixRegexp *regexp.Regexp

	// fieldCount is the number of whitespace-separated layout fields. It is
	// reused for every line, so compute it when the format is compiled.
	fieldCount int
}

// Compile prepares a timestamp format for repeated parsing.
func (f TimestampFormat) Compile() (*CompiledTimestampFormat, error) {
	fieldCount := len(strings.Fields(f.Layout))
	if fieldCount == 0 {
		return nil, errors.New("timestamp layout must not be empty")
	}
	for _, layout := range []string{"Z07:00:00", "Z070000", "-07:00:00", "-070000"} {
		if strings.Contains(f.Layout, layout) {
			return nil, errors.Errorf("unsupported timezone layout %q: second-resolution offsets are not supported", layout)
		}
	}
	pattern := timestampLayoutRegexp(f.Layout, f.HasTrailingChars)
	prefixRegexp, err := regexp.Compile("^" + pattern)
	if err != nil {
		return nil, errors.Annotatef(err, "compiling timestamp layout regexp")
	}
	return &CompiledTimestampFormat{
		TimestampFormat: f,
		prefixRegexp:    prefixRegexp,
		fieldCount:      fieldCount,
	}, nil
}

// shortenTimestampParseError replaces only the input values embedded in a
// time.ParseError. The original value has already been supplied to the parser;
// this changes diagnostics only.
func shortenTimestampParseError(err error, layout string) error {
	parseErr, ok := err.(*time.ParseError)
	if !ok {
		return err
	}

	shortened := *parseErr
	shortened.Value = shortenTimestampDiagnosticValue(shortened.Value, layout)
	shortened.ValueElem = shortenTimestampDiagnosticValue(shortened.ValueElem, layout)
	return &shortened
}

// shortenTimestampDiagnosticValue bounds a malformed timestamp value using a
// layout-based estimate, while keeping diagnostics globally bounded.
func shortenTimestampDiagnosticValue(value, layout string) string {
	const maxDiagnosticRunes = 128

	maxRunes := len(layout) * 3 / 2
	if maxRunes > maxDiagnosticRunes {
		maxRunes = maxDiagnosticRunes
	}
	return truncateTimestampErrorValue(value, maxRunes)
}

// truncateTimestampErrorValue keeps at most maxRunes from a value and appends
// an ellipsis when the value is longer. Timestamp layouts and regexp matches
// are rune-oriented, so truncation is done at rune boundaries as well.
func truncateTimestampErrorValue(value string, maxRunes int) string {
	if utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	return string([]rune(value)[:maxRunes]) + "..."
}

// Parse extracts and parses a timestamp using the compiled format.
func (f *CompiledTimestampFormat) Parse(line string) (time.Time, error) {
	result, err := f.timestampPrefixText(line)
	if err != nil {
		return time.Time{}, err
	}
	t, err := time.Parse(f.Layout, result.text)
	if err != nil {
		err = shortenTimestampParseError(err, f.Layout)
	}
	return t, err
}

// parseInLocationWithDetails extracts and parses a timestamp, returning the
// extracted text location even when time.ParseInLocation rejects its value.
func (f *CompiledTimestampFormat) parseInLocationWithDetails(line string, loc *time.Location) (time.Time, timestampExtractionDetails, error) {
	result, err := f.timestampPrefixText(line)
	if err != nil {
		return time.Time{}, result, err
	}
	t, err := time.ParseInLocation(f.Layout, result.text, loc)
	if err != nil {
		err = shortenTimestampParseError(err, f.Layout)
	}
	return t, result, err
}

// ParseInLocation extracts and parses a timestamp using the compiled format
// and the supplied location for layouts without an explicit timezone.
func (f *CompiledTimestampFormat) ParseInLocation(line string, loc *time.Location) (time.Time, error) {
	t, _, err := f.parseInLocationWithDetails(line, loc)
	return t, err
}

type logFieldSpan struct {
	start int
	end   int
}

// logLineFieldSpans returns byte ranges for the whitespace-delimited fields in
// line. Unlike strings.Fields, it preserves each field's original location so
// callers can modify the original line without changing unrelated spacing.
func logLineFieldSpans(line string) []logFieldSpan {
	var spans []logFieldSpan
	for i := 0; i < len(line); {
		r, size := utf8.DecodeRuneInString(line[i:])
		if unicode.IsSpace(r) {
			i += size
			continue
		}

		start := i
		i += size
		for i < len(line) {
			r, size = utf8.DecodeRuneInString(line[i:])
			if unicode.IsSpace(r) {
				break
			}
			i += size
		}
		spans = append(spans, logFieldSpan{start: start, end: i})
	}
	return spans
}

// timestampExtractionDetails describes a normalized timestamp prefix and its
// location in the original log line.
type timestampExtractionDetails struct {
	// text is the normalized timestamp text selected for parsing. "Normalized"
	// means each of its whitespaces is compressed to a single space, so it might
	// be shorter than the literal substring of the original line.
	text string

	// origStart and origEnd below are byte offsets in the original line. They
	// account for extra whitespace between timestamp fields, so
	// origEnd-origStart may be larger than len(text).

	// origStart is the timestamp's starting byte offset in the original line.
	origStart int
	// origEnd is the first byte after the timestamp in the original line.
	origEnd int
}

// timestampPrefixText takes the original log line and finds where the
// timestamp is located in it. See timestampExtractionDetails for details.
func (f *CompiledTimestampFormat) timestampPrefixText(line string) (timestampExtractionDetails, error) {
	type timestampTextSegment struct {
		normalizedStart int
		normalizedEnd   int
		originalStart   int
		originalEnd     int
	}

	format := f.TimestampFormat
	spans := logLineFieldSpans(line)
	fieldCount := f.fieldCount
	if format.StartFieldIdx < 0 || format.StartFieldIdx+fieldCount > len(spans) {
		diagnosticLine := shortenTimestampDiagnosticValue(line, format.Layout)
		return timestampExtractionDetails{}, errors.Errorf("line %q does not contain the timestamp fields", diagnosticLine)
	}

	startSpan := spans[format.StartFieldIdx]
	if format.StartSubstrOffset < 0 || format.StartSubstrOffset > startSpan.end-startSpan.start {
		return timestampExtractionDetails{}, errors.Errorf("timestamp substring offset %d is outside the starting field", format.StartSubstrOffset)
	}

	var normalized strings.Builder
	segments := make([]timestampTextSegment, 0, fieldCount)
	appendField := func(originalStart, originalEnd int) {
		normalizedStart := normalized.Len()
		normalized.WriteString(line[originalStart:originalEnd])
		segments = append(segments, timestampTextSegment{
			normalizedStart: normalizedStart,
			normalizedEnd:   normalized.Len(),
			originalStart:   originalStart,
			originalEnd:     originalEnd,
		})
	}
	appendField(startSpan.start+format.StartSubstrOffset, startSpan.end)
	for i := 1; i < fieldCount; i++ {
		span := spans[format.StartFieldIdx+i]
		normalized.WriteByte(' ')
		appendField(span.start, span.end)
	}

	normalizedText := normalized.String()
	match := f.prefixRegexp.FindStringSubmatchIndex(normalizedText)
	if match == nil {
		// The regexp defines the timestamp shapes supported by nerdlog. Go's
		// time.Parse accepts a few extra shapes, notably implicit fractional
		// seconds after a seconds field, so do not accept a successful parse
		// when the input failed this lexical check.
		if _, err := time.Parse(format.Layout, normalizedText); err == nil {
			return timestampExtractionDetails{}, errors.Errorf("timestamp does not match layout %q", format.Layout)
		}

		// For genuinely malformed values, keep passing the original text to
		// time.Parse so callers retain its useful, detailed ParseError.
		return timestampExtractionDetails{
			text:      normalizedText,
			origStart: startSpan.start + format.StartSubstrOffset,
			origEnd:   spans[format.StartFieldIdx+fieldCount-1].end,
		}, nil
	}
	matched := match[3]

	originalEnd := startSpan.start + format.StartSubstrOffset
	for _, segment := range segments {
		if matched <= segment.normalizedEnd {
			originalEnd = segment.originalStart + matched - segment.normalizedStart
			break
		}
		originalEnd = segment.originalEnd
	}
	return timestampExtractionDetails{
		text:      normalizedText[:matched],
		origStart: startSpan.start + format.StartSubstrOffset,
		origEnd:   originalEnd,
	}, nil
}

// timestampLayoutRegexp makes a regexp that finds the timestamp accordingly to
// the given layout at the start of a line. It is allowed to be loose;
// time.Parse checks the exact value afterward.
//
// The regexp is needed because time.Parse doesn't tolerate extra input after
// the timestamp, so the regexp will separate timestamp from the rest of the
// line.
func timestampLayoutRegexp(layout string, hasTrailingChars bool) string {
	type timestampLayoutRegexpToken struct {
		layout  string
		pattern string
	}

	var timestampLayoutRegexpTokens = []timestampLayoutRegexpToken{
		{layout: "January", pattern: `(?:January|February|March|April|May|June|July|August|September|October|November|December)`},
		{layout: "Monday", pattern: `(?:Monday|Tuesday|Wednesday|Thursday|Friday|Saturday|Sunday)`},
		{layout: "Z07:00", pattern: `(?:Z|[+-]\d{2}:\d{2})`},
		{layout: "Z0700", pattern: `(?:Z|[+-]\d{4})`},
		{layout: "Z07", pattern: `(?:Z|[+-]\d{2})`},
		{layout: "-07:00", pattern: `[+-]\d{2}:\d{2}`},
		{layout: "-0700", pattern: `[+-]\d{4}`},
		{layout: "-07", pattern: `[+-]\d{2}`},
		{layout: "2006", pattern: `\d{4}`},
		{layout: "Jan", pattern: `(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)`},
		{layout: "Mon", pattern: `(?:Sun|Mon|Tue|Wed|Thu|Fri|Sat)`},
		{layout: "MST", pattern: `[A-Za-z]{3}`},
		{layout: "_2", pattern: ` ?\d{1,2}`},
		{layout: "02", pattern: `\d{1,2}`},
		{layout: "01", pattern: `\d{1,2}`},
		// Even though Go does accept single-digit hour values, accounting for it
		// would complicate awk expression generation sigfificantly because e.g.
		// this layout "2006-01-02T15:04:05.000Z07:00" would have two variable-length
		// components: hour and the timezone. So since single-hour is very uncommon
		// in logs, we just reject it.
		{layout: "15", pattern: `\d{2}`},
		{layout: "04", pattern: `\d{1,2}`},
		{layout: "05", pattern: `\d{1,2}`},
		{layout: "06", pattern: `\d{2}`},
		{layout: "PM", pattern: `(?:AM|PM)`},
		{layout: "pm", pattern: `(?:am|pm)`},
		{layout: "2", pattern: `\d{1,2}`},
		{layout: "1", pattern: `\d{1,2}`},
		{layout: "3", pattern: `\d{1,2}`},
		{layout: "4", pattern: `\d{1,2}`},
		{layout: "5", pattern: `\d{1,2}`},
	}

	var b strings.Builder
	for i := 0; i < len(layout); {
		if layout[i] == '.' {
			j := i + 1
			for j < len(layout) && (layout[j] == '0' || layout[j] == '9') {
				j++
			}
			if j > i+1 {
				if strings.Trim(layout[i+1:j], "0") == "" {
					fmt.Fprintf(&b, `[.,]%s`, strings.Repeat(`\d`, j-i-1))
				} else {
					// Go's 9-based fractional layouts accept an omitted
					// fractional part as well.
					b.WriteString(`(?:[.,]\d{1,9})?`)
				}
				i = j
				continue
			}
		}

		matched := false
		for _, token := range timestampLayoutRegexpTokens {
			if strings.HasPrefix(layout[i:], token.layout) {
				b.WriteString(token.pattern)
				i += len(token.layout)
				matched = true
				break
			}
		}
		if !matched {
			b.WriteString(regexp.QuoteMeta(layout[i : i+1]))
			i++
		}
	}
	pattern := b.String()
	if !hasTrailingChars {
		return "(" + pattern + `)(?:\s|$)`
	}
	return "(" + pattern + ")"
}
