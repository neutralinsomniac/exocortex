package db

import (
	"database/sql"
	"regexp"
	"time"
)

type Row struct {
	ID          int64
	TagID       int64
	Rank        int
	Text        string
	ParentRowID int64
	UpdatedTS   int64
}

func sqlGetRowByID(tx *sql.Tx, id int64) (Row, error) {
	var row Row
	var sqlRow *sql.Row
	var err error

	sqlRow = tx.QueryRow("SELECT id, tag_id, text, rank, parent_row_id, updated_ts FROM row WHERE id = $1", id)

	err = sqlRow.Scan(&row.ID, &row.TagID, &row.Text, &row.Rank, &row.ParentRowID, &row.UpdatedTS)
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

func sqlDeleteRowByID(tx *sql.Tx, id int64) error {
	var statement *sql.Stmt
	var err error

	statement, err = tx.Prepare("DELETE FROM row WHERE id=?")
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

func (e *ExoDB) DeleteRowByID(id int64) error {
	var tx *sql.Tx
	var err error

	tx, err = e.conn.Begin()
	if err != nil {
		goto End
	}

	err = sqlDeleteRowByID(tx, id)
	if err != nil {
		goto End
	}

End:
	sqlCommitOrRollback(tx, err)
	return err
}

func sqlGetRowsForTagID(tx *sql.Tx, tagID int64) ([]Row, error) {
	var rows []Row
	var sqlRows *sql.Rows
	var err error

	sqlRows, err = tx.Query("SELECT id, tag_id, rank, text, parent_row_id, updated_ts FROM row WHERE tag_id = $1 ORDER BY rank, id", tagID)
	if err != nil {
		goto End
	}
	defer sqlRows.Close()

	for sqlRows.Next() {
		var row Row
		err = sqlRows.Scan(&row.ID, &row.TagID, &row.Rank, &row.Text, &row.ParentRowID, &row.UpdatedTS)
		if err != nil {
			goto End
		}
		rows = append(rows, row)
	}

End:
	return rows, err
}

func (e *ExoDB) GetRowsForTagID(tagID int64) ([]Row, error) {
	var tx *sql.Tx
	var rows []Row
	var err error

	tx, err = e.conn.Begin()
	if err != nil {
		goto End
	}

	rows, err = sqlGetRowsForTagID(tx, tagID)
	if err != nil {
		goto End
	}

End:
	sqlCommitOrRollback(tx, err)
	return rows, err
}

func sqlAddRow(tx *sql.Tx, tagID int64, text string, parentRowID int64, rank int) (int64, error) {
	var statement *sql.Stmt
	var res sql.Result
	var rowID int64
	var err error

	statement, err = tx.Prepare("INSERT INTO row (tag_id, text, parent_row_id, rank, updated_ts) VALUES ($1, $2, $3, $4, $5)")
	if err != nil {
		goto End
	}

	res, err = statement.Exec(tagID, text, parentRowID, rank, time.Now().UnixNano())
	if err != nil {
		goto End
	}

	rowID, err = res.LastInsertId()
	if err != nil {
		goto End
	}

	err = sqlUpdateTagTS(tx, tagID)
	if err != nil {
		goto End
	}

End:
	return rowID, err
}

func (e *ExoDB) AddRow(tagID int64, text string, parentRowID int64) (Row, error) {
	var tx *sql.Tx
	var sqlRow *sql.Row
	var row Row
	var rank int
	var rowID int64
	var err error

	tx, err = e.conn.Begin()
	if err != nil {
		goto End
	}

	// first get the max rank for this tag
	sqlRow = tx.QueryRow("SELECT MAX(rank) FROM row WHERE tag_id = $1", tagID)

	err = sqlRow.Scan(&rank)
	if err == nil {
		rank++
	}

	rowID, err = sqlAddRow(tx, tagID, text, parentRowID, rank)
	if err != nil {
		goto End
	}

	err = sqlUpdateRefsForRowID(tx, rowID)
	if err != nil {
		goto End
	}

	row, err = sqlGetRowByID(tx, rowID)
	if err != nil {
		goto End
	}

End:
	sqlCommitOrRollback(tx, err)
	return row, err
}

func sqlUpdateRowText(tx *sql.Tx, rowID int64, text string) error {
	var statement *sql.Stmt
	var row Row
	var err error

	statement, err = tx.Prepare("UPDATE row SET text = ? WHERE id = ?")
	if err != nil {
		goto End
	}

	_, err = statement.Exec(text, rowID)
	if err != nil {
		goto End
	}

	row, err = sqlGetRowByID(tx, rowID)
	if err != nil {
		goto End
	}

	err = sqlUpdateTagTS(tx, row.TagID)
	if err != nil {
		goto End
	}
End:
	return err
}

func sqlUpdateRefsForRowID(tx *sql.Tx, rowID int64) error {
	var tagID int64
	var row Row
	var newTags [][]string
	var re *regexp.Regexp
	var err error

	// update all old refs to this row
	// first remove all old refs
	err = sqlClearRefsToRow(tx, rowID)
	if err != nil {
		goto End
	}

	row, err = sqlGetRowByID(tx, rowID)
	if err != nil {
		goto End
	}

	// now find new refs and create them
	re = regexp.MustCompile(`\[\[(.*?)\]\]`)
	newTags = re.FindAllStringSubmatch(row.Text, -1)

	for _, newTag := range newTags {
		tagID, err = sqlAddTag(tx, newTag[1])
		if err != nil {
			goto End
		}
		sqlAddRef(tx, tagID, rowID)
	}

End:
	return err

}

func (e *ExoDB) UpdateRowText(rowID int64, text string) error {
	var tx *sql.Tx
	var err error

	tx, err = e.conn.Begin()
	if err != nil {
		goto End
	}

	err = sqlUpdateRowText(tx, rowID, text)
	if err != nil {
		goto End
	}

	err = sqlUpdateRefsForRowID(tx, rowID)
	if err != nil {
		goto End
	}

End:
	sqlCommitOrRollback(tx, err)
	return err
}

func (e *ExoDB) sqlUpdateRowRank(tx *sql.Tx, rowID int64, rank int) error {
	var statement *sql.Stmt
	var row Row
	var err error

	statement, err = tx.Prepare("UPDATE row SET rank = ? WHERE id = ?")
	if err != nil {
		goto End
	}

	_, err = statement.Exec(rank, rowID)
	if err != nil {
		goto End
	}

	row, err = sqlGetRowByID(tx, rowID)
	if err != nil {
		goto End
	}

	err = sqlUpdateTagTS(tx, row.TagID)
	if err != nil {
		goto End
	}
End:
	return err
}

func (e *ExoDB) UpdateRowRank(rowID int64, rank int) error {
	var tx *sql.Tx
	var row Row
	var rows []Row
	var newRank int
	var err error

	tx, err = e.conn.Begin()
	if err != nil {
		goto End
	}

	// get the tag for this row
	row, err = sqlGetRowByID(tx, rowID)
	if err != nil {
		goto End
	}

	// now get all rows for this tag
	rows, err = sqlGetRowsForTagID(tx, row.TagID)
	if err != nil {
		goto End
	}

	if rank >= len(rows) {
		goto End
	}

	newRank = 0
	for _, row := range rows {
		if row.ID == rowID {
			// this is our row; set the rank explicitly
			err = e.sqlUpdateRowRank(tx, row.ID, rank)
			if err != nil {
				goto End
			}
		} else if newRank >= rank {
			// this is not our row, and it's "below" the rank we want, so push it down
			err = e.sqlUpdateRowRank(tx, row.ID, newRank+1)
			newRank++
		} else {
			// normal ranking
			err = e.sqlUpdateRowRank(tx, row.ID, newRank)
			newRank++
		}

	}

End:
	sqlCommitOrRollback(tx, err)
	return err
}
