package db

import (
	"crypto/rand"
	"database/sql"
	"fmt"
)

// SyncPayload is the wire format for bidirectional sync.
type SyncPayload struct {
	Since    int64  `json:"since"`
	ServerTS int64  `json:"server_ts,omitempty"`
	Tags     []Tag  `json:"tags"`
	Rows     []Row  `json:"rows"`
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// BuildSyncPayload returns all tags and all rows modified after since.
// All tags are always included (not filtered by since) so the receiver can
// remap remote tag IDs to local ones.
func (e *ExoDB) BuildSyncPayload(since int64) (SyncPayload, error) {
	var p SyncPayload
	p.Since = since

	tx, err := e.conn.Begin()
	if err != nil {
		return p, err
	}

	p.Tags, err = sqlGetAllTags(tx)
	if err != nil {
		sqlCommitOrRollback(tx, err)
		return p, err
	}

	p.Rows, err = sqlGetRowsSince(tx, since)
	sqlCommitOrRollback(tx, nil)
	return p, err
}

func sqlGetRowsSince(tx *sql.Tx, since int64) ([]Row, error) {
	sqlRows, err := tx.Query(
		"SELECT id, tag_id, rank, text, parent_row_id, updated_ts, note, priority, done, uuid FROM row WHERE updated_ts > ?",
		since,
	)
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()

	var rows []Row
	for sqlRows.Next() {
		var r Row
		if err = sqlRows.Scan(&r.ID, &r.TagID, &r.Rank, &r.Text, &r.ParentRowID, &r.UpdatedTS, &r.Note, &r.Priority, &r.Done, &r.UUID); err != nil {
			return nil, err
		}
		rows = append(rows, r)
	}
	return rows, nil
}

// ApplyChanges merges a remote SyncPayload into the local DB using
// last-write-wins (by updated_ts). Tags are matched by name; rows by UUID.
func (e *ExoDB) ApplyChanges(p SyncPayload) error {
	tx, err := e.conn.Begin()
	if err != nil {
		return err
	}

	// Build remote tag ID → local tag ID mapping.
	tagIDMap := make(map[int64]int64, len(p.Tags))
	for _, t := range p.Tags {
		localID, upsertErr := sqlUpsertTag(tx, t)
		if upsertErr != nil {
			sqlCommitOrRollback(tx, upsertErr)
			return upsertErr
		}
		tagIDMap[t.ID] = localID
	}

	for _, r := range p.Rows {
		if r.UUID == "" {
			continue
		}
		localTagID, ok := tagIDMap[r.TagID]
		if !ok {
			continue
		}
		r.TagID = localTagID
		rowID, upsertErr := sqlUpsertRow(tx, r)
		if upsertErr != nil {
			sqlCommitOrRollback(tx, upsertErr)
			return upsertErr
		}
		if rowID != 0 {
			if refErr := sqlUpdateRefsForRowID(tx, rowID); refErr != nil {
				sqlCommitOrRollback(tx, refErr)
				return refErr
			}
		}
	}

	sqlCommitOrRollback(tx, nil)
	return nil
}

// sqlUpsertTag creates or updates a tag by name (LWW on updated_ts).
// Returns the local tag ID.
func sqlUpsertTag(tx *sql.Tx, t Tag) (int64, error) {
	var existing Tag
	err := tx.QueryRow("SELECT id, name, updated_ts FROM tag WHERE name = ?", t.Name).
		Scan(&existing.ID, &existing.Name, &existing.UpdatedTS)
	if err == sql.ErrNoRows {
		res, insertErr := tx.Exec("INSERT INTO tag (name, updated_ts) VALUES (?, ?)", t.Name, t.UpdatedTS)
		if insertErr != nil {
			return 0, insertErr
		}
		id, _ := res.LastInsertId()
		return id, nil
	}
	if err != nil {
		return 0, err
	}
	if t.UpdatedTS > existing.UpdatedTS {
		_, err = tx.Exec("UPDATE tag SET updated_ts = ? WHERE id = ?", t.UpdatedTS, existing.ID)
	}
	return existing.ID, err
}

// sqlUpsertRow creates or updates a row by UUID (LWW on updated_ts).
// Returns the local row ID of the affected row, or 0 if no change was made.
func sqlUpsertRow(tx *sql.Tx, r Row) (int64, error) {
	var localID, localUpdatedTS int64
	err := tx.QueryRow("SELECT id, updated_ts FROM row WHERE uuid = ?", r.UUID).
		Scan(&localID, &localUpdatedTS)
	if err == sql.ErrNoRows {
		res, insertErr := tx.Exec(
			"INSERT INTO row (tag_id, text, parent_row_id, rank, updated_ts, note, priority, done, uuid) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			r.TagID, r.Text, 0, r.Rank, r.UpdatedTS, r.Note, r.Priority, r.Done, r.UUID,
		)
		if insertErr != nil {
			return 0, insertErr
		}
		id, _ := res.LastInsertId()
		return id, nil
	}
	if err != nil {
		return 0, err
	}
	if r.UpdatedTS <= localUpdatedTS {
		return 0, nil // local is newer or same; skip
	}
	_, err = tx.Exec(
		"UPDATE row SET tag_id=?, text=?, rank=?, updated_ts=?, note=?, priority=?, done=? WHERE id=?",
		r.TagID, r.Text, r.Rank, r.UpdatedTS, r.Note, r.Priority, r.Done, localID,
	)
	if err != nil {
		return 0, err
	}
	return localID, nil
}
