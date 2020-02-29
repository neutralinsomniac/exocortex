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

func updatePage() error {
	var err error
	var tags []db.Tag

	if err != nil {
		goto End
	}

	tags, err = exoDB.GetAllTags()
	if err != nil {
		goto End
	}

	page.AllDBTags = tags

End:
	return err
}

func tagHandler(w http.ResponseWriter, r *http.Request) {
	var err error
	var id int

	idString := r.URL.Path[len("/tag/"):]
	id, err = strconv.Atoi(idString)

	page.CurrentTag, err = exoDB.GetTagByID(int64(id))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rootHandler(w, r)
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	var err error

	err = updatePage()
	if err != nil {
		goto End
	}

	page.AllTags = make([]TagURL, 0)
	for _, t := range page.AllDBTags {
		page.AllTags = append(page.AllTags, TagURL{Url: "/tag/" + strconv.Itoa(int(t.ID)), Tag: t})
	}

	err = templates.ExecuteTemplate(w, "index", page)
	if err != nil {
		goto End
	}

End:
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
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
