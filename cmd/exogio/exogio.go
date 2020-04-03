package main

import (
	"fmt"
	"image"
	"regexp"
	"strings"
	"time"

	"gioui.org/app"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/neutralinsomniac/exocortex/db"

	"gioui.org/font/gofont"
)

func checkErr(err error) {
	if err != nil {
		panic(err)
	}
}

type state struct {
	db.State
	tagList          layout.List
	rowList          layout.List
	refList          layout.List
	todayButton      widget.Button
	tagFilterEditor  widget.Editor
	newRowEditor     widget.Editor
	tagNameEditor    widget.Editor
	editingTagName   bool
	currentUIRows    []uiRow
	currentUIRefRows map[db.Tag][]uiRow
	allTagButtons    []uiTagButton
	filteredTags     []*uiTagButton
}

type uiTagButton struct {
	tag    db.Tag
	button widget.Button
}

type uiRow struct {
	row     db.Row
	content []interface{} // string(s) + uiTagButton(s)
	editor  widget.Editor
	editing bool
}

var programState state

func (p *state) FilterTags() {
	p.filteredTags = make([]*uiTagButton, 0)
	for i, t := range p.allTagButtons {
		if strings.Contains(strings.ToLower(t.tag.Name), strings.ToLower(p.tagFilterEditor.Text())) {
			p.filteredTags = append(p.filteredTags, &p.allTagButtons[i])
		}
	}
}

func (p *state) GoToToday() {
	t := time.Now()
	tag, err := programState.DB.AddTag(t.Format("January 02 2006"))
	checkErr(err)

	p.CurrentDBTag = tag
	p.Refresh()
}

func (p *state) Refresh() error {
	var err error

	fmt.Println("refresh!")
	p.State.Refresh()

	p.tagNameEditor.SetText(p.CurrentDBTag.Name)
	programState.editingTagName = false

	p.allTagButtons = make([]uiTagButton, 0)
	for _, tag := range p.AllDBTags {
		p.allTagButtons = append(p.allTagButtons, uiTagButton{tag: tag})
	}
	p.FilterTags()

	p.CurrentDBRows, err = p.DB.GetRowsForTagID(p.CurrentDBTag.ID)
	checkErr(err)
	p.currentUIRows = make([]uiRow, 0)

	// split the text by tags and pre-calculate the row contents
	re := regexp.MustCompile(`\[\[(.*?)\]\]`)
	for _, row := range p.CurrentDBRows {
		uiRow := uiRow{row: row, editor: widget.Editor{SingleLine: true, Submit: true}}
		uiRow.editor.SetText(uiRow.row.Text)
		for tagIndex := re.FindStringIndex(row.Text); tagIndex != nil; tagIndex = re.FindStringIndex(row.Text) {
			// leading text
			uiRow.content = append(uiRow.content, row.Text[:tagIndex[0]])
			// tag button
			tag, err := p.DB.GetTagByName(row.Text[tagIndex[0]+2 : tagIndex[1]-2])
			checkErr(err)
			uiRow.content = append(uiRow.content, &uiTagButton{tag: tag})
			row.Text = row.Text[tagIndex[1]:]
		}
		uiRow.content = append(uiRow.content, row.Text)
		p.currentUIRows = append(p.currentUIRows, uiRow)
	}

	p.currentUIRefRows = make(map[db.Tag][]uiRow)
	for tag, rows := range p.CurrentDBRefs {
		p.currentUIRefRows[tag] = make([]uiRow, 0)
		for _, row := range rows {
			uiRow := uiRow{row: row, editor: widget.Editor{SingleLine: true, Submit: true}}
			uiRow.editor.SetText(uiRow.row.Text)
			for tagIndex := re.FindStringIndex(row.Text); tagIndex != nil; tagIndex = re.FindStringIndex(row.Text) {
				// leading text
				uiRow.content = append(uiRow.content, row.Text[:tagIndex[0]])
				// tag button
				tag, err := p.DB.GetTagByName(row.Text[tagIndex[0]+2 : tagIndex[1]-2])
				checkErr(err)
				uiRow.content = append(uiRow.content, &uiTagButton{tag: tag})
				row.Text = row.Text[tagIndex[1]:]
			}
			uiRow.content = append(uiRow.content, row.Text)
			p.currentUIRefRows[tag] = append(p.currentUIRefRows[tag], uiRow)
		}
	}

	programState.newRowEditor.Focus()

	return err
}

func main() {
	var exoDB db.ExoDB
	err := exoDB.Open("./exocortex.db")
	checkErr(err)
	defer exoDB.Close()

	err = exoDB.LoadSchema()
	checkErr(err)

	programState.DB = &exoDB
	programState.tagList.Axis = layout.Vertical
	programState.tagList.Alignment = layout.Start
	programState.rowList.Axis = layout.Vertical
	programState.refList.Axis = layout.Vertical
	programState.tagFilterEditor.SingleLine = true
	programState.tagFilterEditor.Submit = true
	programState.tagNameEditor.SingleLine = true
	programState.tagNameEditor.Submit = true
	programState.newRowEditor.SingleLine = true
	programState.newRowEditor.Submit = true

	programState.GoToToday()

	go func() {
		w := app.NewWindow()
		loop(w)
	}()
	app.Main()
}

