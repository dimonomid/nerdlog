package core

import (
	"fmt"
	"sort"
	"strings"

	"github.com/juju/errors"
)

type TimeFormatDescr struct {
	// TimestampFormat contains the timestamp layout and its reusable compiled
	// regexp parser.
	TimestampFormat CompiledTimestampFormat

	// MinuteKeyLayout is a Go-style time layout which should parse the time
	// captured by the awk expression `TimeFormatAWKExpr.MinuteKey` (read there
	// what "minute key" means in the first place).
	//
	// So e.g. for the traditional syslog format "Jan _2 15:04:05", it should be
	// "Jan _2 15:04".
	//
	// For the format "2006-01-02T15:04:05.000000Z07:00", it should rather be
	// "2006-01-02T15:04" (if we decided to include the year) or "01-02T15:04"
	// (if we decided to not include the year).
	MinuteKeyLayout string

	// AWKExpr contains all the awk expressions which will be used by the
	// nerdlog_agent.sh script to get the time components from logs.
	AWKExpr TimeFormatAWKExpr
}

// TimeFormatAWKExpr contains all the awk expressions which will be used by
// the nerdlog_agent.sh script to get the time components from logs.
type TimeFormatAWKExpr struct {
	// PrepStatementsMonthYearDayHHMM, PrepStatementsMinuteKey and
	// PrepStatementsHHMM are AWK statements executed before the expressions
	// below. They can create variables to hold shared calculations used by
	// multiple component expressions.

	// PrepStatementsMonthYearDayHHMM is executed right before evaluating the
	// following four expressions below: Month, Year, Day, HHMM, and Timezone.
	PrepStatementsMonthYearDayHHMM string
	// PrepStatementsMinuteKey is executed right before evaluating MinuteKey and
	// Timezone.
	PrepStatementsMinuteKey string
	// PrepStatementsHHMM is executed right before evaluating just the HHMM and
	// Timezone.
	PrepStatementsHHMM string

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

	// MinuteKey is an AWK expression to get a string covering all timestamp
	// components from minute and larger. It'll be used as a key to identify a
	// particular minute (in the mapping from the minute to the amount of logs in
	// that minute), hence the name; so it should not include seconds, and it
	// should include minute+hour+day+month, maybe even year but that's optional,
	// since Nerdlog is not designed to look at logs spanning more than one year.
	//
	// So e.g. for the traditional syslog format "Jan _2 15:04:05", it should be
	// "substr($0, 1, 12)".
	//
	// For the format "2006-01-02T15:04:05.000000Z07:00", it should rather be
	// "substr($0, 1, 16)" (to include the year) or "substr($0, 6, 11)" (to not
	// include the year).
	MinuteKey string

	// Timezone is an AWK expression returning the raw timezone from the
	// timestamp, without any separator which precedes it in the source layout.
	// It is empty for timestamp formats without an explicit timezone.
	Timezone string
}

// InferTimeFormatDescr detects and generates a consistent time descriptor for
// the supplied sample lines, rejecting files whose samples use different
// timestamp formats.
func InferTimeFormatDescr(logFilename string, logLines []string) (*TimeFormatDescr, error) {
	if len(logLines) == 0 {
		return nil, errors.Errorf("no logs in %q, can't detect time format", logFilename)
	}

	descrs := make([]*TimeFormatDescr, 0, len(logLines))

	for i, line := range logLines {
		timestampFormat, err := DetectTimestampFormat(line)
		if err != nil {
			return nil, errors.Annotatef(err, "in %q", logFilename)
		}

		timeDescr, err := GenerateTimeDescr(*timestampFormat)
		if err != nil {
			return nil, errors.Trace(err)
		}

		if i > 0 {
			if descrs[0].TimestampFormat.TimestampFormat != timeDescr.TimestampFormat.TimestampFormat {
				return nil, errors.Errorf(
					"log file %q has lines with different formats: %s (trailing chars: %t) and %s (trailing chars: %t)",
					logFilename,
					descrs[0].TimestampFormat.Layout,
					descrs[0].TimestampFormat.HasTrailingChars,
					timeDescr.TimestampFormat.Layout,
					timeDescr.TimestampFormat.HasTrailingChars,
				)
			}
		}

		descrs = append(descrs, timeDescr)
	}

	return descrs[0], nil
}

