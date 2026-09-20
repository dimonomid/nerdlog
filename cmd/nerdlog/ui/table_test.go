package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTableTestScreen(t *testing.T, width, height int) tcell.SimulationScreen {
	t.Helper()
	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	t.Cleanup(screen.Fini)
	screen.SetSize(width, height)
	return screen
}

func populateTables(owned *Table, upstream *tview.Table) {
	texts := [][]string{
		{"header", "center", "right", "extra"},
		{"one", "[red]red[-]", "界-wide", "tail-a"},
		{"two-long-enough-to-truncate", "plain", "third", "tail-b"},
		{"three", "more", "last", "tail-c"},
		{"four", "values", "here", "tail-d"},
	}

	for row, values := range texts {
		for column, text := range values {
			ownedCell := NewTableCell(text)
			upstreamCell := tview.NewTableCell(text)
			if row == 0 {
				ownedCell.SetTextColor(tcell.ColorLightBlue).SetAttributes(tcell.AttrBold).SetSelectable(false)
				upstreamCell.SetTextColor(tcell.ColorLightBlue).SetAttributes(tcell.AttrBold).SetSelectable(false)
			}
			if column == 1 {
				ownedCell.SetAlign(AlignCenter)
				upstreamCell.SetAlign(tview.AlignCenter)
			}
			if column == 2 {
				ownedCell.SetAlign(AlignRight).SetBackgroundColor(tcell.ColorDarkBlue)
				upstreamCell.SetAlign(tview.AlignRight).SetBackgroundColor(tcell.ColorDarkBlue)
			}
			if row == 2 && column == 0 {
				ownedCell.SetMaxWidth(9)
				upstreamCell.SetMaxWidth(9)
			}
			owned.SetCell(row, column, ownedCell)
			upstream.SetCell(row, column, upstreamCell)
		}
	}
}

func configureTables(owned *Table, upstream *tview.Table) {
	owned.SetRect(0, 0, 27, 5)
	upstream.SetRect(0, 0, 27, 5)
	owned.SetFixed(1, 1).SetSelectable(true, false).SetOffset(1, 1).Select(3, 0)
	upstream.SetFixed(1, 1).SetSelectable(true, false).SetOffset(1, 1).Select(3, 0)
}

func assertScreensEqual(t *testing.T, expected, actual tcell.SimulationScreen) {
	t.Helper()
	width, height := expected.Size()
	actualWidth, actualHeight := actual.Size()
	require.Equal(t, width, actualWidth)
	require.Equal(t, height, actualHeight)

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			expectedMain, expectedComb, expectedStyle, expectedWidth := expected.GetContent(x, y)
			actualMain, actualComb, actualStyle, actualWidth := actual.GetContent(x, y)
			assert.Equal(t, expectedMain, actualMain, "main rune at (%d,%d)", x, y)
			assert.Equal(t, expectedComb, actualComb, "combining runes at (%d,%d)", x, y)
			assert.Equal(t, expectedStyle, actualStyle, "style at (%d,%d)", x, y)
			assert.Equal(t, expectedWidth, actualWidth, "width at (%d,%d)", x, y)
		}
	}
}

func TestTableDrawMatchesPinnedTview(t *testing.T) {
	owned := NewTable()
	upstream := tview.NewTable()
	populateTables(owned, upstream)
	configureTables(owned, upstream)

	ownedScreen := newTableTestScreen(t, 30, 7)
	upstreamScreen := newTableTestScreen(t, 30, 7)
	owned.Draw(ownedScreen)
	upstream.Draw(upstreamScreen)

	assertScreensEqual(t, upstreamScreen, ownedScreen)
}

func TestTableNavigationMatchesPinnedTview(t *testing.T) {
	owned := NewTable()
	upstream := tview.NewTable()
	populateTables(owned, upstream)
	configureTables(owned, upstream)
	// Keep this parity test away from the intentional right-edge persistence
	// difference covered by TestTableVerticalNavigationPreservesHorizontalViewport.
	owned.SetOffset(1, 0)
	upstream.SetOffset(1, 0)

	ownedScreen := newTableTestScreen(t, 30, 7)
	upstreamScreen := newTableTestScreen(t, 30, 7)
	owned.Draw(ownedScreen)
	upstream.Draw(upstreamScreen)

	events := []*tcell.EventKey{
		tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone),
		tcell.NewEventKey(tcell.KeyRune, 'k', tcell.ModNone),
		tcell.NewEventKey(tcell.KeyPgUp, 0, tcell.ModNone),
		tcell.NewEventKey(tcell.KeyRune, 'G', tcell.ModNone),
		tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone),
	}
	for _, event := range events {
		owned.InputHandler()(event, func(tview.Primitive) {})
		upstream.InputHandler()(event, func(tview.Primitive) {})
		owned.Draw(ownedScreen)
		upstream.Draw(upstreamScreen)

		ownedRow, ownedColumn := owned.GetSelection()
		upstreamRow, upstreamColumn := upstream.GetSelection()
		assert.Equal(t, upstreamRow, ownedRow)
		assert.Equal(t, upstreamColumn, ownedColumn)
		ownedRowOffset, ownedColumnOffset := owned.GetOffset()
		upstreamRowOffset, upstreamColumnOffset := upstream.GetOffset()
		assert.Equal(t, upstreamRowOffset, ownedRowOffset)
		assert.Equal(t, upstreamColumnOffset, ownedColumnOffset)
		assertScreensEqual(t, upstreamScreen, ownedScreen)
	}
}

