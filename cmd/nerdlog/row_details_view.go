package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dimonomid/nerdlog/cmd/nerdlog/ui"
	"github.com/dimonomid/nerdlog/core"
	"github.com/gdamore/tcell/v2"
	"github.com/juju/errors"
	"github.com/rivo/tview"
)

const (
	rdvColIdxN     = 0
	rdvColIdxName  = 1
	rdvColIdxValue = 2
)

const (
	rdvEnableHeader = false
)

type RowDetailsViewParams struct {
	Data QueryFull

	ExistingNamesSet map[string]struct{}

	// Msg can be nil; in this case we'll just show the list of columns, without
	// values.
	Msg *core.LogMsg

	// DoneFunc is called when the user submits the form. If it returns a non-nil
	// error, the form will show that error and will not be submitted.
	DoneFunc func(data QueryFull, dqp doQueryParams) error
}

type RowDetailsView struct {
	params   RowDetailsViewParams
	mainView *MainView

	allNamesSet map[string]struct{}

	queryFull QueryFull

	sq *SelectQueryParsed

	// msg can be nil; in this case we'll just show the list of columns, without
	// values.
	msg *core.LogMsg

	flex *tview.Flex

	tbl *ui.TableWithDropdown

	okBtn       *tview.Button
	cancelBtn   *tview.Button
	showOrigBtn *tview.Button
	frame       *tview.Frame

	affinity map[string]*rowDetailsFieldAffinity
}

