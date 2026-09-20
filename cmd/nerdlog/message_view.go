package main

import (
	"strings"

	"github.com/dimonomid/nerdlog/cmd/nerdlog/ui"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type MessageViewParams struct {
	App *tview.Application

	MessageID string
	Title     string
	Message   string

	InputFields []MessageViewInputFieldParams
	// Checkboxes are displayed after the message and input fields, immediately
	// above the buttons.
	Checkboxes []MessageViewCheckboxParams

	// OnInputFieldPressed is called whenever any key is pressed on any of the
	// input fields, except for Tab / Shift+Tab.
	//
	// It can choose to handle the keypress, and return either the same or
	// modified event (in which case the default handler will run), or nil
	// (in which case, nothing else will run).
	OnInputFieldPressed func(label string, idx int, value string, event *tcell.EventKey) *tcell.EventKey

	Buttons         []string
	OnButtonPressed func(label string, idx int)
	ButtonDropdowns []MessageViewDropdown

	OnEsc func()

	// Width and Height are 40 and 10 by default
	Width, Height int

	// Scrollable makes the message text navigable when it does not fit in the
	// visible area.
	Scrollable bool

	// By default, tview.AlignLeft (because it happens to be 0)
	Align int

	NoFocus bool

	BackgroundColor tcell.Color
}

type MessageViewInputFieldParams struct {
	Label      string
	IsPassword bool
}

// MessageViewCheckboxParams describes one checkbox embedded in a MessageView.
type MessageViewCheckboxParams struct {
	Label   string
	Checked bool
}

type MessageViewDropdown struct {
	Label      string
	Options    []string
	OnSelected func(index int, label string)
}

type MessageView struct {
	params   MessageViewParams
	mainView *MainView

	msgboxFlex  *tview.Flex
	buttonsFlex *tview.Flex
	frame       *tview.Frame

	textView    *tview.TextView
	scrollbar   *messageViewScrollbar
	inputFields []*tview.InputField
	checkboxes  []*tview.Checkbox
	buttons     []*tview.Button
	focusers    []tview.Primitive

	// onButtonBlurRevert is needed to support the use case when we need to
	// change the button's label until it loses its focus. We use it for e.g.
	// "Copy" -> "Copied" button.
	onButtonBlurRevert *onButtonBlurRevert

	curWidth  int
	curHeight int
}

// messageViewScrollbar is the visual scroll indicator for a scrollable
// message. tview's TextView supports scrolling but does not draw a scrollbar.
type messageViewScrollbar struct {
	*tview.Box
	textView *tview.TextView
}

func (s *messageViewScrollbar) Draw(screen tcell.Screen) {
	s.Box.DrawForSubclass(screen, s)

	x, y, width, height := s.GetInnerRect()
	if width <= 0 || height <= 0 {
		return
	}

	_, _, textWidth, _ := s.textView.GetRect()
	if textWidth <= 0 {
		return
	}

	text := s.textView.GetText(true)
	totalLines := getWrappedTextViewLineCount(text, textWidth)
	if totalLines <= height {
		return
	}

	offset, _ := s.textView.GetScrollOffset()
	maxOffset := totalLines - height
	if offset < 0 {
		offset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	thumbHeight := height * height / totalLines
	if thumbHeight < 1 {
		thumbHeight = 1
	}
	if thumbHeight > height {
		thumbHeight = height
	}
	thumbTop := offset * (height - thumbHeight) / maxOffset
	if thumbTop < 0 {
		thumbTop = 0
	}
	if thumbTop+thumbHeight > height {
		thumbTop = height - thumbHeight
	}

	trackStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray)
	thumbStyle := tcell.StyleDefault.Foreground(tcell.ColorWhite)
	for row := 0; row < height; row++ {
		style := trackStyle
		ch := '│'
		if row >= thumbTop && row < thumbTop+thumbHeight {
			style = thumbStyle
			ch = '█'
		}
		screen.SetContent(x+width-1, y+row, ch, nil, style)
	}
}

// getWrappedTextViewLineCount estimates how many display rows tview's wrapped
// TextView needs for text at the given width. It mirrors the current TextView
// configuration: wrapping is enabled and word wrapping is disabled.
func getWrappedTextViewLineCount(text string, width int) int {
	if width <= 0 {
		return 0
	}

	totalLines := 0
	for _, line := range strings.Split(text, "\n") {
		// Use screen-cell width rather than byte length so the scrollbar also
		// behaves correctly for Unicode text.
		lineWidth := tview.TaggedStringWidth(line)
		lineCount := (lineWidth + width - 1) / width
		if lineCount == 0 {
			lineCount = 1
		}
		totalLines += lineCount
	}
	return totalLines
}

// onButtonBlurRevert specifies the index and old value of a button (that we
// need to revert to when the button loses focus).
type onButtonBlurRevert struct {
	// index of the button to revert the label of.
	index int
	// oldLabel is the label to set.
	oldLabel string
}

// getMaxLineLength returns the length of the longest line in the given string.
func getMaxLineLength(s string) int {
	maxLen := 0
	start := 0

	for i, c := range s {
		if c == '\n' {
			lineLen := i - start
			if lineLen > maxLen {
				maxLen = lineLen
			}
			start = i + 1
		}
	}

	// Handle the last line if it doesn't end with a newline
	if len(s)-start > maxLen {
		maxLen = len(s) - start
	}

	return maxLen
}

// getNumLines returns the number of lines that are needed to draw the given
// text.
func getNumLines(s string, screenWidth int) int {
	if screenWidth <= 0 {
		return 0
	}

	s = strings.TrimSpace(s)
	lines := strings.Split(s, "\n")
	numLines := 0
	for _, line := range lines {
		// Divide line length by screen width and round up
		lineLen := len(line)
		curNumLines := (lineLen + screenWidth - 1) / screenWidth
		if curNumLines == 0 {
			curNumLines = 1
		}

		numLines += curNumLines
	}
	return numLines
}

// GetOptimalMessageViewSize returns the optimal width and height for a
// MessageView based on the screen width and the text to show.
//
// extraWidth and extraHeight specify the width and height needed for other
// elements, padding, border etc.
func GetOptimalMessageViewSize(screenWidth, extraWidth, extraHeight int, text string) (int, int) {
	width := getMaxLineLength(text) + extraWidth
	if width > screenWidth {
		width = screenWidth
	}

	height := extraHeight + getNumLines(text, screenWidth-extraWidth)
	return width, height
}

func NewMessageView(
	mainView *MainView, params *MessageViewParams,
) *MessageView {
	msgv := &MessageView{
		params:   *params,
		mainView: mainView,
	}

	optimalWidth, optimalHeight := msgv.getOptimalSize(params.Message)

	if msgv.params.Width == 0 {
		msgv.params.Width = optimalWidth
	}

	if msgv.params.Height == 0 {
		msgv.params.Height = optimalHeight
	}

	msgv.msgboxFlex = tview.NewFlex().SetDirection(tview.FlexRow)

	msgv.textView = tview.NewTextView()
	msgv.textView.SetText(strings.TrimSpace(params.Message))
	msgv.textView.SetTextAlign(msgv.params.Align)
	msgv.textView.SetDynamicColors(true)
	msgv.textView.SetScrollable(msgv.params.Scrollable)
	if msgv.params.BackgroundColor != tcell.ColorDefault {
		msgv.textView.SetBackgroundColor(msgv.params.BackgroundColor)
	}

	textViewPrimitive := tview.Primitive(msgv.textView)
	if msgv.params.Scrollable {
		msgv.scrollbar = &messageViewScrollbar{
			Box:      tview.NewBox(),
			textView: msgv.textView,
		}
		textViewPrimitive = tview.NewFlex().SetDirection(tview.FlexColumn).
			AddItem(msgv.textView, 0, 1, false).
			AddItem(msgv.scrollbar, 1, 0, false)
	}
	msgv.msgboxFlex.AddItem(
		textViewPrimitive,
		0,
		1,
		!msgv.params.Scrollable && len(params.Buttons) == 0 && len(params.InputFields) == 0 && len(params.Checkboxes) == 0,
	)

	for i, fieldParams := range msgv.params.InputFields {
		fieldIdx := i

		// Spacer
		if fieldIdx > 0 {
			msgv.msgboxFlex.AddItem(nil, 1, 0, false)
		}

		// Label
		if fieldParams.Label != "" {
			label := tview.NewTextView()
			label.SetText(fieldParams.Label)
			msgv.msgboxFlex.AddItem(label, 1, 0, false)
		}

		// Field itself
		field := tview.NewInputField()
		msgv.inputFields = append(msgv.inputFields, field)
		msgv.msgboxFlex.AddItem(field, 1, 0, fieldIdx == 0)
		msgv.focusers = append(msgv.focusers, field)
		tabHandler := msgv.getGenericTabHandler(field)
		if fieldParams.IsPassword {
			field.SetMaskCharacter('*')
		}
		field.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			// Handle Esc key
			switch event.Key() {
			case tcell.KeyEsc:
				if params.OnEsc != nil {
					params.OnEsc()
				}
			}

			// Handle Tab and Shift+Tab
			event = tabHandler(event)
			if event == nil {
				return nil
			}

			// Call user-specified event handler
			event = msgv.params.OnInputFieldPressed(
				fieldParams.Label, fieldIdx, field.GetText(), event,
			)
			if event == nil {
				return nil
			}

			return event
		})
	}

	for i, checkboxParams := range msgv.params.Checkboxes {
		checkboxIdx := i
		checkbox := tview.NewCheckbox().SetChecked(checkboxParams.Checked)
		checkboxLabel := tview.NewTextView().SetText(checkboxParams.Label)
		checkboxOpenBracket := tview.NewTextView().SetText("[")
		checkboxCloseBracket := tview.NewTextView().SetText("]")
		if msgv.params.BackgroundColor != tcell.ColorDefault {
			checkbox.SetBackgroundColor(msgv.params.BackgroundColor)
			checkboxLabel.SetBackgroundColor(msgv.params.BackgroundColor)
			checkboxOpenBracket.SetBackgroundColor(msgv.params.BackgroundColor)
			checkboxCloseBracket.SetBackgroundColor(msgv.params.BackgroundColor)
		}
		checkboxFlex := tview.NewFlex().SetDirection(tview.FlexColumn)
		checkboxFlex.
			AddItem(checkboxOpenBracket, 1, 0, false).
			AddItem(checkbox, 1, 0, true).
			AddItem(checkboxCloseBracket, 1, 0, false).
			AddItem(nil, 1, 0, false).
			AddItem(checkboxLabel, 0, 1, false)
		msgv.checkboxes = append(msgv.checkboxes, checkbox)
		msgv.msgboxFlex.AddItem(
			checkboxFlex,
			1,
			0,
			checkboxIdx == 0 && len(params.InputFields) == 0 && len(params.Buttons) == 0,
		)
		msgv.focusers = append(msgv.focusers, checkbox)
		tabHandler := msgv.getGenericTabHandler(checkbox)
		checkbox.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			switch event.Key() {
			case tcell.KeyEsc:
				if params.OnEsc != nil {
					params.OnEsc()
				}
			}

			return tabHandler(event)
		})
	}

	msgv.buttonsFlex = tview.NewFlex().SetDirection(tview.FlexColumn)
	msgv.msgboxFlex.AddItem(
		msgv.buttonsFlex,
		1,
		1,
		len(params.Buttons) != 0 && len(params.InputFields) == 0,
	)

	// Add a spacer at the left of the buttons, to make them centered
	// (there's also a spacer at the right, added later)
	msgv.buttonsFlex.AddItem(nil, 0, 1, false)

	for i := 0; i < len(params.Buttons); i++ {
		btnLabel := params.Buttons[i]
		btnIdx := i
		btn := tview.NewButton(btnLabel).SetSelectedFunc(func() {
			params.OnButtonPressed(btnLabel, btnIdx)
		})
		msgv.buttons = append(msgv.buttons, btn)
		msgv.focusers = append(msgv.focusers, btn)
		tabHandler := msgv.getGenericTabHandler(btn)
		btn.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			if msgv.scrollTextView(event) {
				return nil
			}

			// Handle Esc key
			switch event.Key() {
			case tcell.KeyEsc:
				if params.OnEsc != nil {
					params.OnEsc()
				}
			}

			event = tabHandler(event)
			if event == nil {
				return nil
			}

			return event
		})

		// Support reverting button label changes when they lose their focus,
		// like we need to do e.g. on "Copy" -> "Copied".
		btn.SetBlurFunc(func() {
			if revert := msgv.onButtonBlurRevert; revert != nil {
				msgv.buttons[revert.index].SetLabel(revert.oldLabel)
				msgv.onButtonBlurRevert = nil
			}
		})

		// Unless it's the first button, add a 1-char spacing.
		if i > 0 {
			msgv.buttonsFlex.AddItem(nil, 1, 0, false)
		}

		// Add the button itself: spacing of 2 chars at each side, and min 10 chars total.
		// Focus the first one.
		buttonSize := len(btnLabel) + 2*2
		if buttonSize < 10 {
			buttonSize = 10
		}
		msgv.buttonsFlex.AddItem(
			btn,
			buttonSize,
			0,
			i == 0 && len(params.InputFields) == 0,
		)
	}

	for i, dropdownParams := range params.ButtonDropdowns {
		var dropdown *ui.DropDown
		dropdown = ui.NewDropDown()
		labels := dropdownParams.Options
		dropdown.SetOptions(labels, func(label string, index int) {
			if index >= 0 && dropdownParams.OnSelected != nil {
				dropdown.SetCurrentOption(-1)
				dropdownParams.OnSelected(index, label)
			}
		})
		dropdown.SetListStyles(menuUnselected, menuSelected)

		fieldWidth := 10
		dropdown.SetFieldWidth(fieldWidth)
		label := dropdownParams.Label
		labelWidth := len([]rune(label))
		leftPadding := (fieldWidth - labelWidth) / 2
		rightPadding := fieldWidth - labelWidth - leftPadding
		dropdown.SetTextOptions(
			" ", " ", " ", " ",
			strings.Repeat(" ", leftPadding)+label+strings.Repeat(" ", rightPadding),
		)

		if i > 0 || len(params.Buttons) > 0 {
			msgv.buttonsFlex.AddItem(nil, 1, 0, false)
		}

		msgv.buttonsFlex.AddItem(dropdown, fieldWidth, 0, false)
		msgv.focusers = append(msgv.focusers, dropdown)

		dropdown.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			list := dropdown.GetList()
			if event.Key() == tcell.KeyRune {
				if dropdown.IsListOpen() {
					switch event.Rune() {
					case 'j', 'l':
						index := list.GetCurrentItem() + 1
						if index >= list.GetItemCount() {
							index = 0
						}
						list.SetCurrentItem(index)
						return nil
					case 'k', 'h':
						index := list.GetCurrentItem() - 1
						if index < 0 {
							index = list.GetItemCount() - 1
						}
						list.SetCurrentItem(index)
						return nil
					case 'g':
						list.SetCurrentItem(0)
						return nil
					case 'G':
						list.SetCurrentItem(list.GetItemCount() - 1)
						return nil
					}
				} else if event.Rune() == 'j' || event.Rune() == 'k' {
					dropdown.OpenList(func(primitive tview.Primitive) {
						params.App.SetFocus(primitive)
					})
					if event.Rune() == 'j' {
						list.SetCurrentItem(0)
					} else {
						list.SetCurrentItem(list.GetItemCount() - 1)
					}
					return nil
				}
			}

			if event.Key() == tcell.KeyEscape && dropdown.IsListOpen() {
				dropdown.SetCurrentOption(-1)
				dropdown.CloseList(func(primitive tview.Primitive) {
					params.App.SetFocus(primitive)
				})
				return nil
			}

			return event
		})

		// Escape closes the message view when the dropdown itself is focused.
		// Escape pressed while its list is open is handled internally by the
		// dropdown and only closes the list.
		dropdown.SetDoneFunc(func(key tcell.Key) {
			switch key {
			case tcell.KeyEscape:
				if params.OnEsc != nil {
					params.OnEsc()
				}
			case tcell.KeyTab, tcell.KeyBacktab:
				for index, focuser := range msgv.focusers {
					if focuser != dropdown {
						continue
					}
					next := index + 1
					if key == tcell.KeyBacktab {
						next = index - 1
					}
					if next < 0 {
						next = len(msgv.focusers) - 1
					} else if next >= len(msgv.focusers) {
						next = 0
					}
					params.App.SetFocus(msgv.focusers[next])
					break
				}
			}
		})
	}

	// Add a spacer at the right of the buttons, to make them centered
	// (there's also a spacer at the left, added before)
	msgv.buttonsFlex.AddItem(nil, 0, 1, false)

	msgv.frame = tview.NewFrame(msgv.msgboxFlex).SetBorders(0, 0, 0, 0, 0, 0)
	msgv.frame.SetBorder(true).SetBorderPadding(1, 1, 1, 1)
	msgv.frame.SetTitle(params.Title)
	if msgv.params.BackgroundColor != tcell.ColorDefault {
		msgv.frame.SetBackgroundColor(msgv.params.BackgroundColor)
	}

	msgv.curWidth = msgv.params.Width
	msgv.curHeight = msgv.params.Height

	return msgv
}

