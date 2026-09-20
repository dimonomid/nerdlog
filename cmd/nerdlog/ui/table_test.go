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

	ownedScreen := newTableTestScreen(t, 30, 7)
	upstreamScreen := newTableTestScreen(t, 30, 7)
	owned.Draw(ownedScreen)
	upstream.Draw(upstreamScreen)

	events := []*tcell.EventKey{
		tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone),
		tcell.NewEventKey(tcell.KeyRune, 'k', tcell.ModNone),
		tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone),
		tcell.NewEventKey(tcell.KeyPgUp, 0, tcell.ModNone),
		tcell.NewEventKey(tcell.KeyRune, 'G', tcell.ModNone),
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
