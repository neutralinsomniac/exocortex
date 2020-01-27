package db

import (
	"database/sql"
	"time"
)

type Row struct {
	id            int64
	tag_id        int64
	rank          int
	text          string
	parent_row_id int64
	updated_ts    int64
}

func sqlGetRowByID(tx *sql.Tx, id int64) (Row, error) {
	var row Row
	var sqlRow *sql.Row
	var err error

	sqlRow = tx.QueryRow("SELECT id, tag_id, text, rank, parent_row_id, updated_ts FROM row WHERE id = $1", id)

	err = sqlRow.Scan(&row.id, &row.tag_id, &row.rank, &row.parent_row_id, &row.updated_ts)
	if err != nil {
		goto End
	}

End:
	return row, err
}

func (e *ExoDB) GetRowByID(id int64) (Row, error) {
	var tx *sql.Tx
	var row Row
	var err error

	tx, err = e.conn.Begin()
	if err != nil {
		goto End
	}

	row, err = sqlGetRowByID(tx, id)
	if err != nil {
		goto End
	}

End:
	sqlCommitOrRollback(tx, err)
	return row, err
}

func sqlGetRowsForTagID(tx *sql.Tx, tag_id int64) ([]Row, error) {
	var rows []Row
	var sqlRows *sql.Rows
	var err error

	sqlRows, err = tx.Query("SELECT id, tag_id, rank, text, parent_row_id, updated_ts FROM rows WHERE tag_id = $1", tag_id)
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

func (e *ExoDB) GetRowsForTagID(tag_id int64) ([]Row, error) {
	var tx *sql.Tx
	var rows []Row
	var err error

	tx, err = e.conn.Begin()
	if err != nil {
		goto End
	}

	rows, err = sqlGetRowsForTagID(tx, tag_id)
	if err != nil {
		goto End
	}

End:
	sqlCommitOrRollback(tx, err)

	return rows, err
}

func sqlAddRow(tx *sql.Tx, tag_id int64, text string, parent_row_id int64) (int64, error) {
	var statement *sql.Stmt
	var res sql.Result
	var row_id int64
	var err error

	statement, err = tx.Prepare("INSERT INTO ref (tag_id, text, parent_row_id, updated_ts) VALUES ($1, $2, $3, $4)")
	if err != nil {
		goto End
	}

	res, err = statement.Exec(tag_id, text, parent_row_id, time.Now().UnixNano())
	if err != nil {
		goto End
	}

	row_id, err = res.LastInsertId()
	if err != nil {
		goto End
	}

End:
	return row_id, err
}

func (e *ExoDB) AddRow(tag_id int64, text string, parent_row_id int64) (Row, error) {
	var tx *sql.Tx
	var row Row
	var row_id int64
	var err error

	tx, err = e.conn.Begin()
	if err != nil {
		goto End
	}

	row_id, err = sqlAddRow(tx, tag_id, text, parent_row_id)
	if err != nil {
		goto End
	}

	row, err = sqlGetRowByID(tx, row_id)
	if err != nil {
		goto End
	}

End:
	return row, err
}

func sqlUpdateRowText(tx *sql.Tx, row_id int64, text string) error {
	var statement *sql.Stmt
	var err error

	statement, err = tx.Prepare("UPDATE row SET text = ? WHERE id = ?")
	if err != nil {
		goto End
	}

	_, err = statement.Exec(text, row_id)
	if err != nil {
		goto End
	}

End:
	return err
}

func (e *ExoDB) UpdateRowText(row_id int64, text string) error {
	var tx *sql.Tx
	var err error

	tx, err = e.conn.Begin()
	if err != nil {
		goto End
	}

	err = sqlUpdateRowText(tx, row_id, text)
	if err != nil {
		goto End
	}

	// update all old refs to this row
	// first remove all old refs

	// now find new refs and create them

End:
	sqlCommitOrRollback(tx, err)

	return err
}
