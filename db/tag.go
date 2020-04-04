package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"
)

type Tag struct {
	ID        int64
	Name      string
	UpdatedTS int64
}

func sqlAddTag(tx *sql.Tx, name string) (int64, error) {
	var statement *sql.Stmt
	var res sql.Result
	var tagID int64
	var tag Tag
	var duplicateEntry bool
	var err error

	statement, err = tx.Prepare("INSERT INTO tag (name, updated_ts) VALUES (?, ?)")
	if err != nil {
		goto End
	}

	res, err = statement.Exec(name, time.Now().UnixNano())
	// it's not an error if this tag name already exists
	if sqliteErr, ok := err.(sqlite3.Error); ok {
		if sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique {
			duplicateEntry = true
		}
	}
	if err != nil && duplicateEntry == false {
		goto End
	}

	if duplicateEntry == false {
		tagID, err = res.LastInsertId()
		if err != nil {
			goto End
		}
	} else {
		tag, err = sqlGetTagByName(tx, name)
		if err != nil {
			goto End
		}
		tagID = tag.ID
	}

End:
	return tagID, err
}

func sqlGetTagByName(tx *sql.Tx, name string) (Tag, error) {
	var tag Tag
	var sqlRow *sql.Row
	var err error

	sqlRow = tx.QueryRow("SELECT id, name, updated_ts FROM tag WHERE name = $1", name)

	err = sqlRow.Scan(&tag.ID, &tag.Name, &tag.UpdatedTS)
	if err != nil {
		goto End
	}

End:
	return tag, err
}

func (e *ExoDB) AddTag(name string) (Tag, error) {
	var tx *sql.Tx
	var tag Tag
	var tagID int64
	var err error

	tx, err = e.conn.Begin()
	if err != nil {
		goto End
	}

	tagID, err = sqlAddTag(tx, name)
	if err != nil {
		goto End
	}

	tag, err = sqlGetTagByID(tx, tagID)
	if err != nil {
		goto End
	}

End:
	sqlCommitOrRollback(tx, err)

	return tag, err
}

func sqlGetAllTags(tx *sql.Tx) ([]Tag, error) {
	var tags []Tag
	var tag Tag
	var sqlRows *sql.Rows
	var err error

	sqlRows, err = tx.Query("SELECT id, name, updated_ts FROM tag ORDER BY updated_ts desc")
	if err != nil {
		goto End
	}
	defer sqlRows.Close()

	for sqlRows.Next() {
		err = sqlRows.Scan(&tag.ID, &tag.Name, &tag.UpdatedTS)
		if err != nil {
			goto End
		}
		tags = append(tags, tag)
	}

End:
	return tags, err
}

func (e *ExoDB) GetAllTags() ([]Tag, error) {
	var tags []Tag
	var tx *sql.Tx
	var err error

	tx, err = e.conn.Begin()
	if err != nil {
		goto End
	}

	tags, err = sqlGetAllTags(tx)

End:
	sqlCommitOrRollback(tx, err)

	return tags, err
}

func sqlUpdateTagTS(tx *sql.Tx, id int64) error {
	var statement *sql.Stmt
	var err error

	statement, err = tx.Prepare("UPDATE tag SET updated_ts = ? WHERE id = ?")
	if err != nil {
		goto End
	}

	_, err = statement.Exec(time.Now().UnixNano(), id)
	if err != nil {
		goto End
	}

End:
	return err
}
func sqlGetTagByID(tx *sql.Tx, id int64) (Tag, error) {
	var tag Tag
	var err error
	var sqlRow *sql.Row

	sqlRow = tx.QueryRow("SELECT id, name, updated_ts FROM tag WHERE id = $1", id)

	err = sqlRow.Scan(&tag.ID, &tag.Name, &tag.UpdatedTS)
	if err != nil {
		goto End
	}

End:
	return tag, err
}

func (e *ExoDB) GetTagByID(id int64) (Tag, error) {
	var tx *sql.Tx
	var err error
	var tag Tag

	tx, err = e.conn.Begin()
	if err != nil {
		goto End
	}

	tag, err = sqlGetTagByID(tx, id)

End:
	sqlCommitOrRollback(tx, err)

	return tag, err
}

func (e *ExoDB) GetTagByName(name string) (Tag, error) {
	var tag Tag
	var tx *sql.Tx
	var err error

	tx, err = e.conn.Begin()
	if err != nil {
		goto End
	}

	tag, err = sqlGetTagByName(tx, name)

End:
	sqlCommitOrRollback(tx, err)

	return tag, err
}

func sqlDeleteTagByID(tx *sql.Tx, id int64) error {
	var statement *sql.Stmt
	var err error

	statement, err = tx.Prepare("DELETE FROM tag WHERE id = ?")
	if err != nil {
		goto End
	}

	_, err = statement.Exec(id)
	if err != nil {
		goto End
	}

End:
	return err
}

func (e *ExoDB) DeleteTagByID(id int64) error {
	var tx *sql.Tx
	var err error

	tx, err = e.conn.Begin()
	if err != nil {
		goto End
	}

	err = sqlDeleteTagByID(tx, id)
	if err != nil {
		goto End
	}

End:
	sqlCommitOrRollback(tx, err)
	return err
}

func sqlUpdateTagName(tx *sql.Tx, oldname string, newname string) error {
	var statement *sql.Stmt
	var err error

	statement, err = tx.Prepare("UPDATE tag SET name = ?, updated_ts = ? WHERE name = ?")
	if err != nil {
		goto End
	}

	_, err = statement.Exec(newname, time.Now().UnixNano(), oldname)
	if err != nil {
		goto End
	}

End:
	return err
}

func (e *ExoDB) RenameTag(oldname string, newname string) (Tag, error) {
	var tx *sql.Tx
	var refs Refs
	var tag Tag
	var err error

	tx, err = e.conn.Begin()
	if err != nil {
		goto End
	}

	err = sqlUpdateTagName(tx, oldname, newname)
	if err != nil {
		goto End
	}

	// Now update all rows that reference oldname
	tag, err = sqlGetTagByName(tx, newname)
	if err != nil {
		goto End
	}

	refs, err = sqlGetRefsToTagByTagID(tx, tag.ID)
	if err != nil {
		goto End
	}

	for _, rows := range refs {
		for _, row := range rows {
			oldtag := fmt.Sprintf("[[%s]]", oldname)
			newtag := fmt.Sprintf("[[%s]]", newname)
			err = sqlUpdateRowText(tx, row.ID, strings.ReplaceAll(row.Text, oldtag, newtag))
			if err != nil {
				goto End
			}
		}
	}

End:
	sqlCommitOrRollback(tx, err)

	return tag, err
}
