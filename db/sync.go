package db

import (
	"crypto/rand"
	"database/sql"
	"fmt"
)

// Tombstone records a deletion event for LWW sync.
// Key is the row UUID for row tombstones, or the tag name for tag tombstones.
type Tombstone struct {
	Key       string `json:"key"`
	DeletedTS int64  `json:"deleted_ts"`
}

// SyncPayload is the wire format for bidirectional sync.
type SyncPayload struct {
	Since       int64       `json:"since"`
	ServerTS    int64       `json:"server_ts,omitempty"`
	Tags        []Tag       `json:"tags"`
	Rows        []Row       `json:"rows"`
	DeletedRows []Tombstone `json:"deleted_rows,omitempty"`
	DeletedTags []Tombstone `json:"deleted_tags,omitempty"`
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// BuildSyncPayload returns all tags and all rows/tombstones modified after since.
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
	if err != nil {
		sqlCommitOrRollback(tx, nil)
		return p, err
	}

	p.DeletedRows, err = sqlGetRowTombstonesSince(tx, since)
	if err != nil {
		sqlCommitOrRollback(tx, nil)
		return p, err
	}

	p.DeletedTags, err = sqlGetTagTombstonesSince(tx, since)
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

func sqlGetRowTombstonesSince(tx *sql.Tx, since int64) ([]Tombstone, error) {
	sqlRows, err := tx.Query("SELECT uuid, deleted_ts FROM row_tombstone WHERE deleted_ts > ?", since)
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()

	var tombstones []Tombstone
	for sqlRows.Next() {
		var t Tombstone
		if err = sqlRows.Scan(&t.Key, &t.DeletedTS); err != nil {
			return nil, err
		}
		tombstones = append(tombstones, t)
	}
	return tombstones, nil
}

func sqlGetTagTombstonesSince(tx *sql.Tx, since int64) ([]Tombstone, error) {
	sqlRows, err := tx.Query("SELECT uuid, deleted_ts FROM tag_tombstone WHERE deleted_ts > ?", since)
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()

	var tombstones []Tombstone
	for sqlRows.Next() {
		var t Tombstone
		if err = sqlRows.Scan(&t.Key, &t.DeletedTS); err != nil {
			return nil, err
		}
		tombstones = append(tombstones, t)
	}
	return tombstones, nil
}

func sqlAddRowTombstone(tx *sql.Tx, uuid string, ts int64) error {
	_, err := tx.Exec("INSERT OR REPLACE INTO row_tombstone (uuid, deleted_ts) VALUES (?, ?)", uuid, ts)
	return err
}

func sqlAddTagTombstone(tx *sql.Tx, uuid string, ts int64) error {
	_, err := tx.Exec("INSERT OR REPLACE INTO tag_tombstone (uuid, deleted_ts) VALUES (?, ?)", uuid, ts)
	return err
}

// sqlApplyRowTombstone records a remote row deletion locally and deletes the
// row if it is older than the tombstone (LWW).
func sqlApplyRowTombstone(tx *sql.Tx, t Tombstone) error {
	var existingTS int64
	err := tx.QueryRow("SELECT deleted_ts FROM row_tombstone WHERE uuid = ?", t.Key).Scan(&existingTS)
	if err == nil && existingTS >= t.DeletedTS {
		return nil // already have a newer or equal tombstone
	} else if err != nil && err != sql.ErrNoRows {
		return err
	}

	if _, err = tx.Exec("INSERT OR REPLACE INTO row_tombstone (uuid, deleted_ts) VALUES (?, ?)", t.Key, t.DeletedTS); err != nil {
		return err
	}

	var updatedTS int64
	err = tx.QueryRow("SELECT updated_ts FROM row WHERE uuid = ?", t.Key).Scan(&updatedTS)
	if err == sql.ErrNoRows {
		return nil // row not present locally; tombstone recorded for future reference
	}
	if err != nil {
		return err
	}
	if t.DeletedTS > updatedTS {
		_, err = tx.Exec("DELETE FROM row WHERE uuid = ?", t.Key)
	}
	return err
}

// sqlApplyTagTombstone records a remote tag deletion locally and deletes the
// tag (cascading to its rows) if it is older than the tombstone (LWW).
func sqlApplyTagTombstone(tx *sql.Tx, t Tombstone) error {
	var existingTS int64
	err := tx.QueryRow("SELECT deleted_ts FROM tag_tombstone WHERE uuid = ?", t.Key).Scan(&existingTS)
	if err == nil && existingTS >= t.DeletedTS {
		return nil
	} else if err != nil && err != sql.ErrNoRows {
		return err
	}

	if _, err = tx.Exec("INSERT OR REPLACE INTO tag_tombstone (uuid, deleted_ts) VALUES (?, ?)", t.Key, t.DeletedTS); err != nil {
		return err
	}

	var updatedTS int64
	err = tx.QueryRow("SELECT updated_ts FROM tag WHERE uuid = ?", t.Key).Scan(&updatedTS)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if t.DeletedTS > updatedTS {
		_, err = tx.Exec("DELETE FROM tag WHERE uuid = ?", t.Key)
	}
	return err
}

// ApplyChanges merges a remote SyncPayload into the local DB using
// last-write-wins (by updated_ts / deleted_ts). Tags are matched by name;
// rows by UUID. Tombstones are applied after upserts so a concurrent
// upsert+delete on the same object resolves correctly by timestamp.
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
		// localID == 0 means our tombstone wins; skip rows for this tag.
		if localID != 0 {
			tagIDMap[t.ID] = localID
		}
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

	for _, t := range p.DeletedRows {
		if applyErr := sqlApplyRowTombstone(tx, t); applyErr != nil {
			sqlCommitOrRollback(tx, applyErr)
			return applyErr
		}
	}

	for _, t := range p.DeletedTags {
		if applyErr := sqlApplyTagTombstone(tx, t); applyErr != nil {
			sqlCommitOrRollback(tx, applyErr)
			return applyErr
		}
	}

	sqlCommitOrRollback(tx, nil)
	return nil
}