// Layouts sharing a prefix must be ordered longest first. Detection returns
// the first match, so a shorter layout would otherwise hide an extension
// such as a numeric timezone.
var knownTimestampLayouts = []string{
	"Jan _2 15:04:05", // Traditional rsyslog format without year
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04:05-0700",
	"2006-01-02T15:04:05 PM MST",
	"2006-01-02T15:04:05MST",
	"2006-01-02T15:04:05",
	"2006-01-02T15:04",
	"2006-01-02 15:04:05 -0700",
	"2006-01-02 15:04:05-0700",
	"2006-01-02 15:04:05 MST",
	"2006-01-02 15:04:05MST",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"02/Jan/2006:15:04:05 -0700",
	"2006/01/02 15:04:05 -0700",
	"2006/01/02 15:04:05-0700",
	"2006/01/02 15:04:05 MST",
	"2006/01/02 15:04:05MST",
	"2006/01/02 15:04:05",
	"2006/01/02 15:04",
	"Mon Jan 2 15:04:05 2006",
	"Mon Jan _2 03:04:05 PM MST 2006",
	"Mon Jan _2 15:04:05 MST 2006",
	"Mon Jan _2 15:04:05 2006",
	"Mon Jan _2 15:04:05",
	"Mon Jan _2 15:04",
	"02-Jan-2006 15:04:05 -0700",
	"02-Jan-2006 15:04:05 MST",
	"02-Jan-2006 15:04:05",
	"Jan 02 15:04:05",
	"2006-002T15:04:05 PM MST",
	"2006-002T15:04:05-0700",
	"06-01-02T15:04:05MST",
	"06-01-02T15:04:05-0700",
	"2006-002T15:04:05MST",
	"2006-002T15:04:05",
	"2006 Jan 02 Mon 15:04:05",
	"2006 Jan 02 15:04:05",
	"Jan-02 15:04:05",
	"Jan 02 3:04:05",
	"Jan 02 3:04",
	"Jan _2, 2006 3:04:05 PM",
	"Jan 02, 2006 3:04:05 PM",
	"Jan _2, 2006 at 3:04:05 PM",
	"Jan 02, 2006 at 03:04:05 PM",
	"02.01.2006 15:04:05",
	"02/01/06 15:04:05",
	"02 Jan 2006 15:04:05-0700",
	"02 Jan 2006 15:04:05",
	"02 Jan 2006 15:04",
	"01/02/2006 03:04:05 PM MST",
	"01/02/2006 3:04:05 PM MST",
	"01/02/2006 03:04:05 PM-0700",
	"01/02/2006 3:04:05 PM-0700",
	"01/_2/2006 3:04:05 PM-0700",
	"01/_2/2006 3:04:05 PM -0700",
	"01/_2/2006 03:04:05 PM",
	"01/_2/2006 3:04:05 PM",
	"01/_2/2006 3:04:05PM",
	"01/02/06 15:04:05",
	"01/02/2006 15:04:05",
	"02/Jan/2006 15:04:05",
	"02/Jan/06 15:04:05",
	"01.02 15:04:05",
	"0102 15:04:05",
	"20060102 15:04:05",
	"060102 150405",
	"060102 15:04:05",
	"20060102 150405",
	"20060102.150405",
	"15:04:05",
	"04:05",
	"01/02 15:04:05",
	"2006-01-02",
	"2006-01",
	"2006/01/02",
	"2006/01",
	"Jan 02",
}

