package db

import (
	"testing"

	"github.com/mattn/go-sqlite3"
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
		t.Fatal(err)
	}

	err = db.LoadSchema()
	if err != nil {
		t.Fatal(err)
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
		t.Fatal(err)
	}

	if tag.name != "test" {
		t.Fatal("Returned tag name != expected value")
	}
}

func TestAddDuplicateTag(t *testing.T) {
	var db ExoDB
	var err error

	db = setupDB(t)

	_, err = db.AddTag("test")
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.AddTag("test")
	if sqliteErr, ok := err.(sqlite3.Error); ok {
		if sqliteErr.ExtendedCode != sqlite3.ErrConstraintUnique {
			t.Fatal("duplicate tag did not trigger constraint failure: " + err.Error())
		}
	}
}

func TestGetTags(t *testing.T) {
	var db ExoDB
	var err error
	var tags []Tag

	db = setupDB(t)

	_, err = db.AddTag("test")
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.AddTag("test2")
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.AddTag("test3")
	if err != nil {
		t.Fatal(err)
	}

	tags, err = db.GetTags()
	if err != nil {
		t.Fatal(err)
	}

	if len(tags) != 3 {
		t.Fatal("GetTags() did not return expected number of rows (expected: 3, got: " + string(len(tags)) + ")")
	}

	if tags[0].name != "test3" {
		t.Error("First returned tag != test3")
	}

	if tags[1].name != "test2" {
		t.Error("Second returned tag != test2")
	}

	if tags[2].name != "test" {
		t.Error("Third returned tag != test")
	}
}