func loop(w *app.Window) {
	gofont.Register()
	th := material.NewTheme()
	gtx := layout.NewContext(w.Queue())

	for e := range w.Events() {
		if e, ok := e.(system.FrameEvent); ok {
			gtx.Reset(e.Config, e.Size)

			render(gtx, th)

			e.Frame(gtx.Ops)
		}
	}
}

func render(gtx *layout.Context, th *material.Theme) {
	// click on tag header handler
	for _, e := range gtx.Events(&programState.CurrentDBTag) {
		if e, ok := e.(pointer.Event); ok {
			if e.Type == pointer.Release {
				programState.editingTagName = true
				programState.tagNameEditor.Focus()
			}
		}
	}
	// click on ref tag name handler
	for _, t := range programState.SortedRefTagsKeys {
		for _, e := range gtx.Events(t) {
			if e, ok := e.(pointer.Event); ok {
				if e.Type == pointer.Release {
					programState.CurrentDBTag = t
					programState.Refresh()
				}
			}
		}
	}
	// rename tag editor handler
	for _, e := range programState.tagNameEditor.Events(gtx) {
		switch e := e.(type) {
		case widget.SubmitEvent:
			if programState.tagNameEditor.Text() != "" {
				tag, err := programState.DB.RenameTag(programState.CurrentDBTag.Name, e.Text)
				checkErr(err)
				programState.CurrentDBTag = tag
				programState.Refresh()
			}
		case widget.KeyEvent:
			if e.Key.Name == key.NameEscape {
				programState.tagNameEditor.SetText(programState.CurrentDBTag.Name)
				programState.editingTagName = false
			}
		}
	}
	// new tag row editor handler
	for _, e := range programState.newRowEditor.Events(gtx) {
		switch e := e.(type) {
		case widget.SubmitEvent:
			if programState.newRowEditor.Text() != "" {
				_, err := programState.DB.AddRow(programState.CurrentDBTag.ID, e.Text, 0)
				checkErr(err)
				programState.newRowEditor.SetText("")
				programState.Refresh()
			}
		case widget.KeyEvent:
			if e.Key.Name == key.NameEscape {
				programState.newRowEditor.SetText("")
			}
			if e.Key.Name == key.NameTab {
				programState.tagFilterEditor.Focus()
			}
		}
	}
	// today button handler
	for programState.todayButton.Clicked(gtx) {
		programState.GoToToday()
	}
	for _, e := range programState.tagFilterEditor.Events(gtx) {
		switch e := e.(type) {
		case widget.SubmitEvent:
			if e.Text != "" {
				tag, err := programState.DB.AddTag(e.Text)
				checkErr(err)
				programState.tagFilterEditor.SetText("")
				programState.CurrentDBTag = tag
				programState.Refresh()
			}
		case widget.KeyEvent:
			if e.Key.Name == key.NameEscape {
				programState.tagFilterEditor.SetText("")
				programState.FilterTags()
			}
			if e.Key.Name == key.NameTab {
				programState.newRowEditor.Focus()
			}
		case widget.ChangeEvent:
			programState.FilterTags()
		}
	}
	in := layout.UniformInset(unit.Dp(8))
	outerInset := layout.UniformInset(unit.Dp(16))
	var tagsHeaderDims layout.Dimensions
	outerInset.Layout(gtx, func() {
		layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			// all tags pane
			layout.Rigid(func() {
				layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func() {
						layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func() {
								in.Layout(gtx, func() {
									th.H3("Tags").Layout(gtx)
								})
							}),
							layout.Rigid(func() {
								in.Layout(gtx, func() {
									th.Button("Today").Layout(gtx, &programState.todayButton)
								})
							}),
						)
						tagsHeaderDims = gtx.Dimensions
					}),
					layout.Rigid(func() {
						editor := th.Editor("Filter/New Tag")
						editor.TextSize = th.H5("").TextSize
						layout.UniformInset(unit.Dp(16)).Layout(gtx, func() {
							gtx.Constraints.Width.Max = tagsHeaderDims.Size.X
							editor.Layout(gtx, &programState.tagFilterEditor)
						})
					}),
					layout.Rigid(func() {
						in.Layout(gtx, func() {
							in := layout.UniformInset(unit.Dp(4))
							programState.tagList.Layout(gtx, len(programState.filteredTags), func(i int) {
								in.Layout(gtx, func() {
									gtx.Constraints.Width.Min = tagsHeaderDims.Size.X
									programState.filteredTags[i].layout(gtx, th)
								})
							})
						})
					}),
				)
			}),
			// selected tag rows pane
			layout.Flexed(1, func() {
				layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func() {
						layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							// current tag name
							layout.Rigid(func() {
								in.Layout(gtx, func() {
									if programState.editingTagName == false {
										th.H3(programState.CurrentDBTag.Name).Layout(gtx)
										// add edit tag handler
										pointer.Rect(image.Rectangle{Max: gtx.Dimensions.Size}).Add(gtx.Ops)
										pointer.InputOp{Key: &programState.CurrentDBTag}.Add(gtx.Ops)
									} else {
										editor := th.Editor("New tag name")
										editor.TextSize = th.H3("").TextSize
										editor.Layout(gtx, &programState.tagNameEditor)
									}
								})
							}),
							// editor widget for adding a new row
							layout.Rigid(func() {
								layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(16)}.Layout(gtx, func() {
									th.Editor("New row").Layout(gtx, &programState.newRowEditor)
								})
							}),
							// rows for current tag
							layout.Rigid(func() {
								in.Layout(gtx, func() {
									var cachedUIRows = programState.currentUIRows
									programState.rowList.Layout(gtx, len(cachedUIRows), func(i int) {
										layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func() {
											cachedUIRows[i].layout(gtx, th)
										})
									})
								})
							}),
						)
					}),
					// references pane
					layout.Rigid(func() {
						if len(programState.CurrentDBRefs) > 0 {
							// count total refs for rowlist
							refListLen := 0
							for _, refs := range programState.CurrentDBRefs {
								refListLen++            // for the source tag header
								refListLen += len(refs) // for the rows themselves
							}
							layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func() {
									in.Layout(gtx, func() {
										th.H4("References").Layout(gtx)
									})
								}),
								layout.Rigid(func() {
									content := make([]interface{}, 0)
									for _, tag := range programState.SortedRefTagsKeys {
										content = append(content, tag)
										for i, _ := range programState.currentUIRefRows[tag] {
											content = append(content, &programState.currentUIRefRows[tag][i])
										}
									}
									programState.refList.Layout(gtx, len(content), func(i int) {
										in.Layout(gtx, func() {
											switch v := content[i].(type) {
											case db.Tag:
												// source tag for refs
												th.H5(v.Name).Layout(gtx)
												pointer.Rect(image.Rectangle{Max: gtx.Dimensions.Size}).Add(gtx.Ops)
												pointer.InputOp{Key: v}.Add(gtx.Ops)
											case *uiRow:
												// refs themselves
												v.layout(gtx, th)
											}
										})
									})
								}),
							)
						}
					}),
				)
			}),
		)
	})
}