func NewRowDetailsView(
	mainView *MainView, params *RowDetailsViewParams,
) *RowDetailsView {
	rdv := &RowDetailsView{
		params:      *params,
		mainView:    mainView,
		allNamesSet: map[string]struct{}{},
		affinity:    map[string]*rowDetailsFieldAffinity{},
	}

	var focusers []tview.Primitive
	getGenericTabHandler := func(curPrimitive tview.Primitive) func(event *tcell.EventKey) *tcell.EventKey {
		return func(event *tcell.EventKey) *tcell.EventKey {
			key := event.Key()

			nextIdx := 0
			prevIdx := 0

			for i, p := range focusers {
				if p != curPrimitive {
					continue
				}

				prevIdx = i - 1
				if prevIdx < 0 {
					prevIdx = len(focusers) - 1
				}

				nextIdx = i + 1
				if nextIdx >= len(focusers) {
					nextIdx = 0
				}
			}

			switch key {
			case tcell.KeyTab:
				rdv.mainView.params.App.SetFocus(focusers[nextIdx])
				return nil

			case tcell.KeyBacktab:
				rdv.mainView.params.App.SetFocus(focusers[prevIdx])
				return nil
			}

			return event
		}
	}

	rdv.flex = tview.NewFlex().SetDirection(tview.FlexRow)

	rdv.tbl = ui.NewTableWithDropdown()
	if rdvEnableHeader {
		rdv.tbl.SetFixed(1, 0)
	}
	rdv.tbl.SetFocusFunc(func() {
		rdv.tbl.SetSelectable(true, false)
	})
	rdv.tbl.SetBlurFunc(func() {
		rdv.tbl.SetSelectable(false, false)
	})
	rdv.tbl.SetListStyles(menuUnselected, menuSelected)

	if rdvEnableHeader {
		rdv.tbl.SetCell(0, rdvColIdxN, newTableCellHeader("N"))
		rdv.tbl.SetCell(0, rdvColIdxName, newTableCellHeader("name"))
		rdv.tbl.SetCell(0, rdvColIdxValue, newTableCellHeader("value"))
	}

	rdv.tbl.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if rdv.tbl.IsDropdownOpen() {
			//list := rdv.tbl.GetDropdownList()
			switch key := event.Key(); key {
			case tcell.KeyEscape:
				rdv.tbl.CloseDropdownList(rdv.mainView.setFocus)
				return nil

			case tcell.KeyRune:
				switch event.Rune() {
				case 'k':
					return tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
				case 'j':
					return tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
				}
				return nil
			}

			return event
		}

		moveCursorAt := func(name string) {
			numRows := rdv.tbl.GetRowCount()
			start := 0
			if rdvEnableHeader {
				start = 1
			}
			for i := start; i < numRows; i++ {
				rCtx := rdv.tbl.GetCell(i, 0).GetReference().(rowDetailsViewCellCtx)
				if rCtx.field.Name == name {
					rdv.tbl.Select(i, 0)
					break
				}
			}
		}

		updateAffinityItemRemoved := func(idx int) {
			for _, af := range rdv.affinity {
				if af.fieldIdx > idx {
					af.fieldIdx--
				}
			}
		}

		updateAffinityItemAdded := func(idx int) {
			for _, af := range rdv.affinity {
				if af.fieldIdx >= idx {
					af.fieldIdx++
				}
			}
		}

		updateAffinityItemMovedUp := func(oldIdx int) {
			for _, af := range rdv.affinity {
				if af.fieldIdx == oldIdx-1 {
					af.fieldIdx++
				}
			}
		}

		updateAffinityItemMovedDown := func(oldIdx int) {
			for _, af := range rdv.affinity {
				if af.fieldIdx == oldIdx+1 {
					af.fieldIdx--
				}
			}
		}

		doMoveUp := func(idx int) {
			rdv.sq.Fields[idx-1], rdv.sq.Fields[idx] = rdv.sq.Fields[idx], rdv.sq.Fields[idx-1]
		}

		doMoveDown := func(idx int) {
			rdv.sq.Fields[idx+1], rdv.sq.Fields[idx] = rdv.sq.Fields[idx], rdv.sq.Fields[idx+1]
		}

		showFullValue := func() {
			nRow, _ := rdv.tbl.GetSelection()
			rCtx := rdv.tbl.GetCell(nRow, 0).GetReference().(rowDetailsViewCellCtx)

			mtv := NewMyTextView(rdv.mainView, &MyTextViewParams{
				Title: rCtx.field.Name,
				Text:  rCtx.val,
			})
			mtv.Show()
		}

		toggle := func() {
			// Toggle the field in the select query
			nRow, _ := rdv.tbl.GetSelection()
			rCtx := rdv.tbl.GetCell(nRow, 0).GetReference().(rowDetailsViewCellCtx)
			if rCtx.fieldIdx < 0 {
				// Add this field to the select query
				rdv.sq.Fields = append(rdv.sq.Fields, SelectQueryField{
					Name:        rCtx.field.Name,
					DisplayName: rCtx.field.DisplayName,
				})

				// If affinity is present, fill out the details and move the item to
				// its place
				if af, ok := rdv.affinity[rCtx.field.Name]; ok {
					rdv.sq.Fields[len(rdv.sq.Fields)-1] = af.field

					for n := len(rdv.sq.Fields) - 1; n > af.fieldIdx; n-- {
						doMoveUp(n)
					}

					updateAffinityItemAdded(af.fieldIdx)
				}

				delete(rdv.affinity, rCtx.field.Name)
			} else {
				// Remove this field from the select query

				rdv.affinity[rCtx.field.Name] = &rowDetailsFieldAffinity{
					field:    rCtx.field,
					fieldIdx: rCtx.fieldIdx,
				}

				rdv.sq.Fields = append(rdv.sq.Fields[:rCtx.fieldIdx], rdv.sq.Fields[rCtx.fieldIdx+1:]...)

				updateAffinityItemRemoved(rCtx.fieldIdx)
			}

			rdv.updateUI()

			// Move cursor to the same item where it was
			moveCursorAt(rCtx.field.Name)
		}

		moveUp := func() {
			nRow, _ := rdv.tbl.GetSelection()
			rCtx := rdv.tbl.GetCell(nRow, 0).GetReference().(rowDetailsViewCellCtx)

			if rCtx.fieldIdx == -1 {
				// This field is not present in the Fields slice, so just add it.
				toggle()
				return
			} else if rCtx.fieldIdx == 0 {
				// Can't move any higher
				return
			}

			// If the field above is sticky, we need to make the current item sticky
			// as well, since it's gonna be before now.
			if rdv.sq.Fields[rCtx.fieldIdx-1].Sticky {
				rdv.sq.Fields[rCtx.fieldIdx].Sticky = true
			}

			// Swap fields
			doMoveUp(rCtx.fieldIdx)
			updateAffinityItemMovedUp(rCtx.fieldIdx)

			rdv.updateUI()

			// Move cursor up
			moveCursorAt(rCtx.field.Name)
		}

		moveDown := func() {
			nRow, _ := rdv.tbl.GetSelection()
			rCtx := rdv.tbl.GetCell(nRow, 0).GetReference().(rowDetailsViewCellCtx)

			if rCtx.fieldIdx == -1 {
				// This field is not present in the Fields slice
				return
			} else if rCtx.fieldIdx == len(rdv.sq.Fields)-1 {
				// Can't move any lower, so just toggle it.
				toggle()
				return
			}

			// If the field below is non-sticky, we need to make the current item
			// non-sticky as well.
			if !rdv.sq.Fields[rCtx.fieldIdx+1].Sticky {
				rdv.sq.Fields[rCtx.fieldIdx].Sticky = false
			}

			// Swap fields
			doMoveDown(rCtx.fieldIdx)
			updateAffinityItemMovedDown(rCtx.fieldIdx)

			rdv.updateUI()

			// Move cursor down
			moveCursorAt(rCtx.field.Name)
		}

		editColumn := func() {
			nRow, _ := rdv.tbl.GetSelection()
			rCtx := rdv.tbl.GetCell(nRow, 0).GetReference().(rowDetailsViewCellCtx)

			field := rdv.sq.Fields[rCtx.fieldIdx]

			cev := NewColumnEditView(rdv.mainView, &ColumnEditViewParams{
				DoneFunc: func(field SelectQueryField) error {
					if field.Name != rCtx.field.Name {
						// Name has changed

						if field.Name == "" {
							return errors.Errorf("column name can't be empty")
						}

						// Check if the new name clashes with any of the existing columns
						for i, oldField := range rdv.sq.Fields {
							if i == rCtx.fieldIdx {
								continue
							}

							if oldField.Name == field.Name {
								return errors.Errorf("there is already a column with name %q", field.Name)
							}
						}

						rdv.allNamesSet[field.Name] = struct{}{}
					}

					rdv.sq.Fields[rCtx.fieldIdx] = field

					rdv.updateUI()
					// Move cursor to the same item where it was
					moveCursorAt(field.Name)

					return nil
				},
			})
			cev.Show(field)
		}

		addNewColumn := func() {
			nRow, _ := rdv.tbl.GetSelection()
			rCtx := rdv.tbl.GetCell(nRow, 0).GetReference().(rowDetailsViewCellCtx)

			cev := NewColumnEditView(rdv.mainView, &ColumnEditViewParams{
				DoneFunc: func(newField SelectQueryField) error {
					if newField.Name == "" {
						return errors.Errorf("column name can't be empty")
					}

					// Check if the new name clashes with any of the existing columns
					for _, oldField := range rdv.sq.Fields {
						if oldField.Name == newField.Name {
							return errors.Errorf("there is already a column with name %q", newField.Name)
						}
					}

					rdv.allNamesSet[newField.Name] = struct{}{}

					newFields := make([]SelectQueryField, 0, len(rdv.sq.Fields)+1)
					newFields = append(newFields, rdv.sq.Fields[:rCtx.fieldIdx]...)
					newFields = append(newFields, newField)
					newFields = append(newFields, rdv.sq.Fields[rCtx.fieldIdx:]...)

					rdv.sq.Fields = newFields

					rdv.updateUI()
					// Move cursor to the new item.
					moveCursorAt(newField.Name)

					return nil
				},
			})
			cev.Show(SelectQueryField{})
		}

		toggleIncludeAll := func() {
			rdv.sq.IncludeAll = !rdv.sq.IncludeAll
			rdv.updateUI()
		}

		getToggleFilterByValue := func(awkExpr string) func() {
			return func() {
				rdv.queryFull.Query = addToOrRemoveFromAwkQuery(rdv.queryFull.Query, awkExpr)
				rdv.updateUI()
			}
		}

	ks:
		switch event.Key() {
		case tcell.KeyEnter:
			nRow, _ := rdv.tbl.GetSelection()
			rCtx := rdv.tbl.GetCell(nRow, 0).GetReference().(rowDetailsViewCellCtx)

			rdv.tbl.ClearOptions()

			rdv.tbl.AddOption("    Show full value", showFullValue)

			if rCtx.fieldIdx < 0 {
				if !rdv.sq.IncludeAll {
					rdv.tbl.AddOption("    Add this column                     [black]<Space>[-]", toggle)
				} else {
					rdv.tbl.AddOption("    Make this column explicit           [black]<Space>[-]", toggle)
				}
			} else {
				if !rdv.sq.IncludeAll {
					rdv.tbl.AddOption("    Remove column                       [black]<Space>[-]", toggle)
				} else {
					rdv.tbl.AddOption("    Make this column implicit           [black]<Space>[-]", toggle)
				}
			}

			if rCtx.fieldIdx != -1 {
				rdv.tbl.AddOption("    Edit column", editColumn)
			}

			if rCtx.fieldIdx != -1 {
				rdv.tbl.AddOption("    Add new column here", addNewColumn)
			}

			if rCtx.fieldIdx > 0 {
				rdv.tbl.AddOption("    Move up                 [black]<Ctrl-Up>, <Ctrl-K>[-]", moveUp)
			}
			if rCtx.fieldIdx < len(rdv.sq.Fields)-1 && rCtx.fieldIdx != -1 {
				rdv.tbl.AddOption("    Move down               [black]<Ctrl-Dn>, <Ctrl-J>[-]", moveDown)
			}

			if rCtx.fieldIdx < 0 {
				if !rdv.sq.IncludeAll {
					rdv.tbl.AddOption("[ ] Show all implicit columns", toggleIncludeAll)
				} else {
					rdv.tbl.AddOption("[✔] Show all implicit columns", toggleIncludeAll)
				}
			}

			if rCtx.valExists && rCtx.field.Name != FieldNameTime && rCtx.field.Name != "lstream" {
				if !rCtx.filteredByValue {
					rdv.tbl.AddOption("[ ] Filter logs containing value", getToggleFilterByValue(rCtx.awkValue))
				} else {
					rdv.tbl.AddOption("[✔] Filter logs containing value", getToggleFilterByValue(rCtx.awkValue))
				}
			}

			row, _ := rdv.tbl.GetSelection()
			rdv.tbl.OpenDropdownList(row, rdvColIdxName, rdv.mainView.setFocus)
			return nil

		case tcell.KeyCtrlK:
			moveUp()
			return nil

		case tcell.KeyUp:
			if event.Modifiers()&tcell.ModCtrl != 0 {
				moveUp()
				return nil
			}
			// Let the event be handled as usual

		case tcell.KeyDown:
			if event.Modifiers()&tcell.ModCtrl != 0 {
				moveDown()
				return nil
			}
			// Let the event be handled as usual

		case tcell.KeyCtrlJ:
			moveDown()
			return nil

		case tcell.KeyCtrlD:
			// TODO: ideally we'd want to only go half a page down, but for now just
			// return Ctrl+F which will go the full page down
			return tcell.NewEventKey(tcell.KeyCtrlF, 0, tcell.ModNone)
		case tcell.KeyCtrlU:
			// TODO: ideally we'd want to only go half a page up, but for now just
			// return Ctrl+B which will go the full page up
			return tcell.NewEventKey(tcell.KeyCtrlB, 0, tcell.ModNone)

		case tcell.KeyRune:
			switch event.Rune() {
			case ' ':
				toggle()

				return nil
			default:
				break ks
			}

			return nil
		}

		event = rdv.genericInputHandler(event, getGenericTabHandler(rdv.tbl), nil, nil)
		if event == nil {
			return nil
		}

		return event
	})
	rdv.flex.AddItem(rdv.tbl, 0, 1, true)
	focusers = append(focusers, rdv.tbl)

	rdv.flex.AddItem(nil, 1, 0, false)

	rdv.okBtn = tview.NewButton("OK")
	rdv.okBtn.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEnter:
			if err := rdv.applyQuery(); err != nil {
				rdv.mainView.showMessagebox("err", "Error", err.Error(), nil)
			}
			return nil
		}

		event = rdv.genericInputHandler(event, getGenericTabHandler(rdv.okBtn), nil, nil)
		if event == nil {
			return nil
		}

		return event
	})
	focusers = append(focusers, rdv.okBtn)

	rdv.cancelBtn = tview.NewButton("Cancel")
	rdv.cancelBtn.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEnter:
			rdv.Hide()
			return nil
		}

		event = rdv.genericInputHandler(event, getGenericTabHandler(rdv.cancelBtn), nil, nil)
		if event == nil {
			return nil
		}

		return event
	})
	focusers = append(focusers, rdv.cancelBtn)

	if params.Msg != nil {
		rdv.showOrigBtn = tview.NewButton("Show original")
		rdv.showOrigBtn.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			switch event.Key() {
			case tcell.KeyEnter:
				rdv.mainView.showOriginalMsg(*params.Msg)
				return nil
			}

			event = rdv.genericInputHandler(event, getGenericTabHandler(rdv.showOrigBtn), nil, nil)
			if event == nil {
				return nil
			}

			return event
		})
		focusers = append(focusers, rdv.showOrigBtn)
	}

	bottomFlex := tview.NewFlex().SetDirection(tview.FlexColumn)
	bottomFlex.
		AddItem(rdv.okBtn, 10, 0, false).
		AddItem(nil, 1, 0, false).
		AddItem(rdv.cancelBtn, 10, 0, false).
		AddItem(nil, 1, 0, false)

	if rdv.showOrigBtn != nil {
		bottomFlex.
			AddItem(rdv.showOrigBtn, 15, 0, false).
			AddItem(nil, 0, 1, false)
	}
	rdv.flex.AddItem(bottomFlex, 1, 0, false)

	rdv.frame = tview.NewFrame(rdv.flex).SetBorders(0, 0, 0, 0, 0, 0)
	rdv.frame.SetBorder(true).SetBorderPadding(0, 0, 1, 1)
	rdv.frame.SetTitle("Row details")

	rdv.setData(params.Data, params.ExistingNamesSet, params.Msg)

	return rdv
}

