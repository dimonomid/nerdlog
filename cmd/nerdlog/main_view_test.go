package main

import (
	"strings"
	"testing"
	"time"

	"github.com/dimonomid/clock"
	"github.com/dimonomid/nerdlog/cmd/nerdlog/ui"
	"github.com/dimonomid/nerdlog/core"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func simulationScreenLine(screen tcell.SimulationScreen, row int) string {
	width, _ := screen.Size()
	line := make([]rune, 0, width)
	for column := 0; column < width; column++ {
		mainRune, _, _, _ := screen.GetContent(column, row)
		if mainRune == 0 {
			mainRune = ' '
		}
		line = append(line, mainRune)
	}
	return string(line)
}

// TestMainViewApplyLogsPreservesRightAlignedViewportWhenLoadingEarlier covers
// the MOAR regression where loading earlier rows changed the table from its
// rightmost, backfilled layout (s3) to the preceding message-only layout (s2).
// The two layouts share a column offset, so the rendered header is compared to
// ensure applyLogs preserves the table's additional right-edge state.
func TestMainViewApplyLogsPreservesRightAlignedViewportWhenLoadingEarlier(t *testing.T) {
	selectQuery, err := ParseSelectQuery(
		"time STICKY, program, message, lstream, hostname, logfile, loglineno, pid",
	)
	require.NoError(t, err)
	mv := &MainView{
		params: MainViewParams{
			Clock:   clock.New(),
			Options: NewOptionsShared(Options{Timezone: time.UTC}),
		},
		selectQuery:     selectQuery,
		logsTable:       ui.NewTable().SetSelectable(true, false),
		histogram:       NewHistogram(),
		statusLineRight: tview.NewTextView(),
		cmdInput:        tview.NewInputField(),
	}
	logMessage := func(index int) core.LogMsg {
		return core.LogMsg{
			Time:          time.Date(2026, 9, 20, 12, index, 0, 0, time.UTC),
			LogStreamName: "dimon@127.0.0.1:2231",
			LogFilename:   "/var/log/syslog",
			LogLinenumber: 75850 + index,
			Msg:           strings.Repeat("wide message ", 12),
			Context: map[string]string{
				"program":  "systemd",
				"hostname": "dimon-ThinkStation-P620",
				"pid":      "4453",
			},
		}
	}
	initialLogs := []core.LogMsg{logMessage(1), logMessage(2)}
	mv.curLogResp = &core.LogRespTotal{Logs: initialLogs}
	mv.formatLogs()
	// This width leaves the message column truncated while allowing the narrow
	// metadata columns to fit when the table reaches its right-aligned layout.
	mv.logsTable.SetRect(0, 0, 100, 4)
	mv.logsTable.Select(2, 0)

	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	t.Cleanup(screen.Fini)
	screen.SetSize(100, 4)
	mv.logsTable.Draw(screen)

	// The first Right shows the wide message alone (s2). The second reaches the
	// right edge and backfills the remaining width with message + metadata (s3).
	mv.logsTable.InputHandler()(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone), func(tview.Primitive) {})
	mv.logsTable.Draw(screen)
	firstRightHeader := simulationScreenLine(screen, 0)
	mv.logsTable.InputHandler()(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone), func(tview.Primitive) {})
	mv.logsTable.Draw(screen)
	rightAlignedHeader := simulationScreenLine(screen, 0)
	require.NotEqual(t, firstRightHeader, rightAlignedHeader)
	require.Contains(t, rightAlignedHeader, "hostname")
	require.Contains(t, rightAlignedHeader, "logfile")

	// MOAR returns the complete result set with earlier records prepended.
	mv.applyLogs(&core.LogRespTotal{
		LoadedEarlier: true,
		Logs:          []core.LogMsg{logMessage(0), initialLogs[0], initialLogs[1]},
	})
	mv.logsTable.Draw(screen)

	// Comparing the rendered header catches a fallback to s2 even though its
	// stored column offset is the same as the s3 offset.
	assert.Equal(t, rightAlignedHeader, simulationScreenLine(screen, 0))
	selectedRow, _ := mv.logsTable.GetSelection()
	assert.Equal(t, 3, selectedRow)
}

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
