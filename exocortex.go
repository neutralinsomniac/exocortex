package main

import (
	"fmt"
	"regexp"
	"sort"
	"time"

	"gioui.org/app"
	"gioui.org/io/system"
	"gioui.org/layout"
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
	db                *db.ExoDB
	tagList           layout.List
	rowList           layout.List
	refList           layout.List
	todayButton       widget.Button
	newRowEditor      widget.Editor
	currentDBTag      db.Tag
	currentDBRows     []db.Row
	currentDBRefs     db.Refs
	currentUIRows     []uiRow
	currentUIRefRows  map[db.Tag][]uiRow
	sortedRefTagsKeys []db.Tag
	allTags           []uiTagButton
}

type uiTagButton struct {
	tag    db.Tag
	button widget.Button
}

type uiRow struct {
	row        db.Row
	content    []interface{} // string(s) + uiTagButton(s)
	editor     widget.Editor
	editButton widget.Button
	editing    bool
}

var programState state

func switchTag(tag db.Tag) {
	programState.currentDBTag = tag
	programState.Refresh()
}

func (p *state) Refresh() error {
	var err error

	fmt.Println("refresh!")
	allTags, err := p.db.GetAllTags()
	checkErr(err)

	p.allTags = make([]uiTagButton, 0)
	for _, tag := range allTags {
		p.allTags = append(p.allTags, uiTagButton{tag: tag})
	}
	p.currentDBRows, err = p.db.GetRowsForTagID(p.currentDBTag.ID)
	checkErr(err)
	p.currentUIRows = make([]uiRow, 0)

	// split the text by tags and pre-calculate the row contents
	re := regexp.MustCompile(`\[\[(.*?)\]\]`)
	for _, row := range p.currentDBRows {
		uiRow := uiRow{row: row, editor: widget.Editor{SingleLine: true, Submit: true}}
		uiRow.editor.SetText(uiRow.row.Text)
		for tagIndex := re.FindStringIndex(row.Text); tagIndex != nil; tagIndex = re.FindStringIndex(row.Text) {
			// leading text
			uiRow.content = append(uiRow.content, row.Text[:tagIndex[0]])
			// tag button
			tag, err := p.db.GetTagByName(row.Text[tagIndex[0]+2 : tagIndex[1]-2])
			checkErr(err)
			uiRow.content = append(uiRow.content, &uiTagButton{tag: tag})
			row.Text = row.Text[tagIndex[1]:]
		}
		uiRow.content = append(uiRow.content, row.Text)
		p.currentUIRows = append(p.currentUIRows, uiRow)
	}

	// refs
	p.currentDBRefs, err = p.db.GetRefsToTagByTagID(p.currentDBTag.ID)
	checkErr(err)
	p.db.GetAllTags()

	p.currentUIRefRows = make(map[db.Tag][]uiRow)
	for tag, rows := range p.currentDBRefs {
		p.currentUIRefRows[tag] = make([]uiRow, 0)
		for _, row := range rows {
			uiRow := uiRow{row: row, editor: widget.Editor{SingleLine: true, Submit: true}}
			uiRow.editor.SetText(uiRow.row.Text)
			for tagIndex := re.FindStringIndex(row.Text); tagIndex != nil; tagIndex = re.FindStringIndex(row.Text) {
				// leading text
				uiRow.content = append(uiRow.content, row.Text[:tagIndex[0]])
				// tag button
				tag, err := p.db.GetTagByName(row.Text[tagIndex[0]+2 : tagIndex[1]-2])
				checkErr(err)
				uiRow.content = append(uiRow.content, &uiTagButton{tag: tag})
				row.Text = row.Text[tagIndex[1]:]
			}
			uiRow.content = append(uiRow.content, row.Text)
			p.currentUIRefRows[tag] = append(p.currentUIRefRows[tag], uiRow)
		}
	}

	// sort our ui ref row keys since map key order isn't stable
	p.sortedRefTagsKeys = make([]db.Tag, len(p.currentDBRefs))
	i := 0
	for k := range p.currentDBRefs {
		p.sortedRefTagsKeys[i] = k
		i++
	}

	sort.Slice(p.sortedRefTagsKeys, func(i, j int) bool { return p.sortedRefTagsKeys[i].Name < p.sortedRefTagsKeys[j].Name })

	return err
}

