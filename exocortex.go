package main

import (
	"fmt"
	"image"

	"gioui.org/app"
	"gioui.org/io/pointer"
	"gioui.org/io/system"
	"gioui.org/layout"
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
	tagList layout.List
	allTags []tagButton
}

type tagButton struct {
	tag db.Tag
}

var programState state

func main() {
	var db db.ExoDB
	err := db.Open("./exocortex.db")
	checkErr(err)
	defer db.Close()

	err = db.LoadSchema()
	checkErr(err)

	allTags, err := db.GetAllTags()
	checkErr(err)

	for _, tag := range allTags {
		programState.allTags = append(programState.allTags, tagButton{tag})
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

	for e := range w.Events() {
		if e, ok := e.(system.FrameEvent); ok {
			gtx.Reset(e.Config, e.Size)

			programState.render(gtx, th)

			e.Frame(gtx.Ops)
		}
	}
}

func (s *state) render(gtx *layout.Context, th *material.Theme) {
	layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func() {
			th.H3("Tags").Layout(gtx)
		}),
		layout.Rigid(func() {
			s.tagList.Layout(gtx, len(s.allTags), func(i int) {
				s.allTags[i].layout(gtx, th)
			})
		}))
}

func (t *tagButton) layout(gtx *layout.Context, th *material.Theme) {
	for _, e := range gtx.Events(t) {
		if e, ok := e.(pointer.Event); ok {
			if e.Type == pointer.Press {
				fmt.Println("tag", t.tag.Name, "pressed")
			}
		}
	}

	pointer.Rect(image.Rectangle{Max: image.Point{X: 500, Y: 500}}).Add(gtx.Ops)
	pointer.InputOp{Key: t}.Add(gtx.Ops)
	th.Body1(t.tag.Name).Layout(gtx)
}
