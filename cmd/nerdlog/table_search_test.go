package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/dimonomid/nerdlog/clhistory"
	"github.com/dimonomid/nerdlog/cmd/nerdlog/ui"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSearchTestTable(rows ...[]string) *ui.Table {
	table := ui.NewTable()
	table.SetCell(0, 0, ui.NewTableCell("header").SetSelectable(false))
	table.SetCell(rowIdxLoadOlder, 0, ui.NewTableCell("< MOAR ! >"))
	for rowIndex, values := range rows {
		for column, value := range values {
			table.SetCell(rowIndex+2, column, ui.NewTableCell(tview.Escape(value)))
		}
	}
	return table
}

func TestTableSearchFindsOrderedSmartCaseMatches(t *testing.T) {
	table := newSearchTestTable(
		[]string{"Alpha alpha", "界alpha"},
		[]string{"no match", "ALPHA"},
	)
	table.Select(2, 0)
	var search tableSearchState

	search.rebuild(table, "alpha", 2)
	require.Len(t, search.matches, 4)
	assert.Equal(t, tableSearchMatch{row: 2, column: 0, start: 0, end: 5}, search.matches[0])
	assert.Equal(t, tableSearchMatch{row: 2, column: 0, start: 6, end: 11}, search.matches[1])
	assert.Equal(t, tableSearchMatch{row: 2, column: 1, start: 2, end: 7}, search.matches[2])
	assert.Equal(t, 0, search.active)

	search.rebuild(table, "Alpha", 2)
	require.Len(t, search.matches, 1)
	assert.Equal(t, tableSearchMatch{row: 2, column: 0, start: 0, end: 5}, search.matches[0])
}

func TestTableSearchMovesByOccurrenceAndWraps(t *testing.T) {
	table := newSearchTestTable([]string{"one one"}, []string{"one"})
	table.Select(2, 0)
	var search tableSearchState
	search.rebuild(table, "one", 2)

	match, ok := search.move(table, true)
	require.True(t, ok)
	assert.Equal(t, tableSearchMatch{row: 2, column: 0, start: 4, end: 7}, match)

	match, ok = search.move(table, false)
	require.True(t, ok)
	assert.Equal(t, tableSearchMatch{row: 2, column: 0, start: 0, end: 3}, match)

	table.Select(3, 0)
	match, ok = search.move(table, true)
	require.True(t, ok)
	assert.Equal(t, 3, match.row)
}

func TestTableSearchSuppressesHighlightsUntilNextMove(t *testing.T) {
	table := newSearchTestTable([]string{"one one"})
	table.SetRect(0, 0, 10, 3)
	table.SetSelectable(true, false).Select(2, 0)
	var search tableSearchState
	search.rebuild(table, "one", 2)

	search.suppressHighlights(table)
	assert.True(t, search.highlightsSuppressed)

	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	t.Cleanup(screen.Fini)
	screen.SetSize(10, 3)
	table.Draw(screen)
	_, _, style, _ := screen.GetContent(0, 2)
	assert.NotEqual(t, tableSearchActiveStyle, style)
	assert.NotEqual(t, tableSearchMatchStyle, style)

	_, ok := search.move(table, true)
	require.True(t, ok)
	assert.False(t, search.highlightsSuppressed)
	table.Draw(screen)
	_, _, style, _ = screen.GetContent(4, 2)
	assert.Equal(t, tableSearchActiveStyle, style)
}

func TestTableSearchSkipsControlRowsAndUsesDisplayedEscapedText(t *testing.T) {
	table := newSearchTestTable([]string{"literal [red] text"})
	var search tableSearchState

	search.rebuild(table, "red", 0)
	require.Len(t, search.matches, 1)
	assert.Equal(t, 2, search.matches[0].row)
	assert.Equal(t, 9, search.matches[0].start)
	assert.Equal(t, 12, search.matches[0].end)

	search.rebuild(table, "MOAR", 0)
	assert.Empty(t, search.matches)
}

func TestTableSearchDisplayedTextPreservesLiteralBracketsAndStripsTags(t *testing.T) {
	assert.Equal(t, "literal [red] text", tableSearchDisplayedText(tview.Escape("literal [red] text")))
	assert.Equal(t, "unfinished [ bracket", tableSearchDisplayedText("unfinished [ bracket"))
	assert.Equal(t, "colored text", tableSearchDisplayedText("[red]colored[-] text"))
}

func TestTableSearchUsesNonOverlappingMatches(t *testing.T) {
	table := newSearchTestTable([]string{"aaaa"})
	var search tableSearchState
	search.rebuild(table, "aa", 2)

	assert.Equal(t, []tableSearchMatch{
		{row: 2, column: 0, start: 0, end: 2},
		{row: 2, column: 0, start: 2, end: 4},
	}, search.matches)
}