func TestTableVerticalNavigationPreservesHorizontalViewport(t *testing.T) {
	table := NewTable().SetFixed(0, 1).SetSelectable(true, false)
	table.SetRect(0, 0, 30, 1)
	populate := func(rows int) {
		table.Clear()
		for row := 0; row < rows; row++ {
			for column := 0; column < 6; column++ {
				text := "meta"
				switch column {
				case 0:
					text = "time"
				case 1:
					text = "program"
				case 2:
					text = "a message much wider than the available table viewport"
				}
				table.SetCell(row, column, NewTableCell(text))
			}
		}
	}
	populate(2)
	table.Select(0, 0)
	screen := newTableTestScreen(t, 30, 1)
	table.Draw(screen)
	assert.Equal(t, []int{0, 1, 2}, table.visibleColumnIndices)

	table.InputHandler()(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone), func(tview.Primitive) {})
	table.Draw(screen)
	assert.Equal(t, []int{0, 2}, table.visibleColumnIndices)

	table.InputHandler()(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone), func(tview.Primitive) {})
	table.Draw(screen)
	require.Equal(t, []int{0, 2, 3, 4, 5}, table.visibleColumnIndices)

	table.InputHandler()(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone), func(tview.Primitive) {})
	table.Draw(screen)
	selectedRow, _ := table.GetSelection()
	assert.Equal(t, 1, selectedRow)
	assert.Equal(t, []int{0, 2, 3, 4, 5}, table.visibleColumnIndices)

	table.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'k', tcell.ModNone), func(tview.Primitive) {})
	table.Draw(screen)
	selectedRow, _ = table.GetSelection()
	assert.Equal(t, 0, selectedRow)
	assert.Equal(t, []int{0, 2, 3, 4, 5}, table.visibleColumnIndices)

	// Leaving the right-aligned layout should return to the preceding viewport,
	// and moving right should enter it again.
	table.InputHandler()(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone), func(tview.Primitive) {})
	table.Draw(screen)
	assert.Equal(t, []int{0, 2}, table.visibleColumnIndices)
	table.InputHandler()(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone), func(tview.Primitive) {})
	table.Draw(screen)
	assert.Equal(t, []int{0, 2, 3, 4, 5}, table.visibleColumnIndices)

	// Loading earlier data inserts rows above the viewport and adjusts only the
	// vertical offset and selection. The right-aligned viewport must survive.
	selectedRow, _ = table.GetSelection()
	rowOffset, _ := table.GetOffset()
	populate(3)
	table.SetRowOffset(rowOffset + 1)
	table.Select(selectedRow+1, 0)
	table.Draw(screen)
	assert.Equal(t, []int{0, 2, 3, 4, 5}, table.visibleColumnIndices)
}

func TestTableCellStyleSpansApplyAfterSelectionAndClip(t *testing.T) {
	table := NewTable().SetSelectable(true, false)
	table.SetRect(0, 0, 8, 2)
	table.SetCell(0, 0, NewTableCell("abcdefghij"))
	table.SetCell(1, 0, NewTableCell("界-wide"))
	table.Select(0, 0)

	ordinary := tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorYellow)
	active := tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorOrange).Bold(true)
	table.SetCellStyleSpans([]CellStyleSpan{
		{Row: 0, Column: 0, Start: 2, End: 5, Style: active},
		{Row: 0, Column: 0, Start: 7, End: 12, Style: ordinary},
		{Row: 1, Column: 0, Start: 0, End: 2, Style: ordinary},
	})

	screen := newTableTestScreen(t, 8, 2)
	table.Draw(screen)

	for x := 2; x < 5; x++ {
		_, _, style, _ := screen.GetContent(x, 0)
		assert.Equal(t, active, style, "active span style at x=%d", x)
	}
	_, _, clippedStyle, _ := screen.GetContent(7, 0)
	assert.Equal(t, ordinary, clippedStyle)
	_, _, wideStyle, _ := screen.GetContent(0, 1)
	assert.Equal(t, ordinary, wideStyle)

	table.SetCellStyleSpans(nil)
	table.Draw(screen)
	_, _, styleWithoutSpan, _ := screen.GetContent(2, 0)
	assert.NotEqual(t, active, styleWithoutSpan)
}

func TestTableScrollToColumnPreservesVerticalOffset(t *testing.T) {
	table := NewTable().SetFixed(1, 1).SetOffset(4, 0)
	table.ScrollToColumn(3)
	rowOffset, columnOffset := table.GetOffset()
	assert.Equal(t, 4, rowOffset)
	assert.Equal(t, 2, columnOffset)

	table.ScrollToColumn(0)
	rowOffset, columnOffset = table.GetOffset()
	assert.Equal(t, 4, rowOffset)
	assert.Equal(t, 2, columnOffset)
}

func TestTableScrollToRowDoesNotMoveSelection(t *testing.T) {
	table := NewTable().SetFixed(1, 0).SetSelectable(true, false).Select(2, 0)
	table.SetRect(0, 0, 10, 4)
	for row := 0; row < 10; row++ {
		table.SetCell(row, 0, NewTableCell("row"))
	}
	screen := newTableTestScreen(t, 10, 4)
	table.Draw(screen)

	table.ScrollToRow(8)
	selectedRow, _ := table.GetSelection()
	rowOffset, _ := table.GetOffset()
	assert.Equal(t, 2, selectedRow)
	assert.Equal(t, 5, rowOffset)
}
