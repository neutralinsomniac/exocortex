package main

import (
	"fmt"
	"regexp"

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
	db            *db.ExoDB
	tagList       layout.List
	rowList       layout.List
	currentDBTag  db.Tag
	currentDBRows []db.Row
	currentUIRows []uiRow
	allTags       []uiTagButton
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

func main() {
	var db db.ExoDB
	err := db.Open("./exocortex.db")
	checkErr(err)
	defer db.Close()

	err = db.LoadSchema()
	checkErr(err)

	programState.db = &db

	allTags, err := programState.db.GetAllTags()
	checkErr(err)

	for _, tag := range allTags {
		programState.allTags = append(programState.allTags, uiTagButton{tag: tag})
	}

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

	for e := range w.Events() {
		if e, ok := e.(system.FrameEvent); ok {
			gtx.Reset(e.Config, e.Size)

			render(gtx, th)

			e.Frame(gtx.Ops)
		}
	}
}

func render(gtx *layout.Context, th *material.Theme) {
	in := layout.UniformInset(unit.Dp(8))
	layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		// all tags pane
		layout.Flexed(0.2, func() {
			layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func() {
					in.Layout(gtx, func() {
						label := th.H3("Tags")
						label.Layout(gtx)
					})
				}),
				layout.Rigid(func() {
					in.Layout(gtx, func() {
						programState.tagList.Layout(gtx, len(programState.allTags), func(i int) {
							in := layout.UniformInset(unit.Dp(4))
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
		// selected tag pane
		layout.Flexed(0.75, func() {
			layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				// current tag name
				layout.Rigid(func() {
					in.Layout(gtx, func() {
						th.H3(programState.currentDBTag.Name).Layout(gtx)
					})
				}),
				// rows for current tag
				layout.Rigid(func() {
					in.Layout(gtx, func() {
						var cachedUIRows = programState.currentUIRows
						fmt.Println(len(cachedUIRows))
						programState.rowList.Layout(gtx, len(cachedUIRows), func(i int) {
							cachedUIRows[i].layout(gtx, th)
						})
					})
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
	layout.Flex{Axis: layout.Horizontal}.Layout(gtx, flexChildren...)
}

func (t *uiTagButton) layout(gtx *layout.Context, th *material.Theme) {
	var err error
	for t.button.Clicked(gtx) {
		fmt.Println(t, "clicked")
		programState.currentDBTag = t.tag
		programState.currentDBRows, err = programState.db.GetRowsForTagID(t.tag.ID)
		programState.currentUIRows = make([]uiRow, 0)
		checkErr(err)
		// split the text by tags and lay out labels + buttons horizontally
		re := regexp.MustCompile(`\[\[(.*?)\]\]`)
		for _, row := range programState.currentDBRows {
			uiRow := uiRow{row: row}
			for tagIndex := re.FindStringIndex(row.Text); tagIndex != nil; tagIndex = re.FindStringIndex(row.Text) {
				// leading text
				uiRow.content = append(uiRow.content, row.Text[:tagIndex[0]])
				// tag button
				tag, err := programState.db.GetTagByName(row.Text[tagIndex[0]+2 : tagIndex[1]-2])
				checkErr(err)
				uiRow.content = append(uiRow.content, &uiTagButton{tag: tag})
				row.Text = row.Text[tagIndex[1]:]
			}
			uiRow.content = append(uiRow.content, row.Text)
			programState.currentUIRows = append(programState.currentUIRows, uiRow)
		}
	}

	button := th.Button(t.tag.Name)
	button.Layout(gtx, &t.button)
}