func TestTableSearchActiveHighlightSurvivesSelectedRowStyle(t *testing.T) {
	table := newSearchTestTable([]string{"hit"})
	table.SetRect(0, 0, 10, 3)
	table.SetSelectable(true, false).Select(2, 0)
	var search tableSearchState
	search.rebuild(table, "hit", 2)

	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	t.Cleanup(screen.Fini)
	screen.SetSize(10, 3)
	table.Draw(screen)

	for x := 0; x < 3; x++ {
		_, _, style, _ := screen.GetContent(x, 2)
		assert.Equal(t, tableSearchActiveStyle, style)
	}
}

func newTableSearchMainView(t *testing.T, rows ...[]string) *MainView {
	t.Helper()
	cmdHistory, err := clhistory.New(clhistory.CLHistoryParams{})
	require.NoError(t, err)
	searchHistory, err := clhistory.New(clhistory.CLHistoryParams{})
	require.NoError(t, err)
	table := newSearchTestTable(rows...)
	table.SetSelectable(true, false).Select(2, 0)
	app := tview.NewApplication()
	mv := &MainView{
		params: MainViewParams{
			App:           app,
			CmdHistory:    cmdHistory,
			SearchHistory: searchHistory,
		},
		logsTable: table,
		cmdInput:  tview.NewInputField(),
	}
	mv.cmdInput.SetChangedFunc(mv.cmdlineChanged)
	mv.focusedBeforeCmd = table
	return mv
}

func TestMainViewTableSearchPreviewsThenAccepts(t *testing.T) {
	mv := newTableSearchMainView(t, []string{"first"}, []string{"target"})
	mv.focusTableSearch()
	mv.cmdInput.SetText("/target")
	mv.cmdlineChanged(mv.cmdInput.GetText())

	row, _ := mv.logsTable.GetSelection()
	assert.Equal(t, 2, row, "incremental search must not move the row cursor")
	require.Equal(t, "target", mv.tableSearch.query)
	require.Len(t, mv.tableSearch.matches, 1)

	mv.cmdlineDone(tcell.KeyEnter)
	row, _ = mv.logsTable.GetSelection()
	assert.Equal(t, 3, row)
	assert.Equal(t, cmdlineModeNone, mv.cmdlineMode)
	assert.Nil(t, mv.tableSearchEdit)
}

func TestMainViewTableSearchCancelRestoresSearchCursorAndOffset(t *testing.T) {
	mv := newTableSearchMainView(t, []string{"old"}, []string{"new"})
	mv.tableSearch.rebuild(mv.logsTable, "old", 2)
	mv.logsTable.SetOffset(1, 0)
	mv.focusTableSearch()
	mv.cmdInput.SetText("/new")
	mv.cmdlineChanged(mv.cmdInput.GetText())
	mv.logsTable.Select(3, 0)
	mv.logsTable.SetOffset(0, 0)

	mv.cmdlineDone(tcell.KeyEsc)
	assert.Equal(t, "old", mv.tableSearch.query)
	row, _ := mv.logsTable.GetSelection()
	assert.Equal(t, 2, row)
	offsetRow, offsetColumn := mv.logsTable.GetOffset()
	assert.Equal(t, 1, offsetRow)
	assert.Equal(t, 0, offsetColumn)
}

func TestMainViewEmptyTableSearchRepeatsAcceptedSearch(t *testing.T) {
	mv := newTableSearchMainView(t, []string{"hit hit"})
	mv.tableSearch.rebuild(mv.logsTable, "hit", 2)
	mv.focusTableSearch()
	mv.cmdInput.SetText("/")

	mv.cmdlineDone(tcell.KeyEnter)
	match, ok := mv.tableSearch.activeMatch()
	require.True(t, ok)
	assert.Equal(t, 4, match.start)
}

func TestNohlsearchCommandSuppressesHighlightsButKeepsSearch(t *testing.T) {
	for _, command := range []string{"noh", "nohlsearch"} {
		t.Run(command, func(t *testing.T) {
			mv := newTableSearchMainView(t, []string{"hit hit"})
			mv.tableSearch.rebuild(mv.logsTable, "hit", 2)
			app := &nerdlogApp{mainView: mv}

			app.handleCmd(command)
			assert.True(t, mv.tableSearch.highlightsSuppressed)
			assert.Equal(t, "hit", mv.tableSearch.query)
			require.Len(t, mv.tableSearch.matches, 2)

			require.True(t, mv.repeatTableSearch(true))
			assert.False(t, mv.tableSearch.highlightsSuppressed)
			match, ok := mv.tableSearch.activeMatch()
			require.True(t, ok)
			assert.Equal(t, 4, match.start)
		})
	}
}