// DetectTimestampFormat tries to detect a supported timestamp layout from a
// log line.
func DetectTimestampFormat(logLine string) (*TimestampFormat, error) {
	for startFieldIdx := 0; startFieldIdx <= 5; startFieldIdx++ {
		for _, knownLayout := range knownTimestampLayouts {
			for _, layout := range timestampLayoutVariants(knownLayout) {
				for _, layoutVariant := range timestampLayoutBracketVariants(layout) {
					timestampFormat := TimestampFormat{
						Layout:           layoutVariant,
						StartFieldIdx:    startFieldIdx,
						HasTrailingChars: true,
					}
					compiled, err := timestampFormat.Compile()
					if err != nil {
						continue
					}
					if _, err := compiled.Parse(logLine); err != nil {
						continue
					}
					// Prefer the stricter boundary when the timestamp ends at a
					// field boundary. Otherwise retain the attached-character form.
					timestampFormat.HasTrailingChars = false
					compiled, err = timestampFormat.Compile()
					if err == nil {
						if _, err := compiled.Parse(logLine); err == nil {
							return &timestampFormat, nil
						}
					}
					timestampFormat.HasTrailingChars = true
					return &timestampFormat, nil
				}
			}
		}
	}
	return nil, errors.New("unable to detect timestamp format")
}

// timestampLayoutVariants returns a layout with each supported fractional
// second width, from nine digits down to no fraction.
func timestampLayoutVariants(layout string) []string {
	if !strings.Contains(layout, "05") {
		return []string{layout}
	}

	variants := make([]string, 0, 10)
	for digits := 9; digits >= 0; digits-- {
		replacement := "05"
		if digits > 0 {
			replacement += "." + strings.Repeat("0", digits)
		}
		variants = append(variants, strings.Replace(layout, "05", replacement, 1))
	}
	return variants
}

// timestampLayoutBracketVariants returns both the plain layout and the same
// layout enclosed in square brackets.
func timestampLayoutBracketVariants(layout string) []string {
	return []string{layout, "[" + layout + "]"}
}

// GenerateTimeDescr takes a timestamp format, and returns the full time format
// descriptor to be used for parsing all logs.
func GenerateTimeDescr(timestampFormat TimestampFormat) (*TimeFormatDescr, error) {
	// Compile once here because the returned descriptor is reused for every log
	// line in this logstream.
	compiledTimestampFormat, err := timestampFormat.Compile()
	if err != nil {
		return nil, err
	}
	analysis, err := analyzeTimestampLayout(timestampFormat)
	if err != nil {
		return nil, err
	}

	generator := awkLayoutGenerator{layout: analysis}
	timezone := generator.generateTimezone()
	awk := generator.generateExpressions(timezone)

	return &TimeFormatDescr{
		TimestampFormat: *compiledTimestampFormat,
		MinuteKeyLayout: analysis.minuteKeyLayout(timezone),
		AWKExpr:         awk,
	}, nil
}

// timestampLayoutAnalysis is the validated, positional representation shared
// by all AWK and minute-key generators. Layout positions are byte offsets in
// the normalized timestamp layout.
type timestampLayoutAnalysis struct {
	// format retains the original field and substring offsets. Those offsets
	// select where the timestamp begins in a complete log line; all ranges below
	// are relative to format.Layout itself.
	format TimestampFormat
	// fields is strings.Fields(format.Layout) enriched with each field's byte
	// position and variable-width components. It is the bridge between layout
	// byte ranges and AWK's one-based $N fields.
	fields []timestampLayoutField

	// Component locations in format.Layout. Year, seconds, and timezone are
	// optional; month, day, and hhmm are validated as required.
	year     *indexAndLength
	month    *indexAndLength
	day      *indexAndLength
	hhmm     *indexAndLength
	second   *indexAndLength
	timezone *indexAndLength
	// minuteKey is the smallest contiguous layout range containing month, day,
	// and hhmm. It deliberately excludes seconds and the timezone.
	minuteKey indexAndLength
}

