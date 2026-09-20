package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type TableWithDropdown struct {
	*Table

	// The options from which the user can choose.
	options []*dropDownOption

	// Strings to be placed before and after each drop-down option.
	optionPrefix, optionSuffix string

	// The index of the currently selected option. Negative if no option is
	// currently selected.
	currentOption int

	list *tview.List

	dropdownRow int
	dropdownCol int
}

func NewTableWithDropdown() *TableWithDropdown {
	t := &TableWithDropdown{
		Table: NewTable(),

		optionPrefix: " ",
		optionSuffix: " ",

		currentOption: -1,

		list: tview.NewList(),

		dropdownRow: -1,
		dropdownCol: -1,
	}

	t.list.ShowSecondaryText(false).
		SetMainTextColor(tview.Styles.PrimitiveBackgroundColor).
		SetSelectedTextColor(tview.Styles.PrimitiveBackgroundColor).
		SetSelectedBackgroundColor(tview.Styles.PrimaryTextColor).
		SetHighlightFullLine(true).
		SetBackgroundColor(tview.Styles.MoreContrastBackgroundColor)

	t.list.SetFocusFunc(func() {
		// We want selection to still show when the dropdown is focused
		t.SetSelectable(true, false)
	})

	return t
}

func (t *TableWithDropdown) IsDropdownOpen() bool {
	return t.dropdownRow >= 0 && t.dropdownCol >= 0
}

func (t *TableWithDropdown) GetDropdownList() *tview.List {
	return t.list
}

// Focus is called by the application when the primitive receives focus.
func (t *TableWithDropdown) Focus(delegate func(p tview.Primitive)) {
	if t.IsDropdownOpen() {
		delegate(t.list)
	} else {
		t.Box.Focus(delegate)
	}
}

// HasFocus returns whether or not this primitive has focus.
func (t *TableWithDropdown) HasFocus() bool {
	if t.IsDropdownOpen() {
		return t.list.HasFocus()
	}
	return t.Box.HasFocus()
}

func (t *TableWithDropdown) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return t.WrapInputHandler(func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		// If the list has focus, let it process its own key events.
		if t.list.HasFocus() {
			if handler := t.list.InputHandler(); handler != nil {
				handler(event, setFocus)
			}
			return
		}

		t.Table.InputHandler()(event, setFocus)
	})
}

func (t *TableWithDropdown) ClearOptions() *TableWithDropdown {
	t.options = nil
	t.list.Clear()
	return t
}

func (t *TableWithDropdown) AddOption(text string, selected func()) *TableWithDropdown {
	t.options = append(t.options, &dropDownOption{Text: text, Selected: selected})
	t.list.AddItem(t.optionPrefix+text+t.optionSuffix, "", 0, nil)
	return t
}

func (t *TableWithDropdown) SetListStyles(unselected, selected tcell.Style) *TableWithDropdown {
	fg, bg, _ := unselected.Decompose()
	t.list.SetMainTextColor(fg).SetBackgroundColor(bg)
	fg, bg, _ = selected.Decompose()
	t.list.SetSelectedTextColor(fg).SetSelectedBackgroundColor(bg)
	return t
}

func (t *TableWithDropdown) CloseDropdownList(setFocus func(p tview.Primitive)) {
	t.closeList(setFocus)
}

func (t *TableWithDropdown) OpenDropdownList(row, col int, setFocus func(p tview.Primitive)) {
	t.dropdownRow = row
	t.dropdownCol = col

	t.list.SetSelectedFunc(func(index int, mainText, secondaryText string, shortcut rune) {
		// An option was selected. Close the list again.
		t.currentOption = index
		t.closeList(setFocus)

		if t.options[t.currentOption].Selected != nil {
			t.options[t.currentOption].Selected()
		}
	})

	setFocus(t.list)
}

func (t *TableWithDropdown) closeList(setFocus func(tview.Primitive)) {
	t.dropdownRow = -1
	t.dropdownCol = -1
	if t.list.HasFocus() {
		setFocus(t)
	}
}

func (t *TableWithDropdown) Draw(screen tcell.Screen) {
	t.Table.Draw(screen)

	if t.IsDropdownOpen() {
		x, y, _ := t.GetCell(t.dropdownRow, t.dropdownCol).GetLastPosition()

		// What's the longest option text?
		maxWidth := 0
		optionWrapWidth := tview.TaggedStringWidth(t.optionPrefix + t.optionSuffix)
		for _, option := range t.options {
			strWidth := tview.TaggedStringWidth(option.Text) + optionWrapWidth
			if strWidth > maxWidth {
				maxWidth = strWidth
			}
		}

		lx := x
		ly := y + 1
		lwidth := maxWidth
		lheight := len(t.options)
		swidth, sheight := screen.Size()
		// We prefer to align the list left side with the main widget left size,
		// but if there is no space, then shift the list to the left.
		if lx+lwidth >= swidth {
			lx = swidth - lwidth
			if lx < 0 {
				lx = 0
			}
		}
		// We prefer to drop down but if there is no space, maybe drop up?
		if ly+lheight >= sheight && ly-2 > lheight-ly {
			ly = y - lheight
			if ly < 0 {
				ly = 0
			}
		}
		if ly+lheight >= sheight {
			lheight = sheight - ly
		}
		t.list.SetRect(lx, ly, lwidth, lheight)
		t.list.Draw(screen)
	}
}
