package db

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

type ExoDB struct {
	conn  *sql.DB
	debug bool
}

func (e *ExoDB) LoadSchema() error {
	_, err := e.conn.Exec(schema)
	return err
}

func (e *ExoDB) Open(filename string) error {
	var err error
	e.conn, err = sql.Open("sqlite3", filename)
	return err
}

func (e *ExoDB) Close() {
	e.conn.Close()
}

func sqlCommitOrRollback(tx *sql.Tx, err error) {
	if err != nil {
		tx.Rollback()
	} else {
		tx.Commit()
	}
}
