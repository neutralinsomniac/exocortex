package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"

	"github.com/neutralinsomniac/exocortex/db"
)

var templates = template.Must(template.ParseGlob("templates/*"))

var exoDB db.ExoDB
var page Page

type Page struct {
	AllTags     []db.Tag
	CurrentTag  db.Tag
	CurrentRows []db.Row
}

func checkErr(err error) {
	if err != nil {
		panic(err)
	}
}

func (p *Page) updatePage() error {
	var err error
	var tags []db.Tag

	if err != nil {
		goto End
	}

	tags, err = exoDB.GetAllTags()
	if err != nil {
		goto End
	}

	p.AllTags = tags

End:
	return err
}

func tagHandler(w http.ResponseWriter, r *http.Request) {
	var err error
	var id int
	var idString string

	idString = r.URL.Path[len("/tag/"):]
	id, err = strconv.Atoi(idString)

	page.CurrentTag, err = exoDB.GetTagByID(int64(id))
	if err != nil {
		goto End
	}

End:
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	page.render(w, r)
}

func (p *Page) render(w http.ResponseWriter, r *http.Request) {
	var err error

	err = templates.ExecuteTemplate(w, "index", p)
	if err != nil {
		goto End
	}

End:
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	page.CurrentTag = db.Tag{}

	page.render(w, r)
}

func main() {
	var err error

	err = exoDB.Open("./exocortex.db")
	checkErr(err)

	err = exoDB.LoadSchema()
	checkErr(err)

	err = page.updatePage()
	checkErr(err)

	http.HandleFunc("/", rootHandler)
	http.HandleFunc("/tag/", tagHandler)

	fmt.Println("starting listener...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
