package db

import (
	"testing"
)

func checkError(err error) {
	if err != nil {
		panic(err)
	}
}

func setupDB(t *testing.T) ExoDB {
	var db ExoDB
	var err error

	db.debug = true

	err = db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}

	err = db.LoadSchema()
	if err != nil {
		t.Fatal(err)
	}

	return db
}
