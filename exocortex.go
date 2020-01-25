package main

import (
	"github.com/neutralinsomniac/exocortex/db"
)

func checkErr(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	var db db.ExoDB
	err := db.Open("./exocortex.db")
	checkErr(err)
	defer db.Close()
}