// timestampLayoutField describes one whitespace-delimited field in a layout.
// For example, layout "Jan _2 15:04:05" produces three fields. AWK sees the
// corresponding input fields as $1, $2, and $3, adjusted by StartFieldIdx.
type timestampLayoutField struct {
	// text is the field exactly as it appears in TimestampFormat.Layout.
	text string
	// start is the field's byte offset in TimestampFormat.Layout, not in a log
	// line. StartFieldIdx and StartSubstrOffset provide the latter mapping.
	start int
	// variables contains layout components whose input width can differ from
	// their layout spelling, such as "2", "January", or "Z07:00". Analysis
	// currently rejects fields containing more than one such component.
	variables []variableLayoutComponent
}

// analyzeTimestampLayout validates the subset of Go time layouts supported by
// the agent and records all positions needed by subsequent generators.
func analyzeTimestampLayout(format TimestampFormat) (*timestampLayoutAnalysis, error) {
	layout := format.Layout
	// Go treats every 06 not belonging to the four-digit 2006 token as a
	// two-digit year, including in compact layouts such as 060102. Nerdlog does
	// not currently expand that value to the four-digit year required by its
	// index, so reject it instead of silently inferring the year from the month.
	if strings.Contains(strings.ReplaceAll(layout, "2006", ""), "06") {
		return nil, errors.New("unsupported layout: two-digit years are not supported")
	}
	analysis := &timestampLayoutAnalysis{
		format:   format,
		year:     indexAndLengthOfTimeComponent(layout, "2006"),
		month:    indexAndLengthOfTimeComponent(layout, "January", "Jan", "01", "1"),
		day:      indexAndLengthOfTimeComponent(layout, "02", "_2", "2"),
		hhmm:     indexAndLengthOfTimeComponent(layout, "15:04"),
		second:   indexAndLengthOfTimeComponent(layout, "05", "5"),
		timezone: indexAndLengthOfTimeComponent(layout, "Z07:00", "Z0700", "Z07", "-07:00", "-0700", "-07"),
	}

	if indexAndLengthOfTimeComponent(layout, "Monday") != nil {
		return nil, errors.New("unsupported layout: full weekday names are not supported")
	}

	if namedTimezone := indexAndLengthOfTimeComponent(layout, "MST"); namedTimezone != nil {
		return nil, errors.Errorf(
			"unsupported layout: named timezone %q cannot be parsed reliably by awk",
			layout[namedTimezone.index:namedTimezone.index+namedTimezone.length],
		)
	}
	if indexAndLengthOfTimeComponent(layout, "PM", "pm") != nil {
		return nil, errors.New("unsupported layout: 12-hour clocks with AM/PM are not supported")
	}
	// Weekdays may be part of the parsed timestamp, but the agent does not need
	// to extract them for indexing.
	missing := make([]string, 0, 3)
	if analysis.month == nil {
		missing = append(missing, "month")
	}
	if analysis.day == nil {
		missing = append(missing, "day")
	}
	if analysis.hhmm == nil {
		missing = append(missing, "hour/minute")
	}
	if len(missing) > 0 {
		return nil, errors.Errorf(
			"unsupported time layout %q: missing required components: %s",
			layout,
			strings.Join(missing, ", "),
		)
	}

	searchFrom := 0
	for _, fieldText := range strings.Fields(layout) {
		fieldOffset := strings.Index(layout[searchFrom:], fieldText)
		field := timestampLayoutField{
			text:      fieldText,
			start:     searchFrom + fieldOffset,
			variables: findVariableLayoutComponents(fieldText),
		}
		searchFrom = field.start + len(field.text)
		analysis.fields = append(analysis.fields, field)
	}

	for i, field := range analysis.fields {
		if len(field.variables) > 1 {
			// Splitting one AWK field around multiple variable components would
			// make the normal per-line parsing path considerably more expensive.
			return nil, errors.Errorf("unsupported layout: multiple variable-length components in field %q", field.text)
		}
		if !format.HasTrailingChars || i != len(analysis.fields)-1 || len(field.variables) != 1 {
			continue
		}

		variable := field.variables[0]
		if variable.index+variable.length != len(field.text) {
			continue
		}
		// Numeric timezone layouts are bounded specially by generateTimezone;
		// other variable components at the end are ambiguous when metadata can
		// be attached directly to them.
		isKnownTimezone := analysis.timezone != nil && variable.index+field.start == analysis.timezone.index
		if !isKnownTimezone {
			return nil, errors.Errorf(
				"unsupported layout: variable-length component at end of final field %q",
				field.text,
			)
		}
	}

	minuteKeyStart := minIndex(analysis.month, analysis.day, analysis.hhmm).index
	minuteKeyEndComponent := maxIndex(analysis.month, analysis.day, analysis.hhmm)
	minuteKeyEnd := minuteKeyEndComponent.index + minuteKeyEndComponent.length
	if analysis.second != nil && analysis.second.index >= minuteKeyStart && analysis.second.index < minuteKeyEnd {
		return nil, errors.Errorf("seconds are in between of month, day, hour and min; can't extract MinuteKey")
	}
	analysis.minuteKey = indexAndLength{index: minuteKeyStart, length: minuteKeyEnd - minuteKeyStart}

	return analysis, nil
}

