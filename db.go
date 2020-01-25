package db

import (
	"database/sql"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
)

type ExoDB struct {
	conn *sql.DB
}

func (e *ExoDB) Open(filename string) string {
	e.conn, err = sql.Open("sqlite3", filename)
	return err
}

func (e *ExoDB) AddTag(tag string) {

}
