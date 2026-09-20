package main

import (
	"testing"
	"time"

	"github.com/dimonomid/nerdlog/cmd/nerdlog/ui"
	"github.com/dimonomid/nerdlog/core"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

// TestLogMsgDisplayTimeColorsDecreasesByAmount verifies that the table shows
// an out-of-order record's parsed timestamp with severity based on its decrease.
func TestLogMsgDisplayTimeColorsDecreasesByAmount(t *testing.T) {
	effectiveTime := time.Date(2026, 9, 19, 12, 5, 0, 0, time.UTC)
	tests := []struct {
		name     string
		decrease time.Duration
		color    tcell.Color
	}{
		{name: "less than one second", decrease: time.Nanosecond, color: tcell.ColorBlue},
		{name: "exactly one second", decrease: time.Second, color: tcell.ColorBlue},
		{name: "more than one second", decrease: time.Second + time.Nanosecond, color: tcell.ColorYellow},
		{name: "exactly fifteen seconds", decrease: 15 * time.Second, color: tcell.ColorYellow},
		{name: "more than fifteen seconds", decrease: 15*time.Second + time.Nanosecond, color: tcell.ColorRed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			origTime := effectiveTime.Add(-test.decrease)
			displayTime, color := logMsgDisplayTime(core.LogMsg{
				Time:              effectiveTime,
				OrigDecreasedTime: origTime,
			})

			assert.Equal(t, origTime, displayTime)
			assert.Equal(t, test.color, color)
		})
	}
}

// TestLogMsgDisplayTimeUsesEffectiveTime verifies the ordinary timestamp color
// and value remain unchanged.
func TestLogMsgDisplayTimeUsesEffectiveTime(t *testing.T) {
	effectiveTime := time.Date(2026, 9, 19, 12, 5, 0, 0, time.UTC)

	displayTime, color := logMsgDisplayTime(core.LogMsg{Time: effectiveTime})

	assert.Equal(t, effectiveTime, displayTime)
	assert.Equal(t, tcell.ColorLightBlue, color)
}

// TestLogMsgDisplayTimeCellShowsMalformedMarker verifies that a raw record's
// unavailable or inherited timestamp is never presented as a real timestamp.
func TestLogMsgDisplayTimeCellShowsMalformedMarker(t *testing.T) {
	text, color := logMsgDisplayTimeCell(core.LogMsg{
		Time:      time.Date(2026, 9, 19, 12, 5, 0, 0, time.UTC),
		Malformed: true,
	}, time.UTC)

	assert.Equal(t, malformedLogTimeText, text)
	assert.Equal(t, tcell.ColorRed, color)
}

// TestHistogramExternalCursorFollowsOnlyResolvedFocusedRows verifies that an
// unresolved record hides the cursor, and a later resolved selection restores
// it without making the cursor visible while the log table is blurred.
func TestHistogramExternalCursorFollowsOnlyResolvedFocusedRows(t *testing.T) {
	t1 := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Minute)
	mv := &MainView{
		logsTable: ui.NewTable(),
		histogram: NewHistogram(),
	}
	mv.logsTable.SetCell(2, 0, ui.NewTableCell("").SetReference(core.LogMsg{Time: t1}))
	mv.logsTable.SetCell(3, 0, ui.NewTableCell("").SetReference(core.LogMsg{Malformed: true}))
	mv.logsTable.SetCell(4, 0, ui.NewTableCell("").SetReference(core.LogMsg{Time: t2}))
	mv.logsTable.Focus(nil)

	mv.bumpHistogramExternalCursor(2)
	assert.True(t, mv.histogram.externalCursorVisible)
	assert.Equal(t, int(t1.Unix()), mv.histogram.externalCursor)

	mv.bumpHistogramExternalCursor(3)
	assert.False(t, mv.histogram.externalCursorVisible)

	mv.bumpHistogramExternalCursor(4)
	assert.True(t, mv.histogram.externalCursorVisible)
	assert.Equal(t, int(t2.Unix()), mv.histogram.externalCursor)

	mv.logsTable.Blur()
	mv.bumpHistogramExternalCursor(2)
	assert.False(t, mv.histogram.externalCursorVisible)
}

// TestRowDetailsColorsDecreasedTimeByAmount verifies that the time row shows
// both timestamps using the same decrease-severity colors as the log table.
func TestRowDetailsColorsDecreasedTimeByAmount(t *testing.T) {
	effectiveTime := time.Date(2026, 9, 19, 12, 5, 0, 0, time.UTC)
	newView := func(msg core.LogMsg) *RowDetailsView {
		return NewRowDetailsView(
			&MainView{params: MainViewParams{App: tview.NewApplication()}},
			&RowDetailsViewParams{
				Data: QueryFull{SelectQuery: DefaultSelectQuery},
				ExistingNamesSet: map[string]struct{}{
					FieldNameTime:    {},
					FieldNameMessage: {},
				},
				Msg: &msg,
			},
		)
	}
	findTimeRow := func(view *RowDetailsView) (int, bool) {
		for row := 0; row < view.tbl.GetRowCount(); row++ {
			if view.tbl.GetCell(row, rdvColIdxName).Text == FieldNameTime {
				return row, true
			}
		}
		return 0, false
	}

	tests := []struct {
		name     string
		decrease time.Duration
		color    tcell.Color
	}{
		{name: "small", decrease: time.Millisecond, color: tcell.ColorBlue},
		{name: "medium", decrease: 10 * time.Second, color: tcell.ColorYellow},
		{name: "large", decrease: 20 * time.Second, color: tcell.ColorRed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			origTime := effectiveTime.Add(-test.decrease)
			view := newView(core.LogMsg{
				Time:              effectiveTime,
				OrigDecreasedTime: origTime,
			})
			row, ok := findTimeRow(view)
			assert.True(t, ok)
			assert.Equal(
				t,
				origTime.String()+" (DECREASED FROM: "+effectiveTime.String()+")",
				view.tbl.GetCell(row, rdvColIdxValue).Text,
			)
			assert.Equal(t, test.color, view.tbl.GetCell(row, rdvColIdxValue).Color)
		})
	}
}

// TestRowDetailsShowsMalformedTimeMarker verifies that the real row-details
// widget hides a malformed record's internal anchoring timestamp.
func TestRowDetailsShowsMalformedTimeMarker(t *testing.T) {
	msg := core.LogMsg{
		Time:      time.Date(2026, 9, 19, 12, 5, 0, 0, time.UTC),
		Malformed: true,
		Msg:       "raw malformed line",
	}
	view := NewRowDetailsView(
		&MainView{params: MainViewParams{App: tview.NewApplication()}},
		&RowDetailsViewParams{
			Data: QueryFull{SelectQuery: DefaultSelectQuery},
			ExistingNamesSet: map[string]struct{}{
				FieldNameTime:    {},
				FieldNameMessage: {},
			},
			Msg: &msg,
		},
	)

	for row := 0; row < view.tbl.GetRowCount(); row++ {
		if view.tbl.GetCell(row, rdvColIdxName).Text != FieldNameTime {
			continue
		}
		assert.Equal(t, malformedLogTimeText, view.tbl.GetCell(row, rdvColIdxValue).Text)
		assert.Equal(t, tcell.ColorRed, view.tbl.GetCell(row, rdvColIdxValue).Color)
		return
	}
	t.Fatal("time row not found")
}