func (rdv *RowDetailsView) Show() {
	rdv.mainView.showModal(
		pageNameRowDetails, rdv.frame,
		121,
		25,
		true,
	)
}

func (rdv *RowDetailsView) setData(
	data QueryFull, existingNamesSet map[string]struct{}, msg *core.LogMsg,
) {
	sq, err := ParseSelectQuery(data.SelectQuery)
	if err != nil {
		panic("SelectQuery must be valid, but got: " + err.Error())
	}

	rdv.queryFull = data
	rdv.sq = sq
	rdv.msg = msg

	rdv.allNamesSet = map[string]struct{}{
		FieldNameTime:    {},
		FieldNameMessage: {},
	}
	for _, field := range rdv.sq.Fields {
		rdv.allNamesSet[field.Name] = struct{}{}
	}
	for key := range existingNamesSet {
		rdv.allNamesSet[key] = struct{}{}
	}

	rdv.updateUI()
}

func (rdv *RowDetailsView) updateUI() {
	type nameWIdx struct {
		idx   int
		field SelectQueryField
	}

	// Move sticky ones to the front
	sort.SliceStable(rdv.sq.Fields, func(i, j int) bool {
		vi := 1
		if rdv.sq.Fields[i].Sticky {
			vi = 0
		}

		vj := 1
		if rdv.sq.Fields[j].Sticky {
			vj = 0
		}

		return vi < vj
	})

	names := make([]nameWIdx, 0, len(rdv.sq.Fields)+len(rdv.allNamesSet)+len(FieldNamesSpecial))
	for i, field := range rdv.sq.Fields {
		names = append(names, nameWIdx{idx: i, field: field})
	}

	isNameExplicit := func(name string) bool {
		for _, n := range names {
			if n.field.Name == name {
				return true
			}
		}

		return false
	}

	extraNames := make([]nameWIdx, 0, len(rdv.allNamesSet))
	for n := range rdv.allNamesSet {
		if isNameExplicit(n) {
			continue
		}

		extraNames = append(extraNames, nameWIdx{
			idx: -1,
			field: SelectQueryField{
				Name:        n,
				DisplayName: n,
			},
		})
	}
	sort.Slice(extraNames, func(i, j int) bool {
		return extraNames[i].field.Name < extraNames[j].field.Name
	})

	names = append(names, extraNames...)

	for i, name := range names {
		var nCell *tview.TableCell
		if name.idx >= 0 {
			txt := fmt.Sprintf("%d", name.idx+1)
			if name.field.Sticky {
				txt += "[green::b]s[-::-]"
			}
			nCell = newTableCellLogmsg(txt)
		} else {
			if rdv.sq.IncludeAll {
				nCell = newTableCellLogmsg("…")
				nCell.SetTextColor(tcell.ColorLightGray)
			} else {
				nCell = newTableCellLogmsg("x")
				nCell.SetTextColor(tcell.ColorRed)
			}
		}

		var val string
		valExists := false

		var valueCell *tview.TableCell

		if rdv.msg != nil {
			switch name.field.Name {
			case FieldNameTime:
				if !rdv.msg.OrigDecreasedTime.IsZero() {
					val = fmt.Sprintf(
						"%s (DECREASED FROM: %s)",
						rdv.msg.OrigDecreasedTime.String(),
						rdv.msg.Time.String(),
					)
				} else {
					val = rdv.msg.Time.String()
				}
				valExists = true
			case FieldNameMessage:
				val = rdv.msg.Msg
				valExists = true
			default:
				val, valExists = rdv.msg.Context[name.field.Name]
			}
		}

		awkName := name.field.Name
		if awkName == "message" {
			// In JSON logs, message field is named "msg"
			awkName = "msg"
		}

		awkValue := fmt.Sprintf(`/%s/`, awkEscape(val))
		filteredByValue := strings.Contains(rdv.queryFull.Query, awkValue)

		nRow := i
		if rdvEnableHeader {
			nRow++
		}

		rdv.tbl.SetCell(nRow, rdvColIdxN, nCell)

		nameStr := name.field.Name
		if name.field.DisplayName != name.field.Name {
			nameStr += fmt.Sprintf(" [lightgray::i](%s)[-::-]", name.field.DisplayName)
		}

		nameCell := newTableCellLogmsg(nameStr).SetAttributes(tcell.AttrBold)
		if name.idx >= 0 {
			nameCell.SetTextColor(tcell.ColorLightBlue)
		} else {
			nameCell.SetTextColor(tcell.ColorLightGray)
		}
		rdv.tbl.SetCell(nRow, rdvColIdxName, nameCell)

		valStr := tview.Escape(val)
		if filteredByValue {
			valStr = "🔍 " + valStr
		}

		valueCell = newTableCellLogmsg(valStr)
		if name.field.Name == FieldNameTime && rdv.msg != nil && !rdv.msg.OrigDecreasedTime.IsZero() {
			valueCell.SetTextColor(tcell.ColorRed)
		}
		rdv.tbl.SetCell(nRow, rdvColIdxValue, valueCell)

		rdv.tbl.GetCell(nRow, 0).SetReference(rowDetailsViewCellCtx{
			field:     name.field,
			fieldIdx:  name.idx,
			val:       val,
			valExists: valExists,

			awkValue:        awkValue,
			filteredByValue: filteredByValue,
		})
	}
}

