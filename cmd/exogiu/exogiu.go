package main

import (
	"fmt"
	"regexp"
	"time"

	g "github.com/AllenDang/giu"
	"github.com/AllenDang/giu/imgui"
	"github.com/neutralinsomniac/exocortex/db"
)

type state struct {
	db.State
	addTagStr        string
	datePicker       time.Time
	addRowString     string
	currentUIRows    []*uiRow
	currentUIRefRows map[db.Tag][]*uiRow
}

func checkErr(err error) {
	if err != nil {
		panic(err)
	}
}

type uiRow struct {
	row     db.Row
	content []g.Widget // Label(s) + Button(s)
	editing bool
}

var programState state

var tagRe = regexp.MustCompile(`\[\[(.*?)\]\]`)

func (p *state) Refresh() error {
	var err error

	p.State.Refresh()

	p.datePicker = time.Now()

	p.currentUIRows = make([]*uiRow, 0, len(p.CurrentDBRows))
	for i, row := range p.CurrentDBRows {
		row := row
		uiRow := uiRow{row: row}
		for tagIndex := tagRe.FindStringIndex(row.Text); tagIndex != nil; tagIndex = tagRe.FindStringIndex(row.Text) {
			// leading text
			uiRow.content = append(uiRow.content, g.LabelWrapped(row.Text[:tagIndex[0]]))
			uiRow.content = append(uiRow.content, g.Custom(func() {
				if g.IsItemClicked(g.MouseButtonRight) {
					uiRow.editing = !uiRow.editing
				}
			}))
			// tag button
			tag, err := p.DB.GetTagByName(row.Text[tagIndex[0]+2 : tagIndex[1]-2])
			checkErr(err)
			uiRow.content = append(uiRow.content, g.Button(fmt.Sprintf("%s##cur%d", tag.Name, i), func() {
				switchTag(tag)
			}))
			uiRow.content = append(uiRow.content, g.Custom(func() {
				if g.IsItemClicked(g.MouseButtonRight) {
					uiRow.editing = !uiRow.editing
				}
			}))
			row.Text = row.Text[tagIndex[1]:]
		}
		uiRow.content = append(uiRow.content, g.LabelWrapped(row.Text))
		uiRow.content = append(uiRow.content, g.Custom(func() {
			if g.IsItemClicked(g.MouseButtonRight) {
				uiRow.editing = !uiRow.editing
			}
		}))
		p.currentUIRows = append(p.currentUIRows, &uiRow)
	}

	p.currentUIRefRows = make(map[db.Tag][]*uiRow, len(p.CurrentDBRefs))
	for tag, rows := range p.CurrentDBRefs {
		p.currentUIRefRows[tag] = make([]*uiRow, 0, len(rows))
		for i, row := range rows {
			row := row
			uiRow := uiRow{row: row}
			for tagIndex := tagRe.FindStringIndex(row.Text); tagIndex != nil; tagIndex = tagRe.FindStringIndex(row.Text) {
				// leading text
				uiRow.content = append(uiRow.content, g.LabelWrapped(row.Text[:tagIndex[0]]))
				uiRow.content = append(uiRow.content, g.Custom(func() {
					if g.IsItemClicked(g.MouseButtonRight) {
						uiRow.editing = !uiRow.editing
					}
				}))
				// tag button
				tag, err := p.DB.GetTagByName(row.Text[tagIndex[0]+2 : tagIndex[1]-2])
				checkErr(err)
				uiRow.content = append(uiRow.content, g.Button(fmt.Sprintf("%s##ref%d", tag.Name, i), func() {
					switchTag(tag)
				}))
				uiRow.content = append(uiRow.content, g.Custom(func() {
					if g.IsItemClicked(g.MouseButtonRight) {
						uiRow.editing = !uiRow.editing
					}
				}))
				row.Text = row.Text[tagIndex[1]:]
			}
			uiRow.content = append(uiRow.content, g.LabelWrapped(row.Text))
			uiRow.content = append(uiRow.content, g.Custom(func() {
				if g.IsItemClicked(g.MouseButtonRight) {
					uiRow.editing = !uiRow.editing
				}
			}))
			p.currentUIRefRows[tag] = append(p.currentUIRefRows[tag], &uiRow)
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
	if tag.ID != programState.CurrentDBTag.ID && programState.CurrentDBTag.ID != 0 {
		err := programState.DeleteTagIfEmpty(programState.CurrentDBTag.ID)
		checkErr(err)
	}
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

	for i, row := range programState.currentUIRows {
		row := row
		if !row.editing {
			w := g.Row(
				g.Line(
					row.content...,
				),
				/*g.Custom(func() {
					if g.IsItemClicked(g.MouseButtonRight) {
						row.editing = !row.editing
					}
				}),*/
			)
			layout = append(layout, w)
		} else {
			w := g.Row(g.Line(
				g.InputTextV(fmt.Sprintf("##rowEditor%d", i), -1, &row.row.Text, g.InputTextFlagsEnterReturnsTrue, nil, func() {
					if len(row.row.Text) > 0 {
						programState.DB.UpdateRowText(row.row.ID, row.row.Text)
					} else {
						programState.DB.DeleteRowByID(row.row.ID)
					}
					programState.Refresh()
				}),
			))
			layout = append(layout, w)
		}
	}
	return layout
}

func getAllRowRefWidgets() g.Layout {
	layout := make(g.Layout, 0, len(programState.currentUIRefRows))

	layout = append(layout, g.Label(fmt.Sprintf("References to %s", programState.CurrentDBTag.Name)))

	for i, tag := range programState.SortedRefTagsKeys {
		tag := tag
		w := g.Selectable(fmt.Sprintf("%s##tagref%d", tag.Name, i), func() {
			switchTag(tag)
		})
		layout = append(layout, w)
		for i, row := range programState.currentUIRefRows[tag] {
			row := row
			if !row.editing {
				w := g.Row(
					g.Line(
						row.content...,
					),
					/*g.Custom(func() {
						if g.IsItemClicked(g.MouseButtonRight) {
							row.editing = !row.editing
						}
					}),*/
				)
				layout = append(layout, w)
			} else {
				w := g.Row(g.Line(
					g.InputTextV(fmt.Sprintf("##rowRefEditor%d", i), -1, &row.row.Text, g.InputTextFlagsEnterReturnsTrue|g.InputTextFlagsNoHorizontalScroll, nil, func() {
						if len(row.row.Text) > 0 {
							programState.DB.UpdateRowText(row.row.ID, row.row.Text)
						} else {
							programState.DB.DeleteRowByID(row.row.ID)
						}
						programState.Refresh()
					}),
				))
				layout = append(layout, w)
			}
		}
	}
	return layout
}

func loop() {
	g.SingleWindow("exogiu", g.Layout{
		g.SplitLayout("tagsplit", g.DirectionHorizontal, true, 200,
			g.Layout{
				g.InputTextV("##addtag", -1, &programState.addTagStr, g.InputTextFlagsEnterReturnsTrue, nil, func() {
					tag, err := programState.DB.AddTag(programState.addTagStr)
					if err == nil {
						programState.addTagStr = ""
						programState.CurrentDBTag = tag
						programState.Refresh()
						imgui.SetKeyboardFocusHere()
					}
				}),
				g.DatePicker("##date", &programState.datePicker, 0, func() {
					tagStr := programState.datePicker.Format("January 02 2006")
					tag, err := programState.DB.AddTag(tagStr)
					if err == nil {
						switchTag(tag)
					}
				}),
				getAllTagWidgets(),
			},
			g.Layout{
				g.Label(fmt.Sprintf("%s", programState.CurrentDBTag.Name)),
				g.InputTextV("##addrow", -1, &programState.addRowString, g.InputTextFlagsEnterReturnsTrue, nil, func() {
					if len(programState.addRowString) > 0 {
						programState.DB.AddRow(programState.CurrentDBTag.ID, programState.addRowString, 0)
						programState.addRowString = ""
						programState.Refresh()
					}
					imgui.SetKeyboardFocusHere()
				}),
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

	wnd := g.NewMasterWindow("exogiu", 800, 600, 0, nil)

	programState.GoToToday()

	wnd.Main(loop)
}
