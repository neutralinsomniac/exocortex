package main

import (
	"image"
	"log"
	"time"

	"github.com/mjl-/duit"
	"github.com/neutralinsomniac/exocortex/db"
)

type state struct {
	db.State
	dui     *duit.DUI
	tagList *duit.List
	rowList *duit.List
}

func checkErr(err error) {
	if err != nil {
		panic(err)
	}
}

func (s *state) Refresh() {
	s.State.Refresh()

	s.tagList.Values = make([]*duit.ListValue, 0)
	for _, tag := range s.AllDBTags {
		newTagItem := &duit.ListValue{
			Text:  tag.Name,
			Value: tag,
		}
		if tag.Name == s.CurrentDBTag.Name {
			newTagItem.Selected = true
		}
		s.tagList.Values = append(s.tagList.Values, newTagItem)
	}

	s.rowList.Values = make([]*duit.ListValue, 0)
	for _, row := range s.CurrentDBRows {
		newRowItem := &duit.ListValue{
			Text:  row.Text,
			Value: row,
		}
		s.rowList.Values = append(s.rowList.Values, newRowItem)
	}
	s.dui.MarkLayout(nil)
}

func (s *state) GoToToday() {
	t := time.Now()
	tag, err := s.DB.AddTag(t.Format("January 02 2006"))
	checkErr(err)

	s.CurrentDBTag = tag
	s.Refresh()
}

func main() {
	var err error
	var programState state

	programState.DB = &db.ExoDB{}

	err = programState.DB.Open("./exocortex.db")
	checkErr(err)

	err = programState.DB.LoadSchema()
	checkErr(err)

	dui, err := duit.NewDUI("exocortex", nil)
	if err != nil {
		log.Fatalf("new duit: %s\n", err)
	}

	programState.dui = dui

	programState.tagList = &duit.List{
		Changed: func(index int) duit.Event {
			switch v := programState.tagList.Values[index].Value.(type) {
			case db.Tag:
				log.Printf("clicked %s\n", v.Name)
				programState.CurrentDBTag = v
				programState.Refresh()
			default:
				log.Fatal("encountered weird type in tag list: %T", v)
			}
			return duit.Event{Consumed: true}
		},
	}

	programState.rowList = &duit.List{
		Changed: func(index int) duit.Event {
			switch v := programState.rowList.Values[index].Value.(type) {
			case db.Row:
				log.Printf("clicked %s\n", v.Text)
			default:
				log.Fatal("encountered weird type in row list: %T", v)
			}
			return duit.Event{Consumed: true}
		},
	}

	dui.Top.UI = &duit.Box{
		Padding: duit.SpaceXY(6, 4), // inset from the window
		Margin:  image.Pt(6, 4),     // space between kids in this box
		Kids: duit.NewKids(&duit.Grid{
			Columns: 2,
			Padding: []duit.Space{
				{Right: 6, Top: 4, Bottom: 4},
				{Left: 6, Top: 4, Bottom: 4},
			},
			Valign: []duit.Valign{duit.ValignMiddle, duit.ValignTop},
			Kids: duit.NewKids(
				&duit.Label{Text: "Tags"},
				&duit.Label{Text: "Rows"},
				&duit.Box{MaxWidth: 200, Kids: duit.NewKids(programState.tagList)},
				&duit.Box{Kids: duit.NewKids(programState.rowList)},
			),
		}),
	}

	programState.GoToToday()
	programState.Refresh()

	dui.Render()

	for {
		select {
		case e := <-dui.Inputs:
			dui.Input(e)

		case warn, ok := <-dui.Error:
			if !ok {
				return
			}
			log.Printf("duit: %s\n", warn)
		}
	}
}
