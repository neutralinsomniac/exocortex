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
