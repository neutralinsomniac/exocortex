package main

import (
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
	db          *db.ExoDB
	tagList     layout.List
	rowList     layout.List
	currentTag  db.Tag
	currentRows []db.Row
	allTags     []tagButton
}

type tagButton struct {
	tag    db.Tag
	button widget.Button
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
		programState.allTags = append(programState.allTags, tagButton{tag: tag})
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

			programState.render(gtx, th)

			e.Frame(gtx.Ops)
		}
	}
}

func (s *state) render(gtx *layout.Context, th *material.Theme) {
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
						s.tagList.Layout(gtx, len(s.allTags), func(i int) {
							s.allTags[i].layout(gtx, th, s)
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
				layout.Rigid(func() {
					in.Layout(gtx, func() {
						th.H3(s.currentTag.Name).Layout(gtx)
					})
				}),
				layout.Rigid(func() {
					in.Layout(gtx, func() {
						s.rowList.Layout(gtx, len(s.currentRows), func(i int) {
							th.Body1(s.currentRows[i].Text).Layout(gtx)
						})
					})
				}),
			)
		}),
	)
}

func (t *tagButton) layout(gtx *layout.Context, th *material.Theme, s *state) {
	var err error
	for t.button.Clicked(gtx) {
		s.currentTag = t.tag
		s.currentRows, err = s.db.GetRowsForTagID(t.tag.ID)
		checkErr(err)
	}

	in := layout.UniformInset(unit.Dp(4))
	in.Layout(gtx, func() {
		button := th.Button(t.tag.Name)
		button.Layout(gtx, &t.button)
	})
}
