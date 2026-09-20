package main

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dimonomid/nerdlog/cmd/nerdlog/ui"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/uniseg"
)

var (
	tableSearchColorPattern  = regexp.MustCompile(`\[([a-zA-Z]+|#[0-9a-zA-Z]{6}|\-)?(:([a-zA-Z]+|#[0-9a-zA-Z]{6}|\-)?(:([lbidrus]+|\-)?)?)?\]`)
	tableSearchRegionPattern = regexp.MustCompile(`\["([a-zA-Z0-9_,;: \-\.]*)"\]`)
	tableSearchEscapePattern = regexp.MustCompile(`\[([a-zA-Z0-9_,;: \-\."#]+)\[(\[*)\]`)
)

var (
	tableSearchMatchStyle = tcell.StyleDefault.
				Foreground(tcell.ColorBlack).
				Background(tcell.ColorYellow)
	tableSearchActiveStyle = tcell.StyleDefault.
				Foreground(tcell.ColorBlack).
				Background(tcell.ColorOrange).
				Bold(true)
)

type tableSearchMatch struct {
	row, column int
	start, end  int // Screen-column offsets in the cell's displayed text.
}

type tableSearchState struct {
	query                string
	matches              []tableSearchMatch
	active               int
	highlightsSuppressed bool
	spans                []ui.CellStyleSpan
}

func (s *tableSearchState) rebuild(table *ui.Table, query string, originRow int) {
	previousMatch, hadPreviousMatch := s.activeMatch()
	s.query = query
	s.matches = s.matches[:0]
	s.active = -1
	if query == "" {
		table.SetCellStyleSpans(nil)
		return
	}

	queryRunes := []rune(query)
	caseSensitive := false
	for _, r := range queryRunes {
		if unicode.IsUpper(r) {
			caseSensitive = true
			break
		}
	}
	if !caseSensitive {
		queryRunes = foldRunes(queryRunes)
	}
	queryIsASCII := isASCII(query)

	for row := rowIdxLoadOlder + 1; row < table.GetRowCount(); row++ {
		for column := 0; column < table.GetColumnCount(); column++ {
			text := tableSearchDisplayedText(table.GetCell(row, column).Text)
			if queryIsASCII && isASCII(text) {
				for start := 0; start+len(query) <= len(text); {
					if asciiEqual(text[start:start+len(query)], query, caseSensitive) {
						s.matches = append(s.matches, tableSearchMatch{
							row: row, column: column,
							start: start, end: start + len(query),
						})
						start += len(query)
					} else {
						start++
					}
				}
				continue
			}

			textRunes := []rune(text)
			comparisonRunes := textRunes
			if !caseSensitive {
				comparisonRunes = foldRunes(textRunes)
			}
			var widths []int

			for start := 0; start+len(queryRunes) <= len(comparisonRunes); {
				if runesEqual(comparisonRunes[start:start+len(queryRunes)], queryRunes) {
					if widths == nil {
						widths = graphemeWidthPrefixes(text, len(textRunes))
					}
					s.matches = append(s.matches, tableSearchMatch{
						row:    row,
						column: column,
						start:  widths[start],
						end:    widths[start+len(queryRunes)],
					})
					start += len(queryRunes)
				} else {
					start++
				}
			}
		}
	}

	if hadPreviousMatch {
		for index, match := range s.matches {
			if match == previousMatch {
				s.active = index
				break
			}
		}
	}
	if s.active < 0 {
		s.active = s.firstAtOrAfterRow(originRow)
	}
	s.applySpans(table)
}

