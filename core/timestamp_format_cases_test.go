package core

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// timestampFormatCase is one hardcoded detector-visible format variant shared
// by descriptor-generation and through-agent tests.
type timestampFormatCase struct {
	name string
	// canonicalLayout is the exact entry from knownTimestampLayouts. Keeping it
	// explicit makes the completeness check independent of parsing the case name.
	canonicalLayout string
	// format is the literal format used to render the fixture input. For comma
	// fractions it differs from the canonical dot-based detected format below.
	format TimestampFormat
	// wantDetectedFormatOverride is set only when successful inference selects a
	// format different from format. A nil value means format is expected.
	wantDetectedFormatOverride *TimestampFormat
	wantMinuteKeyLayout        string
	wantAWK                    TimeFormatAWKExpr
	wantErr                    string // Direct GenerateTimeDescr rejection.
	wantAgentErr               string // End-to-end inference/bootstrap rejection.
}

func TestTimestampFormatCasesCoverKnownLayouts(t *testing.T) {
	covered := make(map[string][]string)
	commaVariants := make(map[string]int)
	for _, tc := range timestampFormatCases {
		variantName := strings.TrimPrefix(tc.name, tc.canonicalLayout+"/")
		covered[tc.canonicalLayout] = append(covered[tc.canonicalLayout], variantName)
		if strings.Contains(tc.format.Layout, "05,") {
			commaVariants[tc.canonicalLayout]++
			if tc.wantDetectedFormatOverride != nil {
				assert.Contains(t, tc.wantDetectedFormatOverride.Layout, "05.", tc.name)
			}
		}
	}

	want := append([]string(nil), knownTimestampLayouts...)
	got := make([]string, 0, len(covered))
	for layout := range covered {
		got = append(got, layout)
	}
	sort.Strings(want)
	sort.Strings(got)
	assert.Equal(t, want, got)

	secondVariants := []string{
		"plain/no-fraction/field-0/separated",
		"bracketed/no-fraction/field-3/attached",
		"plain/no-fraction/field-3/separated",
		"bracketed/milliseconds/field-0/separated",
		"plain/milliseconds/field-3/attached",
		"bracketed/microseconds/field-3/separated",
		"plain/microseconds/field-0/attached",
		"bracketed/microseconds/field-0/attached",
		"plain/comma-microseconds/field-0/separated",
	}
	nonSecondVariants := []string{
		"plain/field-0/separated",
		"bracketed/field-3/attached",
		"plain/field-3/attached",
		"bracketed/field-0/separated",
	}
	sort.Strings(secondVariants)
	sort.Strings(nonSecondVariants)
	for layout, variants := range covered {
		sort.Strings(variants)
		expected := nonSecondVariants
		if strings.Contains(layout, "05") {
			expected = secondVariants
		}
		assert.Equal(t, expected, variants, layout)
		wantCommaVariants := 0
		if strings.Contains(layout, "05") {
			wantCommaVariants = 1
		}
		assert.Equal(t, wantCommaVariants, commaVariants[layout], layout)
	}
}

// detectedFormat returns the canonical format production inference passes to
// GenerateTimeDescr after parsing the fixture's literal input format.
func (tc timestampFormatCase) detectedFormat() TimestampFormat {
	if tc.wantDetectedFormatOverride != nil {
		return *tc.wantDetectedFormatOverride
	}
	return tc.format
}

func TestGenerateTimeDescr(t *testing.T) {
	for _, tc := range timestampFormatCases {
		t.Run(tc.name, func(t *testing.T) {
			format := tc.detectedFormat()
			got, err := GenerateTimeDescr(format)
			if tc.wantErr != "" {
				assert.EqualError(t, err, tc.wantErr)
				assert.Nil(t, got)
				return
			}

			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, format, got.TimestampFormat.TimestampFormat)
			assert.Equal(t, tc.wantMinuteKeyLayout, got.MinuteKeyLayout)
			assert.Equal(t, tc.wantAWK, got.AWKExpr)
		})
	}
}
