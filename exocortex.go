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
	db               *db.ExoDB
	tagList          layout.List
	rowList          layout.List
	refList          layout.List
	newRowEditor     widget.Editor
	currentDBTag     db.Tag
	currentDBRows    []db.Row
	currentDBRefs    db.Refs
	currentUIRows    []uiRow
	currentUIRefRows map[db.Tag][]uiRow
	allTags          []uiTagButton
}

type uiTagButton struct {
	tag    db.Tag
	button widget.Button
}

type uiRow struct {
	row     db.Row
	content []interface{} // string(s) + uiTagButton(s)
}

var programState state

func (p *state) Refresh() error {
	var err error

	fmt.Println("refresh!")
	allTags, err := p.db.GetAllTags()
	checkErr(err)

	p.allTags = make([]uiTagButton, 0)
	for _, tag := range allTags {
		p.allTags = append(p.allTags, uiTagButton{tag: tag})
	}
	checkErr(err)
	p.currentDBRows, err = p.db.GetRowsForTagID(p.currentDBTag.ID)
	p.currentUIRows = make([]uiRow, 0)
	checkErr(err)

	// split the text by tags and pre-calculate the row contents
	re := regexp.MustCompile(`\[\[(.*?)\]\]`)
	for _, row := range p.currentDBRows {
		uiRow := uiRow{row: row}
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

	p.currentUIRefRows = make(map[db.Tag][]uiRow)
	for tag, rows := range p.currentDBRefs {
		p.currentUIRefRows[tag] = make([]uiRow, 0)
		for _, row := range rows {
			uiRow := uiRow{row: row}
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

	t := time.Now()
	tag, err = programState.db.AddTag(t.Format("January 02 2006"))
	checkErr(err)

	switchTag(tag)

	programState.Refresh()

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
	programState.tagList.Axis = layout.Vertical
	programState.rowList.Axis = layout.Vertical
	programState.newRowEditor.SingleLine = true
	programState.newRowEditor.Submit = true

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
	layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		// all tags pane
		layout.Flexed(0.2, func() {
			layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func() {
					in.Layout(gtx, func() {
						th.H3("Tags").Layout(gtx)
					})
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
							in.Layout(gtx, func() {
								th.Editor("New row").Layout(gtx, &programState.newRowEditor)
							})
						}),
						// rows for current tag
						layout.Rigid(func() {
							in.Layout(gtx, func() {
								var cachedUIRows = programState.currentUIRows
								programState.rowList.Layout(gtx, len(cachedUIRows), func(i int) {
									cachedUIRows[i].layout(gtx, th)
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
									th.Body1(fmt.Sprintf("%d linked references to %s", len(programState.currentDBRefs), programState.currentDBTag.Name)).Layout(gtx)
								})
							}),

							layout.Rigid(func() {
								var cachedUIRefRows = programState.currentUIRefRows

								keys := make([]db.Tag, len(cachedUIRefRows))
								i := 0
								for k := range cachedUIRefRows {
									keys[i] = k
									i++
								}
								sort.Slice(keys, func(i, j int) bool { return keys[i].Name < keys[j].Name })

								content := make([]interface{}, 0)
								for _, tag := range keys {
									content = append(content, tag)
									for _, uiRefRow := range cachedUIRefRows[tag] {
										content = append(content, uiRefRow)
									}
								}
								programState.rowList.Layout(gtx, len(content), func(i int) {
									in.Layout(gtx, func() {
										switch v := content[i].(type) {
										case db.Tag:
											// source tag for refs
											th.H3(v.Name).Layout(gtx)
										case uiRow:
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
}

func switchTag(tag db.Tag) {
	programState.currentDBTag = tag
	programState.Refresh()
}

func (t *uiTagButton) layout(gtx *layout.Context, th *material.Theme) {
	for t.button.Clicked(gtx) {
		fmt.Println(t, "clicked")
		switchTag(t.tag)
	}

	button := th.Button(t.tag.Name)
	button.Layout(gtx, &t.button)
}
