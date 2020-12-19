package main

import (
	"fmt"
	"regexp"
	"time"

	g "github.com/AllenDang/giu"
	"github.com/neutralinsomniac/exocortex/db"
)

type state struct {
	db.State
	currentUIRows    []uiRow
	currentUIRefRows map[db.Tag][]uiRow
}

func checkErr(err error) {
	if err != nil {
		panic(err)
	}
}

type uiRow struct {
	row     db.Row
	content []g.Widget         // Label(s) + Button(s)
	editor  *g.InputTextWidget // Label(s) + Button(s)
	editing bool
}

var programState state

var tagRe = regexp.MustCompile(`\[\[(.*?)\]\]`)

func (p *state) Refresh() error {
	var err error

	fmt.Println("refresh!")
	p.State.Refresh()

	p.currentUIRows = make([]uiRow, 0, len(p.CurrentDBRows))
	for i, row := range p.CurrentDBRows {
		uiRow := uiRow{row: row}
		for tagIndex := tagRe.FindStringIndex(row.Text); tagIndex != nil; tagIndex = tagRe.FindStringIndex(row.Text) {
			// leading text
			uiRow.content = append(uiRow.content, g.LabelWrapped(row.Text[:tagIndex[0]]))
			// tag button
			tag, err := p.DB.GetTagByName(row.Text[tagIndex[0]+2 : tagIndex[1]-2])
			checkErr(err)
			uiRow.content = append(uiRow.content, g.Button(fmt.Sprintf("%s##cur%d", tag.Name, i), func() {
				switchTag(tag)
			}))
			row.Text = row.Text[tagIndex[1]:]
		}
		uiRow.content = append(uiRow.content, g.LabelWrapped(row.Text))
		uiRow.editor = g.InputText(fmt.Sprintf("##rowEditor%d", i), -1, &row.Text)
		p.currentUIRows = append(p.currentUIRows, uiRow)
	}

	p.currentUIRefRows = make(map[db.Tag][]uiRow, len(p.CurrentDBRefs))
	for tag, rows := range p.CurrentDBRefs {
		p.currentUIRefRows[tag] = make([]uiRow, len(rows))
		for i, row := range rows {
			uiRow := uiRow{row: row}
			for tagIndex := tagRe.FindStringIndex(row.Text); tagIndex != nil; tagIndex = tagRe.FindStringIndex(row.Text) {
				// leading text
				uiRow.content = append(uiRow.content, g.LabelWrapped(row.Text[:tagIndex[0]]))
				// tag button
				tag, err := p.DB.GetTagByName(row.Text[tagIndex[0]+2 : tagIndex[1]-2])
				checkErr(err)
				uiRow.content = append(uiRow.content, g.Button(fmt.Sprintf("%s##ref%d", tag.Name, i), func() {
					switchTag(tag)
				}))
				row.Text = row.Text[tagIndex[1]:]
			}
			uiRow.content = append(uiRow.content, g.LabelWrapped(row.Text))
			uiRow.editor = g.InputText(fmt.Sprintf("##rowEditor%d", i), -1, &row.Text)
			p.currentUIRefRows[tag] = append(p.currentUIRefRows[tag], uiRow)
		}
	}

	return err
}

func (p *state) GoToToday() {
	t := time.Now()
	tag, err := programState.DB.AddTag(t.Format("January 02 2006"))
	checkErr(err)

	p.CurrentDBTag = tag
	p.Refresh()
}

func switchTag(tag db.Tag) {
	programState.CurrentDBTag = tag
	programState.Refresh()
}

func getAllTagWidgets() g.Layout {
	layout := make(g.Layout, 0, len(programState.AllDBTags))

	for _, tag := range programState.AllDBTags {
		tag := tag
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
	layout := make(g.Layout, 0, len(programState.currentUIRows))

	for _, row := range programState.currentUIRows {
		row := row
		if !row.editing {
			w := g.Row(g.Line(
				row.content...,
			))
			layout = append(layout, w)
		} else {
			w := g.Row(g.Line(
				row.editor,
			))
			layout = append(layout, w)
		}
	}
	return layout
}

func getAllRowRefWidgets() g.Layout {
	layout := make(g.Layout, 0, len(programState.currentUIRefRows))

	for i, tag := range programState.SortedRefTagsKeys {
		tag := tag
		w := g.Selectable(fmt.Sprintf("%s##tagref%d", tag.Name, i), func() {
			switchTag(tag)
		})
		layout = append(layout, w)
		for _, row := range programState.currentUIRefRows[tag] {
			if !row.editing {
				w := g.Row(g.Line(
					row.content...,
				))
				layout = append(layout, w)
			} else {
				w := g.Row(g.Line(
					row.editor,
				))
				layout = append(layout, w)
			}
		}
	}
	return layout
}

func loop() {
	g.SingleWindow("hello world", g.Layout{
		g.SplitLayout("tagsplit", g.DirectionHorizontal, true, 200,
			getAllTagWidgets(),
			g.Layout{
				g.SplitLayout("refsplit", g.DirectionVertical, true, 200,
					getAllRowWidgets(),
					getAllRowRefWidgets()),
			},
		),
	})
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