type rowDetailsViewCellCtx struct {
	field SelectQueryField

	// fieldIdx is the index in the SelectQueryParsed.Fields slice. If the field
	// is not present in this slice, then -1.
	fieldIdx int

	// val is the field value
	val string
	// valExists is true if value actual exists in the log row
	valExists bool

	awkValue        string
	filteredByValue bool
}

type rowDetailsFieldAffinity struct {
	field SelectQueryField

	// fieldIdx is the index in the SelectQueryParsed.Fields slice. If the field
	// is not present in this slice, then -1.
	fieldIdx int
}

func (rdv *RowDetailsView) Hide() {
	rdv.mainView.hideModal(pageNameRowDetails, true)
}

func (rdv *RowDetailsView) GetQueryFull() QueryFull {
	return QueryFull{
		Time:     rdv.queryFull.Time,
		Query:    rdv.queryFull.Query,
		LStreams: rdv.queryFull.LStreams,

		SelectQuery: rdv.sq.Marshal(),
	}
}

func (rdv *RowDetailsView) genericInputHandler(
	event *tcell.EventKey,
	genericTabHandler func(event *tcell.EventKey) *tcell.EventKey,
	getQFPart func(qf QueryFull) string,
	setQFPart func(qf *QueryFull, part string),
) *tcell.EventKey {
	event = genericTabHandler(event)
	if event == nil {
		return nil
	}

	switch event.Key() {
	case tcell.KeyEsc:
		rdv.Hide()
		return nil
	}

	return event
}

