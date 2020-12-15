package main

import (
	"fmt"
	"image"
	"regexp"
	"strings"
	"time"

	"gioui.org/app"
	"gioui.org/io/pointer"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"

	//"gioui.org/op/clip"
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
	todayButton      widget.Clickable
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
	button widget.Clickable
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

func loop(w *app.Window) error {
	th := material.NewTheme(gofont.Collection())

	var ops op.Ops
	for {
		select {
		case e := <-w.Events():
			switch e := e.(type) {
			case system.DestroyEvent:
				return e.Err
			case system.FrameEvent:
				gtx := layout.NewContext(&ops, e)
				render(gtx, th)
				e.Frame(gtx.Ops)
			}
		}

	}
}

type (
	C = layout.Context
	D = layout.Dimensions
)

func render(gtx layout.Context, th *material.Theme) {
	// click on tag header handler
	for _, e := range gtx.Events(&programState.CurrentDBTag) {
		if e, ok := e.(pointer.Event); ok {
			if e.Type == pointer.Release {
				unEditAllTheThings()
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
	for _, e := range programState.tagNameEditor.Events() {
		switch e := e.(type) {
		case widget.SubmitEvent:
			if programState.tagNameEditor.Text() != "" {
				tag, err := programState.DB.RenameTag(programState.CurrentDBTag.Name, e.Text)
				checkErr(err)
				programState.CurrentDBTag = tag
				programState.Refresh()
			}
		}
	}
	// new tag row editor handler
	for _, e := range programState.newRowEditor.Events() {
		switch e := e.(type) {
		case widget.SubmitEvent:
			if programState.newRowEditor.Text() != "" {
				_, err := programState.DB.AddRow(programState.CurrentDBTag.ID, e.Text, 0)
				checkErr(err)
				programState.newRowEditor.SetText("")
				programState.Refresh()
			}
		}
	}
	// today button handler
	for programState.todayButton.Clicked() {
		programState.GoToToday()
	}
	for _, e := range programState.tagFilterEditor.Events() {
		switch e := e.(type) {
		case widget.SubmitEvent:
			if e.Text != "" {
				tag, err := programState.DB.AddTag(e.Text)
				checkErr(err)
				programState.tagFilterEditor.SetText("")
				programState.CurrentDBTag = tag
				programState.Refresh()
			}
		case widget.ChangeEvent:
			unEditAllTheThings()
			programState.FilterTags()
		}

	}
	in := layout.UniformInset(unit.Dp(8))
	outerInset := layout.UniformInset(unit.Dp(16))
	outerInset.Layout(gtx, func(gtx C) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			// all tags pane
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D {
								return in.Layout(gtx, func(gtx C) D {
									return material.H3(th, "Tags").Layout(gtx)
								})
							}),
							layout.Rigid(func(gtx C) D {
								return in.Layout(gtx, func(gtx C) D {
									return material.Button(th, &programState.todayButton, "Today").Layout(gtx)
								})
							}),
						)
					}),
					layout.Rigid(func(gtx C) D {
						editor := material.Editor(th, &programState.tagFilterEditor, "Filter/New Tag")
						editor.TextSize = material.H5(th, "").TextSize
						return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx C) D {
							return editor.Layout(gtx)
						})
					}),
					layout.Rigid(func(gtx C) D {
						return in.Layout(gtx, func(gtx C) D {
							in := layout.UniformInset(unit.Dp(4))
							return programState.tagList.Layout(gtx, len(programState.filteredTags), func(gtx C, i int) D {
								return in.Layout(gtx, func(gtx C) D {
									return programState.filteredTags[i].layout(gtx, th)
								})
							})
						})
					}),
				)
			}),
			// selected tag rows pane
			layout.Flexed(1, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							// current tag name
							layout.Rigid(func(gtx C) D {
								return in.Layout(gtx, func(gtx C) D {
									if programState.editingTagName == false {
										// add edit tag handler
										dims := material.H3(th, programState.CurrentDBTag.Name).Layout(gtx)
										pointer.Rect(image.Rectangle{Max: dims.Size}).Add(gtx.Ops)
										pointer.InputOp{Tag: &programState.CurrentDBTag, Types: pointer.Release}.Add(gtx.Ops)
										return dims
									} else {
										editor := material.Editor(th, &programState.tagNameEditor, "New tag name")
										editor.TextSize = material.H3(th, "").TextSize
										return editor.Layout(gtx)
									}
								})
							}),
							// editor widget for adding a new row
							layout.Rigid(func(gtx C) D {
								return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(16)}.Layout(gtx, func(gtx C) D {
									return material.Editor(th, &programState.newRowEditor, "New row").Layout(gtx)
								})
							}),
							// rows for current tag
							layout.Rigid(func(gtx C) D {
								return in.Layout(gtx, func(gtx C) D {
									var cachedUIRows = programState.currentUIRows
									return programState.rowList.Layout(gtx, len(cachedUIRows), func(gtx C, i int) D {
										return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx C) D {
											return cachedUIRows[i].layout(gtx, th)
										})
									})
								})
							}),
						)
					}),
					// references pane
					layout.Rigid(func(gtx C) D {
						if len(programState.CurrentDBRefs) > 0 {
							// count total refs for rowlist
							refListLen := 0
							for _, refs := range programState.CurrentDBRefs {
								refListLen++            // for the source tag header
								refListLen += len(refs) // for the rows themselves
							}
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx C) D {
									return in.Layout(gtx, func(gtx C) D {
										return material.H4(th, "References").Layout(gtx)
									})
								}),
								layout.Rigid(func(gtx C) D {
									content := make([]interface{}, 0)
									for _, tag := range programState.SortedRefTagsKeys {
										content = append(content, tag)
										for i, _ := range programState.currentUIRefRows[tag] {
											content = append(content, &programState.currentUIRefRows[tag][i])
										}
									}
									return programState.refList.Layout(gtx, len(content), func(gtx C, i int) D {
										return in.Layout(gtx, func(gtx C) D {
											switch v := content[i].(type) {
											case db.Tag:
												// source tag for refs
												dims := material.H5(th, v.Name).Layout(gtx)

												pointer.Rect(image.Rectangle{Max: dims.Size}).Add(gtx.Ops)
												pointer.InputOp{Tag: v, Types: pointer.Release}.Add(gtx.Ops)
												return dims
											case *uiRow:
												// refs themselves
												dims := v.layout(gtx, th)
												pointer.Rect(image.Rectangle{Max: dims.Size}).Add(gtx.Ops)
												pointer.InputOp{Tag: v, Types: pointer.Release}.Add(gtx.Ops)
												return v.layout(gtx, th)
											}
											return layout.Dimensions{}
										})
									})
								}),
							)
						}
						return layout.Dimensions{}
					}),
				)
			}),
		)
	})
}

