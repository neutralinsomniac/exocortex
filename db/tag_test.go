package db

import (
	"fmt"
	"testing"

	"github.com/mattn/go-sqlite3"
)

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

func TestGetTagByID(t *testing.T) {
	var db ExoDB
	var err error
	var tag Tag

	db = setupDB(t)

	tag, err = db.AddTag("test")
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.GetTagByID(tag.id)
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetTagByName(t *testing.T) {
	var db ExoDB
	var err error
	var tag Tag

	db = setupDB(t)

	_, err = db.AddTag("test")
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.AddTag("test2")
	if err != nil {
		t.Fatal(err)
	}

	tag, err = db.GetTagByName("test")
	if err != nil {
		t.Fatal(err)
	}

	if tag.name != "test" {
		t.Fatal("returned tag name does not match expected (expected: test, got: " + tag.name + ")")
	}
}

func TestGetAllTags(t *testing.T) {
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

	tags, err = db.GetAllTags()
	if err != nil {
		t.Fatal(err)
	}

	if len(tags) != 3 {
		t.Fatal(fmt.Sprintf("GetAllTags() did not return expected number of rows (expected: 3, got: %d)", len(tags)))
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

func TestRenameTag(t *testing.T) {
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

	err = db.RenameTag("test", "test4")
	if err != nil {
		t.Fatal(err)
	}

	tags, err = db.GetAllTags()
	if err != nil {
		t.Fatal(err)
	}

	if len(tags) != 3 {
		t.Fatal("GetAllTags() did not return expected number of rows (expected: 3, got: " + string(len(tags)) + ")")
	}

	if tags[0].name != "test4" {
		t.Error("First tag != expected (expected: test4, got: " + tags[0].name + ")")
	}
	if tags[1].name != "test3" {
		t.Error("Second tag != expected (expected: test3, got: " + tags[1].name + ")")
	}
	if tags[2].name != "test2" {
		t.Error("Third tag != expected (expected: test2, got: " + tags[2].name + ")")
	}
}