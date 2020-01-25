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

	err = db.Open(":memory:")
	if err != nil {
		t.Error(err)
	}

	// load the schema
	err = db.LoadSchema()
	if err != nil {
		t.Error(err)
	}

	return db
}

func TestAddTag(t *testing.T) {
	var db ExoDB
	var err error
	var tag Tag

	db = setupDB(t)

	tag, err = db.AddTag("test")
	if err != nil {
		t.Error(err)
	}

	if tag.name != "test" {
		t.Error("returned tag name != expected value")
	}
}
