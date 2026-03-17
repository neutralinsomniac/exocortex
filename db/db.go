package db

import (
	"database/sql"
	"strings"

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

// Migrate applies incremental schema changes to existing databases.
func (e *ExoDB) Migrate() error {
	migrations := []string{
		`ALTER TABLE row ADD COLUMN note TEXT NOT NULL DEFAULT ''`,
	}
	for _, m := range migrations {
		if _, err := e.conn.Exec(m); err != nil {
			if !strings.Contains(err.Error(), "duplicate column name") {
				return err
			}
		}
	}
	return nil
}

func (e *ExoDB) Open(filename string) error {
	var err error

	e.conn, err = sql.Open("sqlite3", filename)
	if err != nil {
		goto End
	}

	err = e.enableForeignKeys()

End:
	return err
}

func (e *ExoDB) Close() {
	e.conn.Close()
}

func (e *ExoDB) enableForeignKeys() error {
	var err error

	_, err = e.conn.Exec("PRAGMA foreign_keys = ON")

	return err
}

func sqlCommitOrRollback(tx *sql.Tx, err error) {
	if err != nil {
		tx.Rollback()
	} else {
		tx.Commit()
	}
}
