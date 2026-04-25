package db

import "testing"

// When the TUI inserts a new priority-0 row while the cursor is on a priority
// row, it targets rank 0 — the new row should sort to the top of the
// priority-0 section. Priority-1..5 rows still sort above it regardless of
// rank. This test pins the underlying DB behaviour the fix relies on.
func TestNewRowAtRankZeroSortsToTopOfPriorityZero(t *testing.T) {
	db := setupDB(t)
	defer db.Close()

	tag, err := db.AddTag("t")
	if err != nil {
		t.Fatal(err)
	}

	// Build: P0@0, P0@1, P0@2 — then promote the last to priority 1.
	r0, _ := db.AddRow(tag.ID, "a", 0)
	r1, _ := db.AddRow(tag.ID, "b", 0)
	r2, _ := db.AddRow(tag.ID, "c", 0)
	if err := db.UpdateRowPriority(r2.ID, 1); err != nil {
		t.Fatal(err)
	}

	newRow, err := db.AddRow(tag.ID, "NEW", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateRowRank(newRow.ID, 0); err != nil {
		t.Fatal(err)
	}

	rows, _ := db.GetRowsForTagID(tag.ID)
	var minP0 *Row
	for i := range rows {
		if rows[i].Priority != 0 {
			continue
		}
		if minP0 == nil || rows[i].Rank < minP0.Rank {
			minP0 = &rows[i]
		}
	}
	if minP0 == nil {
		t.Fatal("no priority-0 rows after insert")
	}
	if minP0.ID != newRow.ID {
		t.Fatalf("NEW (id=%d) is not the first priority-0 row (got id=%d at rank %d)",
			newRow.ID, minP0.ID, minP0.Rank)
	}

	for _, id := range []int64{r0.ID, r1.ID} {
		var found *Row
		for i := range rows {
			if rows[i].ID == id {
				found = &rows[i]
				break
			}
		}
		if found == nil {
			t.Fatalf("row id=%d missing", id)
		}
		if found.Priority != 0 {
			t.Fatalf("row id=%d should still be priority 0", id)
		}
		if found.Rank <= minP0.Rank {
			t.Fatalf("priority-0 row id=%d (rank %d) should rank below NEW (rank %d)",
				id, found.Rank, minP0.Rank)
		}
	}
}

// Regression: stored ranks can be non-contiguous (DeleteRowByID leaves gaps),
// so targeting the first priority-0 row's RANK can collide with
// UpdateRowRank's `if rank >= len(rows)` bail-out, leaving the new row at
// MAX(rank)+1 — the BOTTOM of priority-0. Targeting rank 0 sidesteps this.
func TestNewRowAtRankZeroHandlesNonContiguousRanks(t *testing.T) {
	db := setupDB(t)
	defer db.Close()

	tag, err := db.AddTag("t")
	if err != nil {
		t.Fatal(err)
	}

	// Add a row at rank 0 then delete it, leaving a gap.
	gap, _ := db.AddRow(tag.ID, "gap", 0)
	a, _ := db.AddRow(tag.ID, "a", 0) // rank 1
	b, _ := db.AddRow(tag.ID, "b", 0) // rank 2
	c, _ := db.AddRow(tag.ID, "c", 0) // rank 3
	if err := db.DeleteRowByID(gap.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateRowPriority(a.ID, 1); err != nil {
		t.Fatal(err)
	}

	// State: a(rank=1, p=1), b(rank=2, p=0), c(rank=3, p=0). len(rows)=3.
	newRow, err := db.AddRow(tag.ID, "NEW", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateRowRank(newRow.ID, 0); err != nil {
		t.Fatal(err)
	}

	rows, _ := db.GetRowsForTagID(tag.ID)
	var minP0 *Row
	for i := range rows {
		if rows[i].Priority != 0 {
			continue
		}
		if minP0 == nil || rows[i].Rank < minP0.Rank {
			minP0 = &rows[i]
		}
	}
	if minP0 == nil {
		t.Fatal("no priority-0 rows after insert")
	}
	if minP0.ID != newRow.ID {
		t.Fatalf("NEW (id=%d) is not the first priority-0 row (got id=%d at rank %d)",
			newRow.ID, minP0.ID, minP0.Rank)
	}
	for _, id := range []int64{b.ID, c.ID} {
		var found *Row
		for i := range rows {
			if rows[i].ID == id {
				found = &rows[i]
				break
			}
		}
		if found == nil {
			t.Fatalf("row id=%d missing", id)
		}
		if found.Rank <= minP0.Rank {
			t.Fatalf("priority-0 row id=%d (rank %d) should rank below NEW (rank %d)",
				id, found.Rank, minP0.Rank)
		}
	}
}
