package main

import (
	"testing"
	"time"

	"github.com/dimonomid/nerdlog/core"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

// TestLogMsgDisplayTimeUsesOriginalDecreasedTime verifies that the table shows
// an out-of-order record's parsed timestamp in red instead of its clamped time.
func TestLogMsgDisplayTimeUsesOriginalDecreasedTime(t *testing.T) {
	effectiveTime := time.Date(2026, 9, 19, 12, 5, 0, 0, time.UTC)
	origTime := effectiveTime.Add(-20 * time.Minute)

	displayTime, color := logMsgDisplayTime(core.LogMsg{
		Time:              effectiveTime,
		OrigDecreasedTime: origTime,
	})

	assert.Equal(t, origTime, displayTime)
	assert.Equal(t, tcell.ColorRed, color)
}

// TestLogMsgDisplayTimeUsesEffectiveTime verifies the ordinary timestamp color
// and value remain unchanged.
func TestLogMsgDisplayTimeUsesEffectiveTime(t *testing.T) {
	effectiveTime := time.Date(2026, 9, 19, 12, 5, 0, 0, time.UTC)

	displayTime, color := logMsgDisplayTime(core.LogMsg{Time: effectiveTime})

	assert.Equal(t, effectiveTime, displayTime)
	assert.Equal(t, tcell.ColorLightBlue, color)
}

// TestRowDetailsShowsDecreasedTimeInRed verifies that the time row shows both
// timestamps in red for a decreased record.
func TestRowDetailsShowsDecreasedTimeInRed(t *testing.T) {
	effectiveTime := time.Date(2026, 9, 19, 12, 5, 0, 0, time.UTC)
	origTime := effectiveTime.Add(-20 * time.Minute)
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
	assert.Equal(t, tcell.ColorRed, view.tbl.GetCell(row, rdvColIdxValue).Color)
}
