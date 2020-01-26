package db

import (
	"database/sql"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"
)

type Tag struct {
	id         int64
	name       string
	updated_ts int64
}

func (e *ExoDB) AddTag(name string) (Tag, error) {
	var tag Tag
	var err error
	var statement *sql.Stmt
	var tagAlreadyAdded bool

	err = e.incTxRefCount()
	if err != nil {
		goto End
	}

	statement, err = e.tx.Prepare("INSERT INTO tag (name, updated_ts) VALUES (?, ?)")
	if err != nil {
		goto End
	}

	_, err = statement.Exec(name, time.Now().UnixNano())
	// it's not an error if this tag name already exists
	if sqliteErr, ok := err.(sqlite3.Error); ok {
		if sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique {
			tagAlreadyAdded = true
		}
	}
	if err != nil && !tagAlreadyAdded {
		goto End
	}

	tag, err = e.GetTagByName(name)
	if err != nil {
		goto End
	}

End:
	e.decTxRefCount(err == nil)

	return tag, err
}

func (e *ExoDB) GetAllTags() ([]Tag, error) {
	var tags []Tag
	var tag Tag
	var err error
	var sqlRows *sql.Rows

	err = e.incTxRefCount()
	if err != nil {
		goto End
	}

	sqlRows, err = e.tx.Query("SELECT id, name, updated_ts FROM tag ORDER BY updated_ts desc")
	if err != nil {
		goto End
	}
	defer sqlRows.Close()

	for sqlRows.Next() {
		err = sqlRows.Scan(&tag.id, &tag.name, &tag.updated_ts)
		if err != nil {
			goto End
		}
		tags = append(tags, tag)
	}

End:
	e.decTxRefCount(err == nil)

	return tags, err
}

func (e *ExoDB) GetTagByID(id int64) (Tag, error) {
	var tag Tag
	var err error
	var sqlRow *sql.Row

	err = e.incTxRefCount()
	if err != nil {
		goto End
	}

	sqlRow = e.tx.QueryRow("SELECT id, name, updated_ts FROM tag WHERE id = $1", id)

	err = sqlRow.Scan(&tag.id, &tag.name, &tag.updated_ts)
	if err != nil {
		goto End
	}

End:
	// it's not an error to return no rows
	e.decTxRefCount(err == nil || err == sql.ErrNoRows)

	return tag, err
}

func (e *ExoDB) GetTagByName(name string) (Tag, error) {
	var tag Tag
	var err error
	var sqlRow *sql.Row

	err = e.incTxRefCount()
	if err != nil {
		goto End
	}

	sqlRow = e.tx.QueryRow("SELECT id, name, updated_ts FROM tag WHERE name = $1", name)

	err = sqlRow.Scan(&tag.id, &tag.name, &tag.updated_ts)
	if err != nil {
		goto End
	}

End:
	// it's not an error to return no rows
	e.decTxRefCount(err == nil || err == sql.ErrNoRows)

	return tag, err
}

func (e *ExoDB) RenameTag(oldname string, newname string) error {
	var rows []Row
	var err error
	var statement *sql.Stmt
	var tag Tag

	err = e.incTxRefCount()
	if err != nil {
		goto End
	}

	statement, err = e.tx.Prepare("UPDATE tag SET name = ?, updated_ts = ? WHERE name = ?")
	if err != nil {
		goto End
	}

	_, err = statement.Exec(newname, time.Now().UnixNano(), oldname)
	if err != nil {
		goto End
	}

	// Now update all rows that reference oldname
	_, err = e.GetTagByName(newname)
	if err != nil {
		goto End
	}

	rows, err = e.GetRowsReferencingTagByTagID(tag.id)
	if err != nil {
		goto End
	}

	for _, row := range rows {
		err = e.UpdateRowText(row.id, strings.ReplaceAll(row.text, "[["+oldname+"]]", "[["+newname+"]]"))
		if err != nil {
			goto End
		}
	}

End:
	e.decTxRefCount(err == nil)

	return err
}