func unEditAllTheThings() {
	programState.editingTagName = false
	for i, row := range programState.currentUIRows {
		row.editing = false
		programState.currentUIRows[i] = row
	}
	for tag, rows := range programState.currentUIRefRows {
		for i, row := range rows {
			row.editing = false
			rows[i] = row
		}
		programState.currentUIRefRows[tag] = rows
	}
}

func (r *uiRow) layout(gtx layout.Context, th *material.Theme) D {
	// TODO FIX THIS (both the tag button events and this event are getting triggered, but this event is winning)
	for _, e := range gtx.Events(r) {
		if e, ok := e.(pointer.Event); ok {
			if e.Type == pointer.Release {
				unEditAllTheThings()
				if !r.editing {
					r.editing = true
					r.editor.Focus()
				}
			}
		}
	}
	for _, e := range r.editor.Events() {
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
		}
	}
	if !r.editing {
		m := op.Record(gtx.Ops)
		flexChildren := []layout.FlexChild{}
		for _, item := range r.content {
			switch v := item.(type) {
			case string:
				flexChildren = append(flexChildren, layout.Rigid(func(gtx C) D {
					return material.Body1(th, v).Layout(gtx)
				}))
			case *uiTagButton:
				flexChildren = append(flexChildren, layout.Rigid(func(gtx C) D {
					return v.layout(gtx, th)
				}))

			default:
				panic("unknown type encountered in uiRow.content")
			}
		}
		// edit row handler

		dims := layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, flexChildren...)
		callOp := m.Stop()

		pointer.Rect(image.Rectangle{Max: dims.Size}).Add(gtx.Ops)
		pointer.InputOp{Tag: r, Types: pointer.Release}.Add(gtx.Ops)

		callOp.Add(gtx.Ops)
		return dims
	} else {
		return material.Editor(th, &r.editor, "").Layout(gtx)
	}
}

func (t *uiTagButton) layout(gtx layout.Context, th *material.Theme) D {
	for t.button.Clicked() {
		programState.CurrentDBTag = t.tag
		programState.Refresh()
	}

	button := material.Button(th, &t.button, t.tag.Name)
	return button.Layout(gtx)
}
