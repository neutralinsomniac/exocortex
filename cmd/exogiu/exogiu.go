package main

import (
	"fmt"
	"time"

	g "github.com/AllenDang/giu"
	"github.com/neutralinsomniac/exocortex/db"
)

type state struct {
	db.State
}

func checkErr(err error) {
	if err != nil {
		panic(err)
	}
}

var programState state

func (p *state) GoToToday() {
	t := time.Now()
	tag, err := programState.DB.AddTag(t.Format("January 02 2006"))
	checkErr(err)

	p.CurrentDBTag = tag
	p.Refresh()
}

func switchTag(tag db.Tag) {
	fmt.Println("called switchTag with tag", tag)
	programState.CurrentDBTag = tag
	programState.Refresh()
}

func getAllTagWidgets() g.Layout {
	layout := make(g.Layout, 0, len(programState.AllDBTags))

	for _, tag := range programState.AllDBTags {
		fmt.Println(tag)
		lineWidget := g.Line(
			g.Button(tag.Name, func() {
				switchTag(tag)
			}),
		)
		layout = append(layout, lineWidget)
	}
	return layout
}

func getAllRowWidgets() g.Layout {
	layout := make(g.Layout, 0, len(programState.CurrentDBRows))

	for _, row := range programState.CurrentDBRows {
		lineWidget := g.Row(g.Label(row.Text))
		layout = append(layout, lineWidget)
	}
	return layout
}

func loop() {
	g.SingleWindow("hello world", g.Layout{
		g.SplitLayout("split", g.DirectionHorizontal, true, 200,
			getAllTagWidgets(),
			getAllRowWidgets(),
		)})
}

func main() {
	var exoDB db.ExoDB
	err := exoDB.Open("./exocortex.db")
	checkErr(err)
	defer exoDB.Close()

	err = exoDB.LoadSchema()
	checkErr(err)

	programState.DB = &exoDB

	programState.GoToToday()

	wnd := g.NewMasterWindow("exogiu", 800, 600, 0, nil)
	wnd.Main(loop)
}
