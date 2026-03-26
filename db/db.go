package db

import (
	"database/sql"
	"strings"
	"time"

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
		`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL DEFAULT '')`,
		`ALTER TABLE row ADD COLUMN priority INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE row ADD COLUMN done INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE row ADD COLUMN uuid TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS row_tombstone (uuid TEXT PRIMARY KEY, deleted_ts INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS tag_tombstone (name TEXT PRIMARY KEY, deleted_ts INTEGER NOT NULL)`,
		`ALTER TABLE tag ADD COLUMN uuid TEXT NOT NULL DEFAULT ''`,
		`DROP TABLE IF EXISTS tag_tombstone`,
		`CREATE TABLE IF NOT EXISTS tag_tombstone (uuid TEXT PRIMARY KEY, deleted_ts INTEGER NOT NULL)`,
	}
	for _, m := range migrations {
		if _, err := e.conn.Exec(m); err != nil {
			if !strings.Contains(err.Error(), "duplicate column name") &&
				!strings.Contains(err.Error(), "already exists") {
				return err
			}
		}
	}
	if err := e.populateRowUUIDs(); err != nil {
		return err
	}
	if err := e.populateTagUUIDs(); err != nil {
		return err
	}
	return e.fixRowTimestamps()
}

// fixRowTimestamps assigns a current timestamp to any rows with a NULL or zero
// updated_ts. These rows predate reliable timestamp tracking and would otherwise
// be invisible to sync (sqlGetRowsSince filters on updated_ts > 0).
func (e *ExoDB) fixRowTimestamps() error {
	rows, err := e.conn.Query("SELECT id FROM row WHERE updated_ts IS NULL OR updated_ts = 0")
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	now := time.Now().UnixNano()
	for _, id := range ids {
		if _, err = e.conn.Exec("UPDATE row SET updated_ts = ? WHERE id = ?", now, id); err != nil {
			return err
		}
	}
	return nil
}

// populateTagUUIDs assigns a UUID to any existing tags that predate the uuid column.
func (e *ExoDB) populateTagUUIDs() error {
	rows, err := e.conn.Query("SELECT id FROM tag WHERE uuid = ''")
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if _, err = e.conn.Exec("UPDATE tag SET uuid = ? WHERE id = ?", newUUID(), id); err != nil {
			return err
		}
	}
	return nil
}

// populateRowUUIDs assigns a UUID to any existing rows that predate the uuid column.
func (e *ExoDB) populateRowUUIDs() error {
	rows, err := e.conn.Query("SELECT id FROM row WHERE uuid = ''")
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if _, err = e.conn.Exec("UPDATE row SET uuid = ? WHERE id = ?", newUUID(), id); err != nil {
			return err
		}
	}
	return nil
}

func (e *ExoDB) GetSetting(key string) (string, error) {
	var value string
	err := e.conn.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (e *ExoDB) SetSetting(key, value string) error {
	_, err := e.conn.Exec("INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)", key, value)
	return err
}

func (e *ExoDB) Open(filename string) error {
	var err error

	e.conn, err = sql.Open("sqlite3", filename)
	if err != nil {
		goto End
	}

	// SQLite only supports one writer at a time; a single connection
	// serialises all operations and prevents "database is locked" errors.
	e.conn.SetMaxOpenConns(1)

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