// numericComponentPadding selects whether variable-width numeric month/day
// components keep the layout's representation or are zero-padded for indexing.
type numericComponentPadding int

const (
	// preserveLayoutPadding leaves numeric 1 and 2 fields unchanged and keeps
	// the space-padding expected by Go's _2 layout token.
	preserveLayoutPadding numericComponentPadding = iota
	// zeroPadNumericComponent produces the two-digit month/day form required by
	// index timestamps and component comparisons.
	zeroPadNumericComponent
)

// generatedTimezone contains the two coordinated timezone representations:
// the AWK expression which extracts it from a log line and the Go-layout text
// appended to MinuteKeyLayout. Both omit any separator from the source layout,
// producing the same normalized minute key whether the source timezone is
// attached or whitespace-separated.
type generatedTimezone struct {
	// awkExpr is concatenated directly to the AWK minute-key expression.
	awkExpr string
	// layoutSuffix is the matching suffix for TimeFormatDescr.MinuteKeyLayout.
	layoutSuffix string
}

// awkLayoutGenerator converts ranges in an analyzed timestamp layout into AWK
// expressions over the corresponding whitespace-separated log fields.
type awkLayoutGenerator struct {
	// layout has already been validated, so generation methods can assume all
	// required components exist and every field has at most one variable part.
	layout *timestampLayoutAnalysis
}

// fieldExpr extracts [start, start+length) from one layout field. start and
// length are relative to timestampLayoutField.text, not the complete log line.
func (g awkLayoutGenerator) fieldExpr(fieldIdx, start, length int) string {
	field := g.layout.fields[fieldIdx]
	// AWK fields are one-based, while StartFieldIdx and fieldIdx are zero-based.
	awkField := g.layout.format.StartFieldIdx + fieldIdx + 1

	if len(field.variables) == 1 {
		variable := field.variables[0]
		if start == variable.index && length == variable.length {
			// A fixed substr length cannot extract a variable-width component.
			// Instead, subtract the stable prefix and suffix from the input field's
			// actual length.
			prefixLength := variable.index
			if fieldIdx == 0 {
				prefixLength += g.layout.format.StartSubstrOffset
			}
			suffixLength := len(field.text) - variable.index - variable.length
			awkStart := prefixLength + 1
			switch {
			case prefixLength == 0 && suffixLength == 0 &&
				(fieldIdx != len(g.layout.fields)-1 || !g.layout.format.HasTrailingChars):
				// The variable component is the complete field, so use the field
				// directly instead of creating an identical substring.
				return fmt.Sprintf("$%d", awkField)
			case suffixLength == 0:
				return fmt.Sprintf("substr($%d, %d)", awkField, awkStart)
			case prefixLength == 0:
				return fmt.Sprintf("substr($%d, %d, length($%d) - %d)", awkField, awkStart, awkField, suffixLength)
			default:
				return fmt.Sprintf("substr($%d, %d, length($%d) - %d - %d)", awkField, awkStart, awkField, prefixLength, suffixLength)
			}
		}
	}

	wholeFieldCanBeUsed := fieldIdx != len(g.layout.fields)-1 || !g.layout.format.HasTrailingChars
	if start == 0 && length == len(field.text) &&
		(fieldIdx != 0 || g.layout.format.StartSubstrOffset == 0) && wholeFieldCanBeUsed {
		return fmt.Sprintf("$%d", awkField)
	}
	awkStart := start + 1
	if fieldIdx == 0 {
		awkStart += g.layout.format.StartSubstrOffset
	}
	return fmt.Sprintf("substr($%d, %d, %d)", awkField, awkStart, length)
}

