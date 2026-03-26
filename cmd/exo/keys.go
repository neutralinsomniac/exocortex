package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/neutralinsomniac/exocortex/db"
)

// ── editor ───────────────────────────────────────────────────────────────────

func openEditorCmd(initialText string, action pendingAction, row db.Row) tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	f, err := os.CreateTemp("", "exo*.txt")
	if err != nil {
		return func() tea.Msg { return editorDoneMsg{err: err, action: action, row: row} }
	}
	fname := f.Name()
	f.WriteString(initialText)
	f.Close()
	return tea.ExecProcess(exec.Command(editor, fname), func(err error) tea.Msg {
		defer os.Remove(fname)
		if err != nil {
			return editorDoneMsg{err: err, action: action, row: row}
		}
		b, readErr := os.ReadFile(fname)
		if readErr != nil {
			return editorDoneMsg{err: readErr, action: action, row: row}
		}
		return editorDoneMsg{text: strings.TrimSpace(string(b)), action: action, row: row}
	})
}

// ── key handlers ─────────────────────────────────────────────────────────────

func (m *model) handleEditorDone(msg editorDoneMsg) tea.Cmd {
	if msg.err != nil {
		m.setErr("editor error: " + msg.err.Error())
		return nil
	}
	if msg.text == "" && msg.action != actionEditNote {
		m.setErr("empty input")
		return nil
	}
	switch msg.action {
	case actionAddRow:
		row, err := m.dbState.AddRow(m.dbState.CurrentDBTag.ID, msg.text, 0)
		if err != nil {
			m.setErr(err.Error())
			return nil
		}
		rank := m.pendingRank
		m.makeRoomForRank(0, rank)
		_ = m.dbState.UpdateRowRank(row.ID, rank)
		tagID, tagName, text := m.dbState.CurrentDBTag.ID, m.dbState.CurrentDBTag.Name, msg.text
		var latestID int64 = row.ID
		m.pushUndo(undoEntry{
			desc:    "add row",
			tagID:   tagID,
			tagName: tagName,
			undoFn: func(exoDB *db.ExoDB) (int64, error) {
				return 0, exoDB.DeleteRowByID(latestID)
			},
			redoFn: func(exoDB *db.ExoDB) (int64, error) {
				t, err := exoDB.AddTag(tagName)
				if err != nil {
					return 0, err
				}
				newRow, err := exoDB.AddRow(t.ID, text, 0)
				if err != nil {
					return 0, err
				}
				latestID = newRow.ID
				return latestID, exoDB.UpdateRowRank(latestID, rank)
			},
		})
		m.status = ""
		m.refresh()
		m.positionCursor(row.ID)
	case actionInsertRow:
		row, err := m.dbState.AddRow(m.dbState.CurrentDBTag.ID, msg.text, 0)
		if err != nil {
			m.setErr(err.Error())
			return nil
		}
		rank := m.pendingRank
		m.makeRoomForRank(0, rank)
		_ = m.dbState.UpdateRowRank(row.ID, rank)
		tagID, tagName, text := m.dbState.CurrentDBTag.ID, m.dbState.CurrentDBTag.Name, msg.text
		var latestID int64 = row.ID
		m.pushUndo(undoEntry{
			desc:    "insert row",
			tagID:   tagID,
			tagName: tagName,
			undoFn: func(exoDB *db.ExoDB) (int64, error) {
				return 0, exoDB.DeleteRowByID(latestID)
			},
			redoFn: func(exoDB *db.ExoDB) (int64, error) {
				t, err := exoDB.AddTag(tagName)
				if err != nil {
					return 0, err
				}
				newRow, err := exoDB.AddRow(t.ID, text, 0)
				if err != nil {
					return 0, err
				}
				latestID = newRow.ID
				return latestID, exoDB.UpdateRowRank(latestID, rank)
			},
		})
		m.status = ""
		m.refresh()
		m.positionCursor(row.ID)
	case actionEditNote:
		oldNote, newNote, rowID := msg.row.Note, msg.text, msg.row.ID
		if err := m.dbState.UpdateRowNote(rowID, newNote); err != nil {
			m.setErr(err.Error())
			return nil
		}
		tagID, tagName := m.dbState.CurrentDBTag.ID, m.dbState.CurrentDBTag.Name
		m.pushUndo(undoEntry{
			desc:    "edit note",
			tagID:   tagID,
			tagName: tagName,
			undoFn: func(exoDB *db.ExoDB) (int64, error) {
				return rowID, exoDB.UpdateRowNote(rowID, oldNote)
			},
			redoFn: func(exoDB *db.ExoDB) (int64, error) {
				return rowID, exoDB.UpdateRowNote(rowID, newNote)
			},
		})
		m.status = ""
		m.refresh()
	case actionEditRow:
		oldText, newText, rowID := msg.row.Text, msg.text, msg.row.ID
		if err := m.dbState.UpdateRowText(rowID, newText); err != nil {
			m.setErr(err.Error())
			return nil
		}
		tagID, tagName := m.dbState.CurrentDBTag.ID, m.dbState.CurrentDBTag.Name
		m.pushUndo(undoEntry{
			desc:    "edit row",
			tagID:   tagID,
			tagName: tagName,
			undoFn: func(exoDB *db.ExoDB) (int64, error) {
				return rowID, exoDB.UpdateRowText(rowID, oldText)
			},
			redoFn: func(exoDB *db.ExoDB) (int64, error) {
				return rowID, exoDB.UpdateRowText(rowID, newText)
			},
		})
		m.status = ""
		m.refresh()
	case actionNewTag:
		prevTag := m.dbState.CurrentDBTag
		newTagName := msg.text
		isNew := !m.allTagNames[newTagName]
		tag, err := m.dbState.AddTag(newTagName)
		if err != nil {
			m.setErr(err.Error())
			return nil
		}
		if isNew {
			newTagID := tag.ID
			m.pushUndo(undoEntry{
				desc:    "new tag",
				tagID:   prevTag.ID,
				tagName: prevTag.Name,
				undoFn: func(exoDB *db.ExoDB) (int64, error) {
					return 0, exoDB.DeleteTagByID(newTagID)
				},
				redoFn: func(exoDB *db.ExoDB) (int64, error) {
					_, err := exoDB.AddTag(newTagName)
					return 0, err
				},
			})
		}
		m.status = ""
		m.switchTag(tag)
	case actionRenameTag:
		oldName := m.dbState.CurrentDBTag.Name
		newName := msg.text
		tag, err := m.dbState.RenameTag(oldName, newName)
		if err != nil {
			m.setErr(err.Error())
			return nil
		}
		m.pushUndo(undoEntry{
			desc:  "rename tag",
			tagID: tag.ID,
			undoFn: func(exoDB *db.ExoDB) (int64, error) {
				_, err := exoDB.RenameTag(newName, oldName)
				return 0, err
			},
			redoFn: func(exoDB *db.ExoDB) (int64, error) {
				_, err := exoDB.RenameTag(oldName, newName)
				return 0, err
			},
		})
		m.status = ""
		m.switchTag(tag)
	}
	return nil
}

