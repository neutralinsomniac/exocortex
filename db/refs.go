package db

import (
	"database/sql"
)

type Ref struct {
	tag_id int64
	row_id int64
}

func (e *ExoDB) GetRowsReferencingTagByTagName(name string) ([]Row, error) {
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

	rows, err = e.GetRowsReferencingTagByTagID(tag.id)
	if err != nil {
		goto End
	}

End:
	e.decTxRefCount(err == nil)

	return rows, err
}

func (e *ExoDB) GetRowsReferencingTagByTagID(tag_id int64) ([]Row, error) {
	var rows []Row
	var row Row
	var err error
	var sqlRows *sql.Rows
	var statement *sql.Stmt

	err = e.incTxRefCount()
	if err != nil {
		goto End
	}

	statement, err = e.tx.Prepare(`SELECT r.id, r.tag_id, r.parent_row_id, r.text, r.rank, r.updated_ts
								   FROM row as r, tag, ref
								   WHERE tag.id = $1
								   AND tag.id = ref.tag_id
								   AND r.id = ref.row_id
								   ORDER BY r.tag_id asc, r.rank asc`)
	if err != nil {
		goto End
	}

	sqlRows, err = statement.Query(tag_id)
	if err != nil {
		goto End
	}
	defer sqlRows.Close()

	for sqlRows.Next() {
		err = sqlRows.Scan(&row.id, &row.tag_id, &row.parent_row_id, &row.text, &row.rank, &row.updated_ts)
		if err != nil {
			goto End
		}
		rows = append(rows, row)
	}

End:
	e.decTxRefCount(err == nil)

	return rows, err
}