func TestCancelTableSearchRestoresSuppressedHighlights(t *testing.T) {
	mv := newTableSearchMainView(t, []string{"old"}, []string{"new"})
	mv.tableSearch.rebuild(mv.logsTable, "old", 2)
	mv.suppressTableSearchHighlights()
	mv.focusTableSearch()
	mv.cmdInput.SetText("/new")
	mv.cmdlineChanged(mv.cmdInput.GetText())
	assert.False(t, mv.tableSearch.highlightsSuppressed)

	mv.cmdlineDone(tcell.KeyEsc)
	assert.Equal(t, "old", mv.tableSearch.query)
	assert.True(t, mv.tableSearch.highlightsSuppressed)
}

func TestTableSearchHistoryNavigationAndIncrementalPreview(t *testing.T) {
	mv := newTableSearchMainView(t, []string{"first"}, []string{"target"})
	require.NoError(t, mv.params.SearchHistory.Add("first"))
	require.NoError(t, mv.params.SearchHistory.Add("target"))
	mv.focusTableSearch()
	mv.cmdInput.SetText("/draft")

	assert.Nil(t, mv.cmdlineInputCapture(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)))
	assert.Equal(t, "/target", mv.cmdInput.GetText())
	assert.Equal(t, "target", mv.tableSearch.query)
	require.Len(t, mv.tableSearch.matches, 1)
	assert.Equal(t, 3, mv.tableSearch.matches[0].row)

	assert.Nil(t, mv.cmdlineInputCapture(tcell.NewEventKey(tcell.KeyCtrlP, 0, tcell.ModNone)))
	assert.Equal(t, "/first", mv.cmdInput.GetText())

	assert.Nil(t, mv.cmdlineInputCapture(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)))
	assert.Equal(t, "/target", mv.cmdInput.GetText())
	assert.Nil(t, mv.cmdlineInputCapture(tcell.NewEventKey(tcell.KeyCtrlN, 0, tcell.ModNone)))
	assert.Equal(t, "/draft", mv.cmdInput.GetText(), "moving past newest must restore typed text")
}

func TestTableSearchHistoryIsSeparateFromCommandHistory(t *testing.T) {
	mv := newTableSearchMainView(t, []string{"row"})
	require.NoError(t, mv.params.CmdHistory.Add("refresh"))
	require.NoError(t, mv.params.SearchHistory.Add("needle"))

	mv.focusCmdline()
	assert.Nil(t, mv.cmdlineInputCapture(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)))
	assert.Equal(t, ":refresh", mv.cmdInput.GetText())
	mv.cmdlineDone(tcell.KeyEsc)

	mv.focusTableSearch()
	assert.Nil(t, mv.cmdlineInputCapture(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)))
	assert.Equal(t, "/needle", mv.cmdInput.GetText())
}

func TestTableSearchHistoryRecordsOnlySubmittedNonEmptySearches(t *testing.T) {
	historyFile := t.TempDir() + "/search_history"
	history, err := clhistory.New(clhistory.CLHistoryParams{Filename: historyFile})
	require.NoError(t, err)
	mv := newTableSearchMainView(t, []string{"hit"})
	mv.params.SearchHistory = history

	mv.focusTableSearch()
	mv.cmdInput.SetText("/hit")
	mv.cmdlineDone(tcell.KeyEnter)

	mv.focusTableSearch()
	mv.cmdInput.SetText("/")
	mv.cmdlineDone(tcell.KeyEnter)

	mv.focusTableSearch()
	mv.cmdInput.SetText("/cancelled")
	mv.cmdlineDone(tcell.KeyEsc)

	mv.focusTableSearch()
	mv.cmdInput.SetText("/missing")
	mv.cmdlineDone(tcell.KeyEnter)

	file, err := os.Open(historyFile)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	items, err := clhistory.NewHistoryDecoder(file).Decode()
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "hit", items[0].Str)
	assert.Equal(t, "missing", items[1].Str)
}

func TestCommandLineCommandModeStillDispatchesCommands(t *testing.T) {
	mv := newTableSearchMainView(t, []string{"row"})
	var command string
	mv.params.OnCmd = func(cmd string, _ CmdOpts) { command = cmd }
	mv.cmdlineMode = cmdlineModeCommand
	mv.cmdInput.SetText(":refresh")

	mv.cmdlineDone(tcell.KeyEnter)
	assert.Equal(t, "refresh", command)
	assert.Equal(t, cmdlineModeNone, mv.cmdlineMode)
	assert.Empty(t, mv.cmdInput.GetText())
}

func BenchmarkTableSearchRebuild(b *testing.B) {
	table := ui.NewTable()
	for row := 0; row < 5000; row++ {
		for column := 0; column < 4; column++ {
			table.SetCell(row+2, column, ui.NewTableCell(fmt.Sprintf(
				"host-%d service-%d repeated searchable message text",
				row%100, column,
			)))
		}
	}
	var search tableSearchState
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		search.rebuild(table, "e", 2)
	}
}