func (r *uiRow) layout(gtx *layout.Context, th *material.Theme) {
	for _, e := range r.editor.Events(gtx) {
		switch e := e.(type) {
		case widget.SubmitEvent:
			if r.editor.Text() != "" {
				err := programState.DB.UpdateRowText(r.row.ID, e.Text)
				checkErr(err)
			} else {
				err := programState.DB.DeleteRowByID(r.row.ID)
				checkErr(err)
			}
			r.editing = false
			programState.DeleteTagIfEmpty(r.row.TagID)
			if programState.CurrentDBTag.ID != r.row.TagID {
				programState.DeleteTagIfEmpty(programState.CurrentDBTag.ID)
			}
			// if current tag is gone, switch
			if _, err := programState.DB.GetTagByID(programState.CurrentDBTag.ID); err != nil {
				programState.GoToToday()
			}
			programState.Refresh()
		case widget.KeyEvent:
			if e.Key.Name == key.NameEscape {
				if r.editor.Text() == "" {
					r.editor.SetText(r.row.Text)
					r.editing = false
					programState.newRowEditor.Focus()
				} else {
					r.editor.SetText("")
				}
			}
		}
	}
	if !r.editing {
		m := new(op.MacroOp)
		m.Record(gtx.Ops)
		flexChildren := []layout.FlexChild{}
		for _, item := range r.content {
			switch v := item.(type) {
			case string:
				flexChildren = append(flexChildren, layout.Rigid(func() {
					th.Body1(v).Layout(gtx)
				}))
			case *uiTagButton:
				flexChildren = append(flexChildren, layout.Rigid(func() {
					v.layout(gtx, th)
				}))

			default:
				panic("unknown type encountered in uiRow.content")
			}
		}
		layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, flexChildren...)
		dims := gtx.Dimensions
		m.Stop()
		// edit row handler
		pointer.Rect(image.Rectangle{Max: dims.Size}).Add(gtx.Ops)
		pointer.InputOp{Key: r}.Add(gtx.Ops)
		// and now draw the labels/buttons on top
		m.Add()
	} else {
		th.Editor("").Layout(gtx, &r.editor)
	}
	for _, e := range gtx.Events(r) {
		if e, ok := e.(pointer.Event); ok {
			if e.Type == pointer.Release {
				r.editing = !r.editing
				r.editor.Focus()
			}
		}
	}
}

func (t *uiTagButton) layout(gtx *layout.Context, th *material.Theme) {
	for t.button.Clicked(gtx) {
		fmt.Println(t, "clicked")
		programState.CurrentDBTag = t.tag
		programState.Refresh()
	}

	button := th.Button(t.tag.Name)
	button.Layout(gtx, &t.button)
}