// sqlUpsertTag creates or updates a tag by UUID (LWW on updated_ts).
// A newer remote name overwrites the local name, propagating renames.
// Returns the local tag ID, or 0 if a local tag tombstone supersedes the
// remote tag (caller should skip rows for that tag).
func sqlUpsertTag(tx *sql.Tx, t Tag) (int64, error) {
	var existing Tag
	err := tx.QueryRow("SELECT id, name, updated_ts FROM tag WHERE uuid = ?", t.UUID).
		Scan(&existing.ID, &existing.Name, &existing.UpdatedTS)
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	if err == nil {
		// Found by UUID; propagate rename if remote is newer.
		if t.UpdatedTS > existing.UpdatedTS {
			_, err = tx.Exec("UPDATE tag SET name = ?, updated_ts = ? WHERE id = ?", t.Name, t.UpdatedTS, existing.ID)
		}
		return existing.ID, err
	}

	// Not found by UUID. Fall back to name lookup — handles the case where the
	// same tag was created independently on two devices before their first sync.
	var nameMatch Tag
	nameErr := tx.QueryRow("SELECT id FROM tag WHERE name = ?", t.Name).Scan(&nameMatch.ID)
	if nameErr != nil && nameErr != sql.ErrNoRows {
		return 0, nameErr
	}
	if nameErr == nil {
		return nameMatch.ID, nil
	}

	// If we have a tombstone for this UUID newer than the incoming tag, our
	// deletion wins — don't resurrect a tag we've already deleted.
	var tombstoneTS int64
	tombErr := tx.QueryRow("SELECT deleted_ts FROM tag_tombstone WHERE uuid = ?", t.UUID).Scan(&tombstoneTS)
	if tombErr == nil && tombstoneTS >= t.UpdatedTS {
		return 0, nil
	}
	if tombErr != nil && tombErr != sql.ErrNoRows {
		return 0, tombErr
	}

	// Truly new tag: insert it.
	res, insertErr := tx.Exec("INSERT INTO tag (name, updated_ts, uuid) VALUES (?, ?, ?)", t.Name, t.UpdatedTS, t.UUID)
	if insertErr != nil {
		return 0, insertErr
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// sqlUpsertRow creates or updates a row by UUID (LWW on updated_ts).
// Returns the local row ID of the affected row, or 0 if no change was made.
func sqlUpsertRow(tx *sql.Tx, r Row) (int64, error) {
	// If we have a local tombstone newer than the incoming row, our deletion
	// wins — don't resurrect a row we've already deleted.
	var tombstoneTS int64
	tombErr := tx.QueryRow("SELECT deleted_ts FROM row_tombstone WHERE uuid = ?", r.UUID).Scan(&tombstoneTS)
	if tombErr == nil && tombstoneTS >= r.UpdatedTS {
		return 0, nil
	}

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