// scrollTextView handles navigation keys while one of the message buttons has
// focus. The text view itself remains non-focusable so it cannot trap focus.
func (msgv *MessageView) scrollTextView(event *tcell.EventKey) bool {
	if !msgv.params.Scrollable {
		return false
	}

	delta := 0
	switch event.Key() {
	case tcell.KeyHome:
		msgv.textView.ScrollToBeginning()
		return true
	case tcell.KeyEnd:
		msgv.textView.ScrollToEnd()
		return true
	case tcell.KeyUp:
		delta = -1
	case tcell.KeyDown:
		delta = 1
	case tcell.KeyPgUp:
		delta = -msgv.textViewPageSize()
	case tcell.KeyPgDn:
		delta = msgv.textViewPageSize()
	case tcell.KeyCtrlU:
		delta = -msgv.textViewPageSize() / 2
	case tcell.KeyCtrlD:
		delta = msgv.textViewPageSize() / 2
	case tcell.KeyRune:
		switch event.Rune() {
		case 'g':
			msgv.textView.ScrollToBeginning()
			return true
		case 'G':
			msgv.textView.ScrollToEnd()
			return true
		case 'k':
			delta = -1
		case 'j':
			delta = 1
		default:
			return false
		}
	default:
		return false
	}

	row, column := msgv.textView.GetScrollOffset()
	msgv.textView.ScrollTo(row+delta, column)
	return true
}

