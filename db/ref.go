package db

import (
	"database/sql"
)

// Refs represents all refs to a given tag, where the key is the tag the row(s) came from
type Refs map[Tag][]Row

func sqlAddRef(tx *sql.Tx, tagID int64, rowID int64) error {
	var statement *sql.Stmt
	var err error

	statement, err = tx.Prepare("INSERT INTO ref (tag_id, row_id) VALUES ($1, $2)")
	if err != nil {
		goto End
	}

	_, err = statement.Exec(tagID, rowID)
	if err != nil {
		goto End
	}

End:
	return err
}

func sqlClearRefsToRow(tx *sql.Tx, rowID int64) error {
	var statement *sql.Stmt
	var err error

	statement, err = tx.Prepare("DELETE FROM ref WHERE row = $1")
	if err != nil {
		goto End
	}

	_, err = statement.Exec(rowID)
	if err != nil {
		goto End
	}

End:
	return err
}

func sqlGetRefsToTagByTagID(tx *sql.Tx, tagID int64) (Refs, error) {
	var statement *sql.Stmt
	var sqlRows *sql.Rows
	var refs Refs
	var tag Tag
	var row Row
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

	sqlRows, err = statement.Query(tagID)
	if err != nil {
		goto End
	}
	defer sqlRows.Close()

	refs = make(Refs)
	for sqlRows.Next() {
		err = sqlRows.Scan(&row.id, &row.tagID, &row.parentRowID, &row.text, &row.rank, &row.updatedTS)
		if err != nil {
			goto End
		}

		tag, err = sqlGetTagByID(tx, row.tagID)
		if err != nil {
			goto End
		}

		refs[tag] = append(refs[tag], row)
	}

End:
	return refs, err
}

func sqlGetRefsToTagByTagName(tx *sql.Tx, name string) (Refs, error) {
	var refs Refs
	var tag Tag
	var err error

	tag, err = sqlGetTagByName(tx, name)
	if err != nil {
		goto End
	}

	refs, err = sqlGetRefsToTagByTagID(tx, tag.id)
	if err != nil {
		goto End
	}

End:
	return refs, err
}

func (e *ExoDB) GetRefsToTagByTagName(name string) (Refs, error) {
	var tx *sql.Tx
	var refs Refs
	var err error

	tx, err = e.conn.Begin()
	if err != nil {
		goto End
	}

	refs, err = sqlGetRefsToTagByTagName(tx, name)
	if err != nil {
		goto End
	}

End:
	sqlCommitOrRollback(tx, err)

	return refs, err
}

func (e *ExoDB) GetRefsToTagByTagID(tagID int64) (Refs, error) {
	var tx *sql.Tx
	var refs Refs
	var err error

	tx, err = e.conn.Begin()
	if err != nil {
		goto End
	}

	refs, err = sqlGetRefsToTagByTagID(tx, tagID)
	if err != nil {
		goto End
	}

End:
	sqlCommitOrRollback(tx, err)

	return refs, err
}