// rangeExpr returns the normalized text covered by [start, end). Fields are
// joined with one space, matching the Go timestamp parser's normalization.
func (g awkLayoutGenerator) rangeExpr(start, end int, padding numericComponentPadding) string {
	var parts []string
	for i, field := range g.layout.fields {
		fieldEnd := field.start + len(field.text)
		if end <= field.start || start >= fieldEnd {
			continue
		}

		partStart := maxInt(start, field.start) - field.start
		partEnd := minInt(end, fieldEnd) - field.start
		part := g.fieldExpr(i, partStart, partEnd-partStart)

		if len(field.variables) == 1 && (partStart > 0 || partEnd < len(field.text)) {
			variable := field.variables[0]
			variableEnd := variable.index + variable.length
			if partStart <= variable.index && variableEnd <= partEnd &&
				!(partStart == variable.index && partEnd == variableEnd) {
				// The requested range includes a variable component plus some fixed
				// text. Adjust its nominal layout width by the difference between the
				// layout field and the actual input field.
				awkField := g.layout.format.StartFieldIdx + i + 1
				fieldOffset := 0
				if i == 0 {
					fieldOffset = g.layout.format.StartSubstrOffset
				}
				awkStart := partStart + fieldOffset + 1
				if partEnd == len(field.text) {
					part = fmt.Sprintf("substr($%d, %d)", awkField, awkStart)
				} else {
					actualFieldLength := fmt.Sprintf("length($%d) - %d - %d", awkField, fieldOffset, len(field.text))
					part = fmt.Sprintf("substr($%d, %d, %d + (%s))", awkField, awkStart, partEnd-partStart, actualFieldLength)
				}
			}
		}

		isUnpaddedMonthOrDayField := padding == zeroPadNumericComponent &&
			(field.text == "1" || field.text == "2")
		if (field.text == "_2" || isUnpaddedMonthOrDayField) && partStart == 0 && partEnd == len(field.text) {
			pad := " "
			if padding == zeroPadNumericComponent {
				pad = "0"
			}
			part = fmt.Sprintf(`((length(%s) == 1) ? %q %s : %s)`, part, pad, part, part)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ` " " `)
}

// generateExpressions builds all agent-side timestamp expressions from the
// same analyzed positions. The supplied timezone was generated separately
// because its AWK expression and Go minute layout must be kept in sync.
func (g awkLayoutGenerator) generateExpressions(timezone generatedTimezone) TimeFormatAWKExpr {
	layout := g.layout
	awk := TimeFormatAWKExpr{
		Month:     g.rangeExpr(layout.month.index, layout.month.index+layout.month.length, zeroPadNumericComponent),
		Day:       g.rangeExpr(layout.day.index, layout.day.index+layout.day.length, zeroPadNumericComponent),
		HHMM:      g.rangeExpr(layout.hhmm.index, layout.hhmm.index+layout.hhmm.length, zeroPadNumericComponent),
		MinuteKey: g.rangeExpr(layout.minuteKey.index, layout.minuteKey.index+layout.minuteKey.length, preserveLayoutPadding),
		Timezone:  timezone.awkExpr,
	}
	if layout.year == nil {
		awk.Year = "yearByMonth[month]"
	} else {
		awk.Year = g.rangeExpr(layout.year.index, layout.year.index+layout.year.length, zeroPadNumericComponent)
	}
	if layout.month.length == 3 {
		awk.Month = fmt.Sprintf("monthByName[%s]", awk.Month)
	} else if layout.month.length == 7 {
		awk.Month = fmt.Sprintf("monthByName[substr(%s, 1, 3)]", awk.Month)
	}
	return awk
}

// generateTimezone creates both sides of timezone handling: the AWK extraction
// expression and the corresponding suffix parsed by Go.
func (g awkLayoutGenerator) generateTimezone() generatedTimezone {
	layout := g.layout
	if layout.timezone == nil {
		return generatedTimezone{}
	}

	timezoneEnd := layout.timezone.index + layout.timezone.length
	timezoneExpr := g.rangeExpr(layout.timezone.index, timezoneEnd, zeroPadNumericComponent)
	for i, field := range layout.fields {
		fieldEnd := field.start + len(field.text)
		if layout.timezone.index < field.start || timezoneEnd > fieldEnd {
			continue
		}

		fieldOffset := 0
		if i == 0 {
			fieldOffset = layout.format.StartSubstrOffset
		}
		awkField := layout.format.StartFieldIdx + i + 1
		awkStart := layout.timezone.index - field.start + fieldOffset + 1
		timezoneLayout := layout.format.Layout[layout.timezone.index:timezoneEnd]
		if !layout.format.HasTrailingChars && timezoneEnd == fieldEnd {
			// Consuming the field remainder supports both the one-byte Z form and
			// the longer numeric form without branching in AWK.
			if awkStart == 1 {
				timezoneExpr = fmt.Sprintf("$%d", awkField)
			} else {
				timezoneExpr = fmt.Sprintf("substr($%d, %d)", awkField, awkStart)
			}
		} else if layout.format.HasTrailingChars {
			// With attached metadata, consuming the remainder would include that
			// metadata. Bound numeric offsets explicitly and special-case Z, even
			// when fixed layout text such as a closing bracket follows the timezone.
			switch timezoneLayout {
			case "Z07:00":
				timezoneExpr = fmt.Sprintf(`((substr($%d, %d, 1) == "Z") ? "Z" : substr($%d, %d, 6))`, awkField, awkStart, awkField, awkStart)
			case "Z0700":
				timezoneExpr = fmt.Sprintf(`((substr($%d, %d, 1) == "Z") ? "Z" : substr($%d, %d, 5))`, awkField, awkStart, awkField, awkStart)
			case "Z07":
				timezoneExpr = fmt.Sprintf(`((substr($%d, %d, 1) == "Z") ? "Z" : substr($%d, %d, 3))`, awkField, awkStart, awkField, awkStart)
			case "-0700":
				timezoneExpr = fmt.Sprintf("substr($%d, %d, 5)", awkField, awkStart)
			case "-07:00":
				timezoneExpr = fmt.Sprintf("substr($%d, %d, 6)", awkField, awkStart)
			case "-07":
				timezoneExpr = fmt.Sprintf("substr($%d, %d, 3)", awkField, awkStart)
			}
		}
		break
	}

	return generatedTimezone{
		awkExpr:      timezoneExpr,
		layoutSuffix: layout.format.Layout[layout.timezone.index:timezoneEnd],
	}
}

// minuteKeyLayout returns the Go layout used to parse the minute-key strings
// emitted by AWK.
func (a *timestampLayoutAnalysis) minuteKeyLayout(timezone generatedTimezone) string {
	return a.format.Layout[a.minuteKey.index:a.minuteKey.index+a.minuteKey.length] + timezone.layoutSuffix
}

// indexAndLengthOfTimeComponent finds a standalone layout component and
// returns its byte range within s. Components embedded in another numeric or
// alphabetic token are ignored.
func indexAndLengthOfTimeComponent(s string, components ...string) *indexAndLength {
	for _, comp := range components {
		// Try the longer or more specific spellings supplied by the caller.
		for start := 0; start < len(s); {
			pos := strings.Index(s[start:], comp)
			if pos < 0 {
				break
			}
			pos += start
			end := pos + len(comp)
			// A layout token is useful only when it is not part of a larger
			// numeric or alphabetic token.
			if isVariableComponentMatch(s, pos, end, componentIsNumeric(comp)) {
				return &indexAndLength{index: pos, length: len(comp)}
			}
			start = pos + 1
		}
	}
	return nil
}

// componentIsNumeric reports whether a layout component starts with a numeric
// or underscore character, which determines the boundary rules used when
// locating it.
func componentIsNumeric(component string) bool {
	return len(component) > 0 && isLayoutDigitOrUnderscore(component[0])
}

type indexAndLength struct {
	// index is a byte offset in the string from which this range was derived.
	index int
	// length is measured in bytes, matching Go slices and gawk's byte mode.
	length int
}

// variableLayoutComponent identifies a layout token whose rendered input does
// not have one fixed byte width.
type variableLayoutComponent struct {
	// index is relative to its containing timestampLayoutField.text.
	index int
	// length is the token's width in the layout, not its rendered input width.
	length int
}

// findVariableLayoutComponents returns the variable-width time components in a
// single layout field, in source order and with overlapping matches removed.
func findVariableLayoutComponents(field string) []variableLayoutComponent {
	specs := []string{
		"January",
		"Monday",
		"Z07:00",
		"Z0700",
		"Z07",
		"MST",
		"1",
		"2",
		"3",
		"4",
		"5",
	}
	var result []variableLayoutComponent
	for _, spec := range specs {
		// Find every occurrence first; overlapping candidates are resolved after
		// all token types have been inspected.
		for start := 0; start < len(field); {
			pos := strings.Index(field[start:], spec)
			if pos < 0 {
				break
			}
			pos += start
			end := pos + len(spec)
			if isVariableComponentMatch(field, pos, end, componentIsNumeric(spec)) {
				result = append(result, variableLayoutComponent{
					index:  pos,
					length: len(spec),
				})
			}
			start = end
		}
	}
	sort.Slice(result, func(i, j int) bool {
		// Prefer source order, and for equal starts prefer the longer token
		// (for example Z07:00 over Z07).
		if result[i].index != result[j].index {
			return result[i].index < result[j].index
		}
		return result[i].length > result[j].length
	})
	filtered := result[:0]
	for _, component := range result {
		// Drop candidates hidden inside a component already kept.
		if len(filtered) == 0 || component.index >= filtered[len(filtered)-1].index+filtered[len(filtered)-1].length {
			filtered = append(filtered, component)
		}
	}
	return filtered
}

// isVariableComponentMatch reports whether the component [start:end] is
// delimited sufficiently to be treated as an independent variable-width
// layout component.
func isVariableComponentMatch(field string, start, end int, numeric bool) bool {
	if !numeric {
		// Text components must not touch another layout letter.
		return (start == 0 || !isLayoutLetter(field[start-1])) && (end == len(field) || !isLayoutLetter(field[end]))
	}
	// Numeric components must not touch another digit or underscore, since
	// those characters may be part of a neighboring numeric layout token.
	return (start == 0 || !isLayoutDigitOrUnderscore(field[start-1])) && (end == len(field) || !isLayoutDigitOrUnderscore(field[end]))
}

func isLayoutLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isLayoutDigitOrUnderscore(b byte) bool {
	return (b >= '0' && b <= '9') || b == '_'
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minIndex(vals ...*indexAndLength) *indexAndLength {
	// Return the leftmost component; callers use it as the start of a range.
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

// maxIndex returns the component with the greatest source index.
func maxIndex(vals ...*indexAndLength) *indexAndLength {
	// Return the rightmost component; callers add its length for the range end.
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