func (msgv *MessageView) textViewPageSize() int {
	_, _, _, height := msgv.textView.GetRect()
	if height < 1 {
		return 1
	}
	return height
}

func (msgv *MessageView) Show() {
	msgv.mainView.showModal(
		pageNameMessage+msgv.params.MessageID, msgv.frame,
		msgv.params.Width,
		msgv.params.Height,
		!msgv.params.NoFocus,
	)
}

func (msgv *MessageView) Hide() {
	msgv.mainView.hideModal(pageNameMessage+msgv.params.MessageID, !msgv.params.NoFocus)
}

// SetText updates the text on the messagebox, and if resizeIfNeeded is true
// and the messagebox is not big enough, then also expands itself.
func (msgv *MessageView) SetText(text string, resizeIfNeeded bool) {
	msgv.textView.SetText(strings.TrimSpace(text))

	if resizeIfNeeded {
		optimalWidth, optimalHeight := msgv.getOptimalSize(text)

		needResize := false
		if msgv.curWidth < optimalWidth {
			msgv.curWidth = optimalWidth
			needResize = true
		}

		if msgv.curHeight < optimalHeight {
			msgv.curHeight = optimalHeight
			needResize = true
		}

		if needResize {
			msgv.mainView.resizeModal(
				pageNameMessage+msgv.params.MessageID,
				msgv.curWidth,
				msgv.curHeight,
			)
		}
	}
}