func main() {
	var exoDB db.ExoDB
	var tag db.Tag
	err := exoDB.Open("./exocortex.db")
	checkErr(err)
	defer exoDB.Close()

	err = exoDB.LoadSchema()
	checkErr(err)

	programState.db = &exoDB
	programState.tagList.Axis = layout.Vertical
	programState.rowList.Axis = layout.Vertical
	programState.refList.Axis = layout.Vertical
	programState.newRowEditor.SingleLine = true
	programState.newRowEditor.Submit = true

	t := time.Now()
	tag, err = programState.db.AddTag(t.Format("January 02 2006"))
	checkErr(err)

	switchTag(tag)

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
	for _, e := range programState.newRowEditor.Events(gtx) {
		switch e := e.(type) {
		case widget.SubmitEvent:
			if programState.newRowEditor.Text() != "" {
				_, err := programState.db.AddRow(programState.currentDBTag.ID, e.Text, 0, 0)
				checkErr(err)
				programState.newRowEditor.SetText("")
				programState.Refresh()
			}
		}
	}
	in := layout.UniformInset(unit.Dp(8))
	for programState.todayButton.Clicked(gtx) {
		in.Layout(gtx, func() {
			t := time.Now()
			tag, err := programState.db.AddTag(t.Format("January 02 2006"))
			checkErr(err)

			switchTag(tag)
		})
	}
	layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		// all tags pane
		layout.Flexed(0.2, func() {
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
				}),
				layout.Rigid(func() {
					in.Layout(gtx, func() {
						in := layout.UniformInset(unit.Dp(4))
						programState.tagList.Layout(gtx, len(programState.allTags), func(i int) {
							in.Layout(gtx, func() {
								programState.allTags[i].layout(gtx, th)
							})
						})
					})
				}),
			)
		}),
		// space
		layout.Flexed(0.05, func() {}),
		// selected tag rows pane
		layout.Flexed(0.75, func() {
			layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func() {
					layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						// current tag name
						layout.Rigid(func() {
							in.Layout(gtx, func() {
								th.H3(programState.currentDBTag.Name).Layout(gtx)
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
					if len(programState.currentDBRefs) > 0 {
						// count total refs for rowlist
						refListLen := 0
						for _, refs := range programState.currentDBRefs {
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
								for _, tag := range programState.sortedRefTagsKeys {
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
}

func (r *uiRow) layout(gtx *layout.Context, th *material.Theme) {
	//fmt.Println(r.editButton)
	for r.editButton.Clicked(gtx) {
		r.editing = !r.editing
		r.editor.Focus()
	}
	for _, e := range r.editor.Events(gtx) {
		switch e := e.(type) {
		case widget.SubmitEvent:
			if r.editor.Text() != "" {
				err := programState.db.UpdateRowText(r.row.ID, e.Text)
				checkErr(err)
			} else {
				programState.db.DeleteRowByID(r.row.ID)
			}
			r.editing = false
			programState.Refresh()
		}
	}
	if !r.editing {
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
		flexChildren = append(flexChildren, layout.Flexed(1, func() {}))
		flexChildren = append(flexChildren, layout.Rigid(func() {
			th.Button("Edit").Layout(gtx, &r.editButton)
		}))
		layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, flexChildren...)
	} else {
		th.Editor("").Layout(gtx, &r.editor)
	}
}

func (t *uiTagButton) layout(gtx *layout.Context, th *material.Theme) {
	for t.button.Clicked(gtx) {
		fmt.Println(t, "clicked")
		switchTag(t.tag)
	}

	button := th.Button(t.tag.Name)
	button.Layout(gtx, &t.button)
}
