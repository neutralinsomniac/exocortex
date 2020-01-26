package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type ExoDB struct {
	conn        *sql.DB
	tx          *sql.Tx
	tx_refcount int
}

type Tag struct {
	id         int64
	name       string
	updated_ts int64
}

type Row struct {
	id            int64
	tag_id        int64
	rank          int
	text          string
	parent_row_id int64
	updated_ts    int64
}

type Ref struct {
	tag_id int64
	row_id int64
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
	e.tx_refcount = 0
	e.tx = nil
	e.conn.Close()
}

func (e *ExoDB) incTxRefCount() error {
	var err error

	fmt.Println("incTxRefCount() called. e.tx_refcount ==", e.tx_refcount)
	if e.tx == nil {
		e.tx, err = e.conn.Begin()
	}

	if err != nil {
		panic("e.conn.Begin returned: " + err.Error())
	}

	e.tx_refcount++

	return err
}

func (e *ExoDB) decTxRefCount(commit bool) error {
	var err error

	if e.tx_refcount <= 0 {
		fmt.Println("decTxRefCount() called with refcount ==", e.tx_refcount)
	}

	e.tx_refcount--

	// always rollback if we're called with commit == false (something went wrong)
	if e.tx_refcount == 0 || commit == false {
		if commit == true {
			err = e.tx.Commit()
			e.tx = nil
		} else {
			err = e.tx.Rollback()
			e.tx = nil
		}
	}

	return err
}

func (e *ExoDB) AddTag(name string) (Tag, error) {
	var tag Tag
	var err error
	var statement *sql.Stmt
	var id int64
	var res sql.Result

	err = e.incTxRefCount()
	if err != nil {
		goto End
	}

	statement, err = e.tx.Prepare("INSERT INTO tag (name, updated_ts) VALUES (?, ?)")
	if err != nil {
		goto End
	}

	res, err = statement.Exec(name, time.Now().UnixNano())
	if err != nil {
		goto End
	}

	id, err = res.LastInsertId()
	if err != nil {
		goto End
	}

	tag, err = e.GetTagByID(id)

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
	e.decTxRefCount(err == nil)

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
	e.decTxRefCount(err == nil)

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

	rows, err = e.GetRefsToTagByTagID(tag.id)
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

func (e *ExoDB) GetRefsToTagByTagName(name string) ([]Row, error) {
	var tag Tag
	var rows []Row
	var err error

	err = e.incTxRefCount()
	if err != nil {
		goto End
	}

	tag, err = e.GetTagByName(name)
	if err != nil {
		goto End
	}

	rows, err = e.GetRefsToTagByTagID(tag.id)
	if err != nil {
		goto End
	}

End:
	e.decTxRefCount(err == nil)

	return rows, err
}

func (e *ExoDB) GetRefsToTagByTagID(tag_id int64) ([]Row, error) {
	var rows []Row
	var err error

	return rows, err
}

func (e *ExoDB) GetRowsForTagID(tag_id int64) ([]Row, error) {
	var rows []Row
	var sqlRows *sql.Rows
	var err error

	err = e.incTxRefCount()
	if err != nil {
		goto End
	}

	sqlRows, err = e.tx.Query("SELECT id, tag_id, rank, text, parent_row_id, updated_ts FROM rows WHERE tag_id = $1", tag_id)
	if err != nil {
		goto End
	}
	defer sqlRows.Close()

	for sqlRows.Next() {
		var row Row
		err = sqlRows.Scan(&row.id, &row.tag_id, &row.rank, &row.text, &row.parent_row_id, &row.updated_ts)
		if err != nil {
			goto End
		}
		rows = append(rows, row)
	}

End:
	return rows, err
}

func (e *ExoDB) AddRow(tag_id int64, text string, parent_row_id int64) error {
	var err error

	return err
}

func (e *ExoDB) UpdateRowText(row_id int64, text string) error {
	var err error

	return err
}