func (m *model) handleMainKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c", "q":
		_ = m.dbState.SetSetting("last_tag", m.dbState.CurrentDBTag.Name)
		_ = m.dbState.DeleteTagIfEmpty(m.dbState.CurrentDBTag.ID)
		return tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.clampLineOffset()
		}
	case "down", "j":
		if m.cursor < len(m.rowItems)-1 {
			m.cursor++
			m.clampLineOffset()
		}
	case "g":
		m.cursor = 0
		m.clampLineOffset()
	case "G":
		m.cursor = len(m.rowItems) - 1
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.clampLineOffset()
	case "pgup", "ctrl+b":
		m.pageCursor(-1)
	case "pgdown", "ctrl+f":
		m.pageCursor(+1)

	case "S":
		serverURL, _ := m.dbState.GetSetting("sync_url")
		if serverURL == "" {
			m.setErr("sync_url setting not configured")
			break
		}
		token, _ := m.dbState.GetSetting("sync_token")
		encrypt := !strings.HasPrefix(serverURL, "https://")
		m.setStatus("syncing...")
		exoDB := m.dbState.ExoDB
		return func() tea.Msg {
			err := exoDB.SyncWith(serverURL, token, encrypt)
			return syncResultMsg{err: err}
		}

	case "ctrl+s":
		serverURL, _ := m.dbState.GetSetting("sync_url")
		if serverURL == "" {
			m.setErr("sync_url setting not configured")
			break
		}
		token, _ := m.dbState.GetSetting("sync_token")
		encrypt := !strings.HasPrefix(serverURL, "https://")
		m.setStatus("syncing (full)...")
		exoDB := m.dbState.ExoDB
		return func() tea.Msg {
			err := exoDB.ForceFullSyncWith(serverURL, token, encrypt)
			return syncResultMsg{err: err}
		}

	case "u":
		m.applyUndo()

	case "U":
		m.applyRedo()

	case "K": // move selected row up within its priority group
		if len(m.rowItems) == 0 || m.cursor == 0 || m.rowItems[m.cursor].isRef {
			break
		}
		row := m.rowItems[m.cursor].row
		// find the nearest non-ref item above with the same priority
		prev := -1
		for i := m.cursor - 1; i >= 0; i-- {
			if !m.rowItems[i].isRef && m.rowItems[i].row.Priority == row.Priority {
				prev = i
				break
			}
		}
		if prev == -1 {
			break
		}
		rowID, fromRank, toRank := row.ID, row.Rank, m.rowItems[prev].row.Rank
		_ = m.dbState.UpdateRowRank(rowID, toRank)
		m.pushUndo(undoEntry{
			desc:    "move row up",
			tagID:   m.dbState.CurrentDBTag.ID,
			tagName: m.dbState.CurrentDBTag.Name,
			undoFn: func(exoDB *db.ExoDB) (int64, error) {
				return rowID, exoDB.UpdateRowRank(rowID, fromRank)
			},
			redoFn: func(exoDB *db.ExoDB) (int64, error) {
				return rowID, exoDB.UpdateRowRank(rowID, toRank)
			},
		})
		m.cursor--
		m.refresh()
		m.clampLineOffset()

	case "J": // move selected row down within its priority group
		if len(m.rowItems) == 0 || m.cursor >= len(m.rowItems)-1 || m.rowItems[m.cursor].isRef {
			break
		}
		row := m.rowItems[m.cursor].row
		// find the nearest non-ref item below with the same priority
		next := -1
		for i := m.cursor + 1; i < len(m.rowItems); i++ {
			if !m.rowItems[i].isRef && m.rowItems[i].row.Priority == row.Priority {
				next = i
				break
			}
		}
		if next == -1 {
			break
		}
		rowID, fromRank, toRank := row.ID, row.Rank, m.rowItems[next].row.Rank
		_ = m.dbState.UpdateRowRank(rowID, toRank)
		m.pushUndo(undoEntry{
			desc:    "move row down",
			tagID:   m.dbState.CurrentDBTag.ID,
			tagName: m.dbState.CurrentDBTag.Name,
			undoFn: func(exoDB *db.ExoDB) (int64, error) {
				return rowID, exoDB.UpdateRowRank(rowID, fromRank)
			},
			redoFn: func(exoDB *db.ExoDB) (int64, error) {
				return rowID, exoDB.UpdateRowRank(rowID, toRank)
			},
		})
		m.cursor++
		m.refresh()
		m.clampLineOffset()

	case ";":
		m.selectedRows = make(map[int64]bool)

	case " ":
		if len(m.rowItems) == 0 || m.cursor >= len(m.rowItems) {
			break
		}
		item := m.rowItems[m.cursor]
		if item.isRef {
			break
		}
		id := item.row.ID
		if m.selectedRows[id] {
			delete(m.selectedRows, id)
		} else {
			m.selectedRows[id] = true
		}
		if m.cursor < len(m.rowItems)-1 && !m.rowItems[m.cursor+1].isRef {
			m.cursor++
		}
		m.clampLineOffset()

	case "!", "@", "#", "$", "%", ")":
		if len(m.rowItems) == 0 || m.cursor >= len(m.rowItems) || m.rowItems[m.cursor].isRef {
			break
		}
		priorityMap := map[string]int{"!": 1, "@": 2, "#": 3, "$": 4, "%": 5, ")": 0}
		newPriority := priorityMap[msg.String()]
		selSnap := m.snapshotSelection()
		targets := m.collectTargets()
		type priorChange struct {
			id         int64
			oldP, newP int
		}
		var changes []priorChange
		for _, it := range targets {
			old := it.row.Priority
			p := newPriority
			if newPriority != 0 && old == newPriority {
				p = 0 // toggle off
			}
			if err := m.dbState.UpdateRowPriority(it.row.ID, p); err != nil {
				m.setErr(err.Error())
				break
			}
			changes = append(changes, priorChange{it.row.ID, old, p})
		}
		if len(changes) == 0 {
			break
		}
		tagID, tagName := m.dbState.CurrentDBTag.ID, m.dbState.CurrentDBTag.Name
		m.pushUndo(undoEntry{
			desc:              fmt.Sprintf("set priority on %d row(s)", len(changes)),
			tagID:             tagID,
			tagName:           tagName,
			postUndoSelection: selSnap,
			undoFn: func(exoDB *db.ExoDB) (int64, error) {
				for _, c := range changes {
					if err := exoDB.UpdateRowPriority(c.id, c.oldP); err != nil {
						return 0, err
					}
				}
				return changes[0].id, nil
			},
			redoFn: func(exoDB *db.ExoDB) (int64, error) {
				for _, c := range changes {
					if err := exoDB.UpdateRowPriority(c.id, c.newP); err != nil {
						return 0, err
					}
				}
				return changes[0].id, nil
			},
		})
		cursorID := m.rowItems[m.cursor].row.ID
		m.selectedRows = make(map[int64]bool)
		m.refresh()
		m.positionCursor(cursorID)

	case "i":
		m.status = ""
		m.goToInbox()

	case "ctrl+t", "b":
		m.popTag()

	case "enter":
		if len(m.rowItems) == 0 || m.cursor >= len(m.rowItems) {
			break
		}
		item := m.rowItems[m.cursor]
		if item.isRef {
			rowID := item.row.ID
			m.status = ""
			m.switchTag(item.refTag)
			for i, ri := range m.rowItems {
				if ri.row.ID == rowID {
					m.cursor = i
					m.clampLineOffset()
					break
				}
			}
			break
		}
		matches := tagRe.FindAllStringSubmatch(item.row.Text, -1)
		switch len(matches) {
		case 0:
			// no-op
		case 1:
			tag, err := m.dbState.GetTagByName(matches[0][1])
			if err == nil && tag.ID != 0 {
				m.status = ""
				m.switchTag(tag)
			}
		default:
			m.setStatus("multiple tags in row — use number shortcuts to navigate")
		}

	case "o":
		m.mode = modeInput
		m.pendingAction = actionAddRow
		m.pendingRank = m.afterCursorRank()
		m.setInputWidth()
		m.textInput.SetValue("")
		m.textInput.Placeholder = "row text (empty = open $EDITOR)"
		m.textInput.Focus()
		return textinput.Blink

	case "O":
		m.mode = modeInput
		m.pendingAction = actionInsertRow
		if m.cursor < len(m.rowItems) && !m.rowItems[m.cursor].isRef {
			m.pendingRank = m.rowItems[m.cursor].row.Rank
		} else {
			m.pendingRank = 0
		}
		m.setInputWidth()
		m.textInput.SetValue("")
		m.textInput.Placeholder = "row text (empty = open $EDITOR)"
		m.textInput.Focus()
		return textinput.Blink

	case "e":
		if len(m.rowItems) == 0 {
			m.setErr("no row selected")
			break
		}
		m.pendingRow = m.rowItems[m.cursor].row
		m.mode = modeInput
		m.pendingAction = actionEditRow
		m.setInputWidth()
		m.textInput.SetValue(m.pendingRow.Text)
		m.textInput.CursorEnd()
		m.textInput.Placeholder = "edit text (empty = open $EDITOR)"
		m.textInput.Focus()
		return textinput.Blink

	case "N":
		if len(m.rowItems) == 0 || m.cursor >= len(m.rowItems) {
			break
		}
		item := m.rowItems[m.cursor]
		if item.isRef {
			break
		}
		return openEditorCmd(item.row.Note, actionEditNote, item.row)

	case "d":
		if len(m.rowItems) == 0 {
			m.setErr("no row selected")
			break
		}
		selSnap := m.snapshotSelection()
		targets := m.collectTargets()
		// Find the first non-deleted row at or after the cursor to land on.
		deletedIDs := make(map[int64]bool, len(targets))
		for _, t := range targets {
			deletedIDs[t.row.ID] = true
		}
		var landRowID int64
		for i := m.cursor; i < len(m.rowItems); i++ {
			it := m.rowItems[i]
			if !it.isRef && !deletedIDs[it.row.ID] {
				landRowID = it.row.ID
				break
			}
		}
		// Save info needed for undo before deleting.
		type savedRow struct {
			tagID    int64
			tagName  string
			text     string
			note     string
			rank     int
			priority int
		}
		var saved []savedRow
		for _, it := range targets {
			tagID, tagName := m.rowTagContext(it)
			saved = append(saved, savedRow{tagID, tagName, it.row.Text, it.row.Note, it.row.Rank, it.row.Priority})
			if err := m.dbState.DeleteRowByID(it.row.ID); err != nil {
				m.setErr(err.Error())
				break
			}
		}
		m.snarfedRows = nil
		for _, t := range targets {
			m.snarfedRows = append(m.snarfedRows, t.row)
		}
		// Single undo entry restores all deleted rows.
		var restoredIDs []int64
		hadSelection := selSnap != nil
		m.pushUndo(undoEntry{
			desc:    fmt.Sprintf("cut %d row(s)", len(saved)),
			tagID:   saved[0].tagID,
			tagName: saved[0].tagName,
			postUndoSelectionFn: func() map[int64]bool {
				if !hadSelection || len(restoredIDs) <= 1 {
					return nil
				}
				sel := make(map[int64]bool, len(restoredIDs))
				for _, id := range restoredIDs {
					sel[id] = true
				}
				return sel
			},
			undoFn: func(exoDB *db.ExoDB) (int64, error) {
				restoredIDs = restoredIDs[:0]
				var lastID int64
				for _, s := range saved {
					t, err := exoDB.AddTag(s.tagName)
					if err != nil {
						return 0, err
					}
					newRow, err := exoDB.AddRow(t.ID, s.text, 0)
					if err != nil {
						return 0, err
					}
					_ = exoDB.UpdateRowRank(newRow.ID, s.rank)
					if s.priority != 0 {
						_ = exoDB.UpdateRowPriority(newRow.ID, s.priority)
					}
					if s.note != "" {
						_ = exoDB.UpdateRowNote(newRow.ID, s.note)
					}
					restoredIDs = append(restoredIDs, newRow.ID)
					lastID = newRow.ID
				}
				return lastID, nil
			},
			redoFn: func(exoDB *db.ExoDB) (int64, error) {
				for _, id := range restoredIDs {
					if err := exoDB.DeleteRowByID(id); err != nil {
						return 0, err
					}
				}
				return 0, nil
			},
		})
		m.selectedRows = make(map[int64]bool)
		m.setStatus(fmt.Sprintf("cut %d row(s)", len(saved)))
		m.refresh()
		if landRowID != 0 {
			m.positionCursor(landRowID)
		} else {
			m.clampCursorToDirectRows()
		}

	case "D":
		if len(m.rowItems) == 0 {
			m.setErr("no row selected")
			break
		}
		selSnap := m.snapshotSelection()
		targets := m.collectTargets()
		type doneChange struct {
			id      int64
			prev    bool
			next    bool
			oldRank int
			newRank int
		}
		// Compute max rank across all rows so un-done rows go to end of list.
		maxRank := 0
		for _, it := range m.rowItems {
			if !it.isRef && it.row.Rank > maxRank {
				maxRank = it.row.Rank
			}
		}
		var changes []doneChange
		errored := false
		for _, it := range targets {
			if it.isRef {
				continue
			}
			next := !it.row.Done
			if err := m.dbState.UpdateRowDone(it.row.ID, next); err != nil {
				m.setErr(err.Error())
				errored = true
				break
			}
			c := doneChange{id: it.row.ID, prev: it.row.Done, next: next, oldRank: it.row.Rank}
			if !next {
				// Marking un-done: move to end of list.
				maxRank++
				c.newRank = maxRank
				_ = m.dbState.UpdateRowRank(it.row.ID, c.newRank)
			}
			changes = append(changes, c)
		}
		if errored || len(changes) == 0 {
			break
		}
		tagID, tagName := m.dbState.CurrentDBTag.ID, m.dbState.CurrentDBTag.Name
		m.pushUndo(undoEntry{
			desc:              fmt.Sprintf("toggle done on %d row(s)", len(changes)),
			tagID:             tagID,
			tagName:           tagName,
			postUndoSelection: selSnap,
			undoFn: func(exoDB *db.ExoDB) (int64, error) {
				for _, c := range changes {
					if err := exoDB.UpdateRowDone(c.id, c.prev); err != nil {
						return 0, err
					}
					if !c.next {
						if err := exoDB.UpdateRowRank(c.id, c.oldRank); err != nil {
							return 0, err
						}
					}
				}
				return changes[0].id, nil
			},
			redoFn: func(exoDB *db.ExoDB) (int64, error) {
				for _, c := range changes {
					if err := exoDB.UpdateRowDone(c.id, c.next); err != nil {
						return 0, err
					}
					if !c.next {
						if err := exoDB.UpdateRowRank(c.id, c.newRank); err != nil {
							return 0, err
						}
					}
				}
				return changes[0].id, nil
			},
		})
		m.selectedRows = make(map[int64]bool)
		if len(changes) == 1 && changes[0].next {
			// Animate the strikethrough before refreshing.
			m.animRowID = changes[0].id
			m.animText = targets[0].row.Text
			m.animPos = 0
			return doneAnimTick()
		}
		m.refresh()
		if len(changes) == 1 && !changes[0].next {
			m.positionCursor(changes[0].id)
		} else {
			m.clampCursorToDirectRows()
		}

	case "y":
		if len(m.rowItems) == 0 {
			m.setErr("no row selected")
			break
		}
		m.snarfedRows = nil
		for _, it := range m.collectTargets() {
			m.snarfedRows = append(m.snarfedRows, it.row)
		}
		m.selectedRows = make(map[int64]bool)
		m.setStatus(fmt.Sprintf("yanked %d row(s)", len(m.snarfedRows)))

	case "p":
		if len(m.snarfedRows) == 0 {
			m.setErr("snarf buffer empty")
			break
		}
		tagID, tagName := m.dbState.CurrentDBTag.ID, m.dbState.CurrentDBTag.Name
		texts := make([]string, len(m.snarfedRows))
		notes := make([]string, len(m.snarfedRows))
		for i, r := range m.snarfedRows {
			texts[i] = r.Text
			notes[i] = r.Note
		}
		baseRank := m.afterCursorRank()
		var pastedIDs []int64
		errored := false
		for i, text := range texts {
			newRow, err := m.dbState.AddRow(tagID, text, 0)
			if err != nil {
				m.setErr(err.Error())
				errored = true
				break
			}
			_ = m.dbState.UpdateRowRank(newRow.ID, baseRank+i)
			if notes[i] != "" {
				_ = m.dbState.UpdateRowNote(newRow.ID, notes[i])
			}
			pastedIDs = append(pastedIDs, newRow.ID)
		}
		if errored {
			break
		}
		m.pushUndo(undoEntry{
			desc:    fmt.Sprintf("paste %d row(s)", len(pastedIDs)),
			tagID:   tagID,
			tagName: tagName,
			undoFn: func(exoDB *db.ExoDB) (int64, error) {
				for _, id := range pastedIDs {
					if err := exoDB.DeleteRowByID(id); err != nil {
						return 0, err
					}
				}
				return 0, nil
			},
			redoFn: func(exoDB *db.ExoDB) (int64, error) {
				pastedIDs = pastedIDs[:0]
				t, err := exoDB.AddTag(tagName)
				if err != nil {
					return 0, err
				}
				var lastID int64
				for i, text := range texts {
					r, err := exoDB.AddRow(t.ID, text, 0)
					if err != nil {
						return 0, err
					}
					_ = exoDB.UpdateRowRank(r.ID, baseRank+i)
					if notes[i] != "" {
						_ = exoDB.UpdateRowNote(r.ID, notes[i])
					}
					pastedIDs = append(pastedIDs, r.ID)
					lastID = r.ID
				}
				return lastID, nil
			},
		})
		m.status = ""
		m.refresh()

	case "P":
		if len(m.snarfedRows) == 0 {
			m.setErr("snarf buffer empty")
			break
		}
		tagID, tagName := m.dbState.CurrentDBTag.ID, m.dbState.CurrentDBTag.Name
		texts := make([]string, len(m.snarfedRows))
		notes := make([]string, len(m.snarfedRows))
		for i, r := range m.snarfedRows {
			texts[i] = r.Text
			notes[i] = r.Note
		}
		baseRank := m.cursor
		if m.cursor >= len(m.rowItems) || m.rowItems[m.cursor].isRef {
			baseRank = 0
		}
		var pastedIDs []int64
		errored := false
		for i, text := range texts {
			newRow, err := m.dbState.AddRow(tagID, text, 0)
			if err != nil {
				m.setErr(err.Error())
				errored = true
				break
			}
			_ = m.dbState.UpdateRowRank(newRow.ID, baseRank+i)
			if notes[i] != "" {
				_ = m.dbState.UpdateRowNote(newRow.ID, notes[i])
			}
			pastedIDs = append(pastedIDs, newRow.ID)
		}
		if errored {
			break
		}
		m.pushUndo(undoEntry{
			desc:    fmt.Sprintf("paste %d row(s) above cursor", len(pastedIDs)),
			tagID:   tagID,
			tagName: tagName,
			undoFn: func(exoDB *db.ExoDB) (int64, error) {
				for _, id := range pastedIDs {
					if err := exoDB.DeleteRowByID(id); err != nil {
						return 0, err
					}
				}
				return 0, nil
			},
			redoFn: func(exoDB *db.ExoDB) (int64, error) {
				pastedIDs = pastedIDs[:0]
				t, err := exoDB.AddTag(tagName)
				if err != nil {
					return 0, err
				}
				var lastID int64
				for i, text := range texts {
					r, err := exoDB.AddRow(t.ID, text, 0)
					if err != nil {
						return 0, err
					}
					_ = exoDB.UpdateRowRank(r.ID, baseRank+i)
					if notes[i] != "" {
						_ = exoDB.UpdateRowNote(r.ID, notes[i])
					}
					pastedIDs = append(pastedIDs, r.ID)
					lastID = r.ID
				}
				return lastID, nil
			},
		})
		m.status = ""
		m.refresh()

	case "n":
		m.mode = modeInput
		m.pendingAction = actionNewTag
		m.setInputWidth()
		m.textInput.SetValue("")
		m.textInput.Placeholder = "new tag name"
		m.textInput.Focus()
		return textinput.Blink

	case "r":
		m.mode = modeInput
		m.pendingAction = actionRenameTag
		m.setInputWidth()
		m.textInput.SetValue(m.dbState.CurrentDBTag.Name)
		m.textInput.Placeholder = "rename tag"
		m.textInput.Focus()
		return textinput.Blink

	case "t":
		m.mode = modeTagSelect
		m.tagInput.SetValue("")
		m.tagInput.Focus()
		m.tagCursor = 0
		m.updateFilteredTags()
		return textinput.Blink

	case "?":
		m.mode = modeHelp

	case "\\":
		m.hideDone = !m.hideDone
		m.refresh()

	case "/":
		m.allSearchRows = nil
		for _, tag := range m.dbState.AllDBTags {
			rows, err := m.dbState.GetRowsForTagID(tag.ID)
			if err != nil {
				continue
			}
			for _, row := range rows {
				if m.hideDone && row.Done {
					continue
				}
				m.allSearchRows = append(m.allSearchRows, searchResult{row: row, tag: tag})
			}
		}
		m.mode = modeSearch
		m.searchInput.SetValue("")
		m.searchInput.Focus()
		m.searchCursor = 0
		m.updateSearchResults()
		return textinput.Blink

	default:
		if i, err := strconv.Atoi(msg.String()); err == nil {
			if tag, ok := m.tagShortcutsRev[i]; ok {
				m.status = ""
				m.switchTag(tag)
			} else {
				m.setErr(fmt.Sprintf("no tag ref: %d", i))
			}
		}
	}
	return nil
}