func (rdv *RowDetailsView) applyQuery() error {
	err := rdv.params.DoneFunc(rdv.GetQueryFull(), doQueryParams{})
	if err != nil {
		return errors.Trace(err)
	}

	rdv.Hide()

	return nil
}

func awkEscape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, ".", "\\.")
	s = strings.ReplaceAll(s, "(", "\\(")
	s = strings.ReplaceAll(s, ")", "\\)")
	s = strings.ReplaceAll(s, "{", "\\{")
	s = strings.ReplaceAll(s, "}", "\\}")
	s = strings.ReplaceAll(s, "[", "\\[")
	s = strings.ReplaceAll(s, "]", "\\]")
	s = strings.ReplaceAll(s, "<", "\\<")
	s = strings.ReplaceAll(s, ">", "\\>")
	s = strings.ReplaceAll(s, ":", "\\:")
	// TODO: it's not complete, e.g. quotes aren't handled properly, but
	// we can polish it after we switch to structured logs everywhere.
	return s
}

func addToOrRemoveFromAwkQuery(query string, part string) string {
	if !strings.Contains(query, part) {
		// Need to add
		if query == "" {
			query = part
		} else {
			// TODO: it won't work if we have some ||, to handle it correctly we'd
			// have to parse the awk expression, but for now I'm just ignoring the
			// issue.
			query += " && " + part
		}
	} else {
		// Need to remove
		if strings.Contains(query, part+" && ") {
			query = strings.ReplaceAll(query, part+" && ", "")
		} else if strings.Contains(query, " && "+part) {
			query = strings.ReplaceAll(query, " && "+part, "")
		} else {
			query = strings.ReplaceAll(query, part, "")
		}
	}

	return query
}
