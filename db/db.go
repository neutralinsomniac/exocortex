package db

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

type ExoDB struct {
	conn *sql.DB
}

func (e *ExoDB) Open(filename string) error {
	var err error
	e.conn, err = sql.Open("sqlite3", filename)
	return err
}

func (e *ExoDB) Close() {
	e.conn.Close()
}

func (e *ExoDB) AddTag(tag string) error {
	statement, err := e.conn.Prepare("INSERT INTO tag (name) VALUES (?)")
	if err != nil {
		return err
	}
	_, err = statement.Exec(tag)

	return err
}

func (e *ExoDB) GetTags() ([]string, error) {
	var tags []string
	rows, err := e.conn.Query("SELECT name FROM tag")

	if err != nil {
		return nil, err
	}

	var tag string
	for rows.Next() {
		rows.Scan(&tag)
		tags = append(tags, tag)
	}

	return tags, err
}
