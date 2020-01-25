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

func (e *ExoDB) AddTag(tag string) {
	statement, _ := e.conn.Prepare("INSERT INTO tag (name) VALUES (?)")
	statement.Exec(tag)
}

func (e *ExoDB) GetTags() []string {
	var tags []string
	rows, _ := e.conn.Query("SELECT name FROM tag")

	var tag string
	for rows.Next() {
		rows.Scan(&tag)
		tags = append(tags, tag)
	}

	return tags
}