func (m *model) handleInputKey(msg tea.KeyMsg) tea.Cmd {
	// When autocomplete is active, intercept navigation keys.
	if m.acActive {
		switch msg.Type {
		case tea.KeyEsc:
			m.acActive = false
			return nil
		case tea.KeyUp:
			if m.acCursor > 0 {
				m.acCursor--
			}
			return nil
		case tea.KeyDown:
			if m.acCursor < len(m.acTags)-1 {
				m.acCursor++
			}
			return nil
		case tea.KeyTab, tea.KeyEnter:
			m.completeTag()
			return nil
		}
	}

	switch msg.Type {
	case tea.KeyEsc:
		m.mode = modeMain
		m.acActive = false
		m.textInput.Blur()
		return nil
	case tea.KeyEnter:
		text := strings.TrimSpace(m.textInput.Value())
		m.mode = modeMain
		m.acActive = false
		m.textInput.Blur()
		if text == "" {
			switch m.pendingAction {
			case actionNewTag, actionRenameTag:
				m.setErr("empty input")
				return nil
			default:
				initialText := m.pendingRow.Text
				if m.pendingAction == actionAddRow || m.pendingAction == actionInsertRow {
					initialText = ""
				}
				return openEditorCmd(initialText, m.pendingAction, m.pendingRow)
			}
		}
		return m.handleEditorDone(editorDoneMsg{text: text, action: m.pendingAction, row: m.pendingRow})
	}
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	m.updateTagComplete()
	return cmd
}

