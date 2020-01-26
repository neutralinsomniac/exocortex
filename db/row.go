package db

import (
	"database/sql"
)

type Row struct {
	id            int64
	tag_id        int64
	rank          int
	text          string
	parent_row_id int64
	updated_ts    int64
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
	e.decTxRefCount(err == nil)

	return rows, err
}

func (e *ExoDB) AddRow(tag_id int64, text string, parent_row_id int64) error {
	var err error

	return err
}

func (e *ExoDB) UpdateRowText(row_id int64, text string) error {
	var statement *sql.Stmt
	var err error

	err = e.incTxRefCount()
	if err != nil {
		goto End
	}

	statement, err = e.tx.Prepare("UPDATE row SET text = ? WHERE id = ?")
	if err != nil {
		goto End
	}

	_, err = statement.Exec(text, row_id)
	if err != nil {
		goto End
	}

	// update all old refs to this row
	// first remove all old refs

	// now find new refs and create them

End:
	e.decTxRefCount(err == nil)

	return err
}
