package db

import (
	"testing"
)

// Tag tombstones must survive Migrate() (which runs on every startup).
func TestTagTombstonePersistsAcrossMigrate(t *testing.T) {
	db := setupDB(t)
	defer db.Close()

	tx, err := db.conn.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlAddTagTombstone(tx, "test-uuid-123", 99999); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	tx.Commit()

	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}

	var ts int64
	err = db.conn.QueryRow("SELECT deleted_ts FROM tag_tombstone WHERE uuid = ?", "test-uuid-123").Scan(&ts)
	if err != nil {
		t.Fatalf("tag tombstone lost across Migrate(): %v", err)
	}
	if ts != 99999 {
		t.Fatalf("wrong deleted_ts: got %d want 99999", ts)
	}
}

// A locally-tombstoned tag must not be resurrected by a remote sync that
// still has it (when the tombstone is at least as new as the remote tag).
func TestUpsertTagRespectsTombstone(t *testing.T) {
	db := setupDB(t)
	defer db.Close()

	tx, _ := db.conn.Begin()
	if err := sqlAddTagTombstone(tx, "ghost-uuid", 1000); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	tx.Commit()

	remote := SyncPayload{
		Tags: []Tag{{ID: 1, Name: "ghost", UpdatedTS: 500, UUID: "ghost-uuid"}},
		Rows: []Row{{TagID: 1, Text: "should-not-appear", UpdatedTS: 500, UUID: "row-uuid"}},
	}
	if err := db.ApplyChanges(remote); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := db.conn.QueryRow("SELECT COUNT(*) FROM tag WHERE uuid = ?", "ghost-uuid").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n > 0 {
		t.Fatalf("locally-tombstoned tag was resurrected by remote upsert (n=%d)", n)
	}
	// And rows belonging to that tag must be skipped, not orphaned with TagID=0.
	if err := db.conn.QueryRow("SELECT COUNT(*) FROM row WHERE uuid = ?", "row-uuid").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n > 0 {
		t.Fatalf("row for tombstoned tag was inserted (n=%d)", n)
	}
}

// A remote tag *newer* than the local tombstone wins (LWW); the tag should be
// recreated. Confirms that tombstone-respect is timestamp-gated, not absolute.
func TestUpsertTagRemoteNewerThanTombstoneWins(t *testing.T) {
	db := setupDB(t)
	defer db.Close()

	tx, _ := db.conn.Begin()
	if err := sqlAddTagTombstone(tx, "phoenix-uuid", 500); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	tx.Commit()

	remote := SyncPayload{
		Tags: []Tag{{ID: 1, Name: "phoenix", UpdatedTS: 1000, UUID: "phoenix-uuid"}},
	}
	if err := db.ApplyChanges(remote); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := db.conn.QueryRow("SELECT COUNT(*) FROM tag WHERE uuid = ?", "phoenix-uuid").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("remote tag newer than tombstone should win: got n=%d, want 1", n)
	}
}
