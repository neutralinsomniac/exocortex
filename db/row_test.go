package db

import (
	"fmt"
	"testing"
)

func TestAddRow(t *testing.T) {
	var db ExoDB
	var tag Tag
	var row Row
	var err error

	rowText := "test tag [[test2]]"

	db = setupDB(t)

	tag, err = db.AddTag("test")
	if err != nil {
		t.Fatal("AddTag failed: " + err.Error())
	}

	row, err = db.AddRow(tag.ID, rowText, 0, 0)
	if err != nil {
		t.Fatal("AddRow failed: " + err.Error())
	}

	if row.tagID != tag.ID {
		t.Fatal("row.tagID != tag.id")
	}

	if row.text != rowText {
		t.Fatal("row.text != rowText")
	}
}

func TestGetRowsForTagID(t *testing.T) {
	var db ExoDB
	var tag Tag
	var rows []Row
	var err error

	row1Text := "test tag [[test1]]"
	row2Text := "test tag [[test2]]"

	db = setupDB(t)

	tag, err = db.AddTag("test")
	if err != nil {
		t.Fatal("AddTag failed: " + err.Error())
	}

	_, err = db.AddRow(tag.ID, row1Text, 0, 0)
	if err != nil {
		t.Fatal("AddRow failed: " + err.Error())
	}

	_, err = db.AddRow(tag.ID, row2Text, 0, 0)
	if err != nil {
		t.Fatal("AddRow failed: " + err.Error())
	}

	rows, err = db.GetRowsForTagID(tag.ID)
	if err != nil {
		t.Fatal("GetRowsForTagID failed: " + err.Error())
	}

	if len(rows) != 2 {
		t.Fatal(fmt.Sprintf("GetRowsForTagID did not return 2 results (returned %d)", len(rows)))
	}

	if rows[0].text != row1Text {
		t.Fatal("GetRowsForTagID row 1 text does not match expected")
	}

	if rows[1].text != row2Text {
		t.Fatal("GetRowsForTagID row 1 text does not match expected")
	}
}