// GetText returns the current MessageView text.
func (msgv *MessageView) GetText(stripAllTags bool) string {
	return msgv.textView.GetText(stripAllTags)
}

// IsCheckboxChecked returns the current state of a checkbox by parameter index.
func (msgv *MessageView) IsCheckboxChecked(index int) bool {
	return msgv.checkboxes[index].IsChecked()
}

// SetButtonLabelOpts contains extra options for SetButtonLabel.
type SetButtonLabelOpts struct {
	// If RevertOnBlur is true, then once the button loses its focus,
	// its label will be reverted back.
	RevertOnBlur bool
}

// SetButtonLabel updates the label on the button with the given button index.
// No check is done for whether the given index is valid, so if not, it will panic.
func (msgv *MessageView) SetButtonLabel(index int, label string, opts SetButtonLabelOpts) {
	if opts.RevertOnBlur {
		msgv.onButtonBlurRevert = &onButtonBlurRevert{
			index:    index,
			oldLabel: msgv.buttons[index].GetLabel(),
		}
	}

	msgv.buttons[index].SetLabel(label)
}

// getOptimalSize returns optimal width and height for the message box with
// its input fields etc.
func (msgv *MessageView) getOptimalSize(text string) (int, int) {
	// Calculate how much height will be taken by all the input fields.
	inputFieldsHeight := 0
	for i, field := range msgv.params.InputFields {
		// Except for the first field, there is a spacing.
		if i > 0 {
			inputFieldsHeight++
		}

		// One line for the field itself
		inputFieldsHeight++

		// If the label is present, then one more line.
		if field.Label != "" {
			inputFieldsHeight++
		}
	}

	// extraWidth covers padding and border
	extraWidth := 4
	// A scrollable message adds a one-cell scrollbar beside the text view.
	if msgv.params.Scrollable {
		extraWidth++
	}
	// extraHeight covers padding, border, buttons, fields, and checkboxes.
	extraHeight := 6 + inputFieldsHeight + len(msgv.params.Checkboxes)

	optimalWidth, optimalHeight := GetOptimalMessageViewSize(
		msgv.mainView.screenWidth,
		extraWidth,
		extraHeight,
		text,
	)

	// The body can be shorter than the frame title (for example, the
	// "No warnings" state), but the title should not be truncated when the
	// screen is wide enough for it.
	minTitleWidth := len(msgv.params.Title) + extraWidth
	if minTitleWidth > msgv.mainView.screenWidth {
		minTitleWidth = msgv.mainView.screenWidth
	}
	if optimalWidth < minTitleWidth {
		optimalWidth = minTitleWidth
	}

	return optimalWidth, optimalHeight
}

func (msgv *MessageView) getGenericTabHandler(curPrimitive tview.Primitive) func(event *tcell.EventKey) *tcell.EventKey {
	return func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()

		nextIdx := 0
		prevIdx := 0

		for i, p := range msgv.focusers {
			if p != curPrimitive {
				continue
			}

			prevIdx = i - 1
			if prevIdx < 0 {
				prevIdx = len(msgv.focusers) - 1
			}

			nextIdx = i + 1
			if nextIdx >= len(msgv.focusers) {
				nextIdx = 0
			}
		}

		switch key {
		case tcell.KeyTab:
			msgv.params.App.SetFocus(msgv.focusers[nextIdx])
			return nil

		case tcell.KeyBacktab:
			msgv.params.App.SetFocus(msgv.focusers[prevIdx])
			return nil
		}

		return event
	}
}