func isASCII(text string) bool {
	for index := 0; index < len(text); index++ {
		if text[index] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

func asciiEqual(text, query string, caseSensitive bool) bool {
	if caseSensitive {
		return text == query
	}
	for index := range text {
		a, b := text[index], query[index]
		if 'A' <= a && a <= 'Z' {
			a += 'a' - 'A'
		}
		if 'A' <= b && b <= 'Z' {
			b += 'a' - 'A'
		}
		if a != b {
			return false
		}
	}
	return true
}

func tableSearchDisplayedText(text string) string {
	if !strings.ContainsRune(text, '[') {
		return text
	}
	colorIndices := tableSearchColorPattern.FindAllStringIndex(text, -1)
	regionIndices := tableSearchRegionPattern.FindAllStringIndex(text, -1)
	escapeIndices := tableSearchEscapePattern.FindAllStringIndex(text, -1)

	// The color expression recognizes [] even though tview treats it as part of
	// an escaped tag. Match tview's parser by excluding those empty tags.
	for index := len(colorIndices) - 1; index >= 0; index-- {
		if colorIndices[index][1]-colorIndices[index][0] == 2 {
			colorIndices = append(colorIndices[:index], colorIndices[index+1:]...)
		}
	}

	type tagIndex struct {
		start, end int
		escaped    bool
	}
	tags := make([]tagIndex, 0, len(colorIndices)+len(regionIndices)+len(escapeIndices))
	for _, index := range colorIndices {
		tags = append(tags, tagIndex{start: index[0], end: index[1]})
	}
	for _, index := range regionIndices {
		tags = append(tags, tagIndex{start: index[0], end: index[1]})
	}
	for _, index := range escapeIndices {
		tags = append(tags, tagIndex{start: index[0], end: index[1], escaped: true})
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i].start < tags[j].start })

	plain := make([]byte, 0, len(text))
	from := 0
	for _, tag := range tags {
		if tag.start < from {
			continue
		}
		plain = append(plain, text[from:tag.start]...)
		if tag.escaped {
			plain = append(plain, text[tag.start:tag.end-2]...)
			plain = append(plain, ']')
		}
		from = tag.end
	}
	plain = append(plain, text[from:]...)
	return string(plain)
}

func foldRunes(runes []rune) []rune {
	folded := make([]rune, len(runes))
	for index, r := range runes {
		folded[index] = unicode.ToLower(r)
	}
	return folded
}

func runesEqual(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

func graphemeWidthPrefixes(text string, runeCount int) []int {
	prefixes := make([]int, runeCount+1)
	graphemes := uniseg.NewGraphemes(text)
	runeIndex, width := 0, 0
	for graphemes.Next() {
		clusterRuneCount := utf8.RuneCountInString(graphemes.Str())
		for offset := 0; offset < clusterRuneCount; offset++ {
			prefixes[runeIndex+offset] = width
		}
		runeIndex += clusterRuneCount
		width += graphemes.Width()
		prefixes[runeIndex] = width
	}
	return prefixes
}

func (s *tableSearchState) firstAtOrAfterRow(row int) int {
	if len(s.matches) == 0 {
		return -1
	}
	for index, match := range s.matches {
		if match.row >= row {
			return index
		}
	}
	return 0
}

func (s *tableSearchState) lastAtOrBeforeRow(row int) int {
	if len(s.matches) == 0 {
		return -1
	}
	for index := len(s.matches) - 1; index >= 0; index-- {
		if s.matches[index].row <= row {
			return index
		}
	}
	return len(s.matches) - 1
}

func (s *tableSearchState) move(table *ui.Table, forward bool) (tableSearchMatch, bool) {
	if len(s.matches) == 0 {
		return tableSearchMatch{}, false
	}
	s.highlightsSuppressed = false

	selectedRow, _ := table.GetSelection()
	if s.active >= 0 && s.active < len(s.matches) && s.matches[s.active].row == selectedRow {
		if forward {
			s.active = (s.active + 1) % len(s.matches)
		} else {
			s.active = (s.active - 1 + len(s.matches)) % len(s.matches)
		}
	} else if forward {
		s.active = s.firstAtOrAfterRow(selectedRow)
	} else {
		s.active = s.lastAtOrBeforeRow(selectedRow)
	}

	s.applySpans(table)
	return s.matches[s.active], true
}

func (s *tableSearchState) activeMatch() (tableSearchMatch, bool) {
	if s.active < 0 || s.active >= len(s.matches) {
		return tableSearchMatch{}, false
	}
	return s.matches[s.active], true
}

func (s *tableSearchState) restoreActiveMatch(match tableSearchMatch, valid bool) {
	if !valid {
		return
	}
	for index, candidate := range s.matches {
		if candidate == match {
			s.active = index
			return
		}
	}
}

func (s *tableSearchState) applySpans(table *ui.Table) {
	if len(s.matches) == 0 || s.highlightsSuppressed {
		table.SetCellStyleSpans(nil)
		return
	}
	s.spans = s.spans[:0]
	if cap(s.spans) < len(s.matches) {
		s.spans = make([]ui.CellStyleSpan, 0, len(s.matches))
	}
	for index, match := range s.matches {
		style := tableSearchMatchStyle
		if index == s.active {
			style = tableSearchActiveStyle
		}
		s.spans = append(s.spans, ui.CellStyleSpan{
			Row: match.row, Column: match.column,
			Start: match.start, End: match.end,
			Style: style,
		})
	}
	table.SetCellStyleSpans(s.spans)
}

func (s *tableSearchState) suppressHighlights(table *ui.Table) {
	s.highlightsSuppressed = true
	s.applySpans(table)
}