func (m *model) handleTagSelectKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = modeMain
		m.tagInput.Blur()
		return nil
	case tea.KeyEnter:
		if m.tagCursor < len(m.filteredTags) {
			tag := m.filteredTags[m.tagCursor]
			m.mode = modeMain
			m.tagInput.Blur()
			m.status = ""
			m.switchTag(tag)
		}
		return nil
	case tea.KeyUp:
		if m.tagCursor > 0 {
			m.tagCursor--
		}
		return nil
	case tea.KeyDown:
		if m.tagCursor < len(m.filteredTags)-1 {
			m.tagCursor++
		}
		return nil
	}
	var cmd tea.Cmd
	m.tagInput, cmd = m.tagInput.Update(msg)
	m.updateFilteredTags()
	return cmd
}

func (m *model) handleSearchKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = modeMain
		m.searchInput.Blur()
		return nil
	case tea.KeyEnter:
		if m.searchCursor < len(m.searchResults) {
			sr := m.searchResults[m.searchCursor]
			m.mode = modeMain
			m.searchInput.Blur()
			m.status = ""
			tag, err := m.dbState.GetTagByID(sr.tag.ID)
			if err != nil || tag.ID == 0 {
				m.setErr("tag not found")
				return nil
			}
			m.switchTag(tag)
			m.positionCursor(sr.row.ID)
		}
		return nil
	case tea.KeyUp:
		if m.searchCursor > 0 {
			m.searchCursor--
		}
		return nil
	case tea.KeyDown:
		if m.searchCursor < len(m.searchResults)-1 {
			m.searchCursor++
		}
		return nil
	}
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	m.updateSearchResults()
	return cmd
}
