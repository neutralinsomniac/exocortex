package db

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

type ExoDB struct {
	conn *sql.DB
}

type Tag struct {
	id   int64
	name string
}

type Row struct {
	id            int64
	tag_id        int64
	rank          int
	text          string
	parent_row_id int64
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
	e.conn.Close()
}

func (e *ExoDB) AddTag(name string) (Tag, error) {
	var tag Tag

	statement, err := e.conn.Prepare("INSERT INTO tag (name) VALUES (?)")
	if err != nil {
		return tag, err
	}
	res, err := statement.Exec(name)
	if err != nil {
		return tag, err
	}
	tag.id, _ = res.LastInsertId()
	tag.name = name

	return tag, err
}

func (e *ExoDB) GetTags() ([]Tag, error) {
	var tags []Tag
	rows, err := e.conn.Query("SELECT id, name FROM tag ORDER BY updated_ts desc")

	if err != nil {
		return nil, err
	}

	var tag Tag
	for rows.Next() {
		rows.Scan(&tag.id, &tag.name)
		tags = append(tags, tag)
	}

	return tags, err
}

func (e *ExoDB) GetTagByID(id int64) (Tag, error) {
	var tag Tag
	row := e.conn.QueryRow("SELECT id, name FROM tag WHERE id = $1", id)

	switch err := row.Scan(&tag.id, &tag.name); err {
	case sql.ErrNoRows:
		return Tag{}, nil
	default:
		return tag, err
	}
}

func (e *ExoDB) RenameTag(oldname string, newname string) error {
	var tx *sql.Tx
	var err error
	var statement *sql.Stmt

	tx, err = e.conn.Begin()
	if err != nil {
		goto End
	}

	statement, err = tx.Prepare("UPDATE tag SET name = ? WHERE name = ?")
	if err != nil {
		goto End
	}

	_, err = statement.Exec(newname, oldname)
	if err != nil {
		goto End
	}

End:
	if tx != nil {
		if err != nil {
			tx.Rollback()
		} else {
			tx.Commit()
		}
	}

	return err
}

func (e *ExoDB) UpdateRowText(row_id int64, text string) error {
	return nil
}
