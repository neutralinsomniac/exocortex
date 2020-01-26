package db

import (
	"database/sql"
)

type Ref struct {
	tag_id int64
	row_id int64
}

func sqlGetRowsReferencingTagByTagID(tx *sql.Tx, tag_id int64) ([]Row, error) {
	var statement *sql.Stmt
	var sqlRows *sql.Rows
	var row Row
	var rows []Row
	var err error

	statement, err = tx.Prepare(`SELECT r.id, r.tag_id, r.parent_row_id, r.text, r.rank, r.updated_ts
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
	return rows, err
}

func (e *ExoDB) GetRowsReferencingTagByTagName(name string) ([]Row, error) {
	var tx *sql.Tx
	var tag Tag
	var rows []Row
	var err error

	tx, err = e.conn.Begin()
	if err != nil {
		goto End
	}

	tag, err = sqlGetTagByName(tx, name)
	if err != nil {
		goto End
	}

	rows, err = sqlGetRowsReferencingTagByTagID(tx, tag.id)
	if err != nil {
		goto End
	}

End:
	sqlCommitOrRollback(tx, err)

	return rows, err
}

func (e *ExoDB) GetRowsReferencingTagByTagID(tag_id int64) ([]Row, error) {
	var tx *sql.Tx
	var rows []Row
	var err error

	tx, err = e.conn.Begin()
	if err != nil {
		goto End
	}

	rows, err = sqlGetRowsReferencingTagByTagID(tx, tag_id)
	if err != nil {
		goto End
	}

End:
	sqlCommitOrRollback(tx, err)

	return rows, err
}
