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

type TagURL struct {
	db.Tag
	Url string
}

type Page struct {
	AllTags     []TagURL
	AllDBTags   []db.Tag
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

	p.AllDBTags = tags

	p.AllTags = make([]TagURL, 0)
	for _, t := range p.AllDBTags {
		p.AllTags = append(p.AllTags, TagURL{Url: "/tag/" + strconv.Itoa(int(t.ID)), Tag: t})
	}

End:
	return err
}

func tagHandler(w http.ResponseWriter, r *http.Request) {
	var err error
	var id int
	var page Page
	var idString string

	err = page.updatePage()
	if err != nil {
		goto End
	}

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
	var err error
	var page Page

	err = page.updatePage()
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

func main() {
	var err error

	err = exoDB.Open("./exocortex.db")
	checkErr(err)

	err = exoDB.LoadSchema()
	checkErr(err)

	exoDB.AddTag("test1")
	exoDB.AddTag("test2")
	exoDB.AddTag("test3")
	http.HandleFunc("/", rootHandler)
	http.HandleFunc("/tag/", tagHandler)
	fmt.Println("starting listener...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
