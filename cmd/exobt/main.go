package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/neutralinsomniac/exocortex/db"
)

// ── styles ───────────────────────────────────────────────────────────────────

var (
	styleHeader    = lipgloss.NewStyle().Bold(true)
	styleSelected  = lipgloss.NewStyle().Background(lipgloss.Color("240")).Foreground(lipgloss.Color("255"))
	styleMarked = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleRefHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	styleErr       = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleOK        = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleDim       = lipgloss.NewStyle().Faint(true)
	styleKey       = lipgloss.NewStyle().Bold(true)
)

// ── types ────────────────────────────────────────────────────────────────────

type viewMode int

const (
	modeMain viewMode = iota
	modeInput
	modeTagSelect
	modeCalendar
	modeHelp
)

type pendingAction int

const (
	actionAddRow pendingAction = iota
	actionInsertRow
	actionNewTag
	actionRenameTag
	actionEditRow
	actionEditNote
)

type rowItem struct {
	row    db.Row
	isRef  bool
	refTag db.Tag
}

type editorDoneMsg struct {
	text   string
	action pendingAction
	row    db.Row
	err    error
}

type undoEntry struct {
	desc    string
	tagID   int64
	tagName string // used to recreate the tag if it was auto-deleted
	// fns return the row ID that was created/modified (0 = no specific row, e.g. a deletion)
	undoFn func(exoDB *db.ExoDB) (int64, error)
	redoFn func(exoDB *db.ExoDB) (int64, error) // nil = not redoable
}

// ── model ────────────────────────────────────────────────────────────────────

type model struct {
	dbState db.State

	// display rows and shortcuts
	rowItems        []rowItem
	tagShortcuts    map[db.Tag]int
	tagShortcutsRev map[int]db.Tag
	tagNameToNum    map[string]int // fast lookup for rendering
	allTagNames     map[string]bool

	selectedRows map[int64]bool // row IDs in the multi-select set

	snarfedRows []db.Row
	tagStack    []string
	undoStack  []undoEntry
	redoStack  []undoEntry

	// navigation
	cursor int
	mode   viewMode

	// input mode
	textInput     textinput.Model
	pendingAction pendingAction
	pendingRow    db.Row
	pendingRank   int // rank at which the pending add/insert should land

	// tag select mode
	tagInput     textinput.Model
	filteredTags []db.Tag
	tagCursor    int

	// calendar mode
	calDate time.Time

	// status bar
	status string
	isErr  bool

	width, height int
}

var tagRe = regexp.MustCompile(`\[\[(.*?)\]\]`)

var inputPrompts = map[pendingAction]string{
	actionAddRow:    "Add row",
	actionInsertRow: "Insert row",
	actionNewTag:    "New tag",
	actionRenameTag: "Rename tag",
	actionEditRow:   "Edit row",
}

func newModel(exoDB *db.ExoDB) model {
	ti := textinput.New()
	ti.CharLimit = 1024

	tagi := textinput.New()
	tagi.Placeholder = "type to filter..."
	tagi.CharLimit = 200

	m := model{
		textInput:       ti,
		tagInput:        tagi,
		tagShortcuts:    make(map[db.Tag]int),
		tagShortcutsRev: make(map[int]db.Tag),
		tagNameToNum:    make(map[string]int),
		allTagNames:     make(map[string]bool),
		selectedRows:    make(map[int64]bool),
	}
	m.dbState.ExoDB = exoDB
	m.goToToday()
	return m
}

// ── db / state helpers ───────────────────────────────────────────────────────

func (m *model) refresh() {
	m.dbState.Refresh()
	m.tagShortcuts = make(map[db.Tag]int)
	m.tagShortcutsRev = make(map[int]db.Tag)
	m.tagNameToNum = make(map[string]int)
	m.allTagNames = make(map[string]bool)
	for _, t := range m.dbState.AllDBTags {
		m.allTagNames[t.Name] = true
	}
	m.rebuildRows()
}

func (m *model) rebuildRows() {
	m.rowItems = nil

	for _, row := range m.dbState.CurrentDBRows {
		m.rowItems = append(m.rowItems, rowItem{row: row})
	}
	for _, refTag := range m.dbState.SortedRefTagsKeys {
		m.assignTagShortcut(refTag)
		for _, row := range m.dbState.CurrentDBRefs[refTag] {
			m.rowItems = append(m.rowItems, rowItem{
				row: row, isRef: true, refTag: refTag,
			})
		}
	}

	// precompute tag shortcuts from row text so View() needs no DB calls
	for _, item := range m.rowItems {
		for _, match := range tagRe.FindAllStringSubmatch(item.row.Text, -1) {
			name := match[1]
			if _, seen := m.tagNameToNum[name]; seen {
				continue
			}
			tag, err := m.dbState.GetTagByName(name)
			if err == nil && tag.ID != 0 {
				n := m.assignTagShortcut(tag)
				m.tagNameToNum[name] = n
			}
		}
	}

	if m.cursor >= len(m.rowItems) {
		m.cursor = len(m.rowItems) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// setInputWidth keeps m.textInput.Width in sync with the terminal width so
// the input scrolls correctly while typing.
func (m *model) setInputWidth() {
	const escHint = "  esc to cancel"
	prompt := inputPrompts[m.pendingAction] + ": "
	w := m.textW() - 1 - ansi.StringWidth(prompt) - ansi.StringWidth(escHint)
	if w < 1 {
		w = 1
	}
	m.textInput.Width = w
}

// afterCursorRank returns the rank at which a new row should be inserted so
// that it appears immediately after the cursor. When the cursor is on a ref
// row (or the list is empty) the row is appended after all direct rows.
func (m *model) afterCursorRank() int {
	if m.cursor < len(m.rowItems) && !m.rowItems[m.cursor].isRef {
		return m.cursor + 1
	}
	n := 0
	for _, it := range m.rowItems {
		if !it.isRef {
			n++
		}
	}
	return n
}

func (m *model) assignTagShortcut(tag db.Tag) int {
	if n, ok := m.tagShortcuts[tag]; ok {
		return n
	}
	max := 0
	for _, i := range m.tagShortcuts {
		if i > max {
			max = i
		}
	}
	n := max + 1
	m.tagShortcuts[tag] = n
	m.tagShortcutsRev[n] = tag
	m.tagNameToNum[tag.Name] = n
	return n
}

func (m *model) switchTag(tag db.Tag) {
	if tag.ID != m.dbState.CurrentDBTag.ID && m.dbState.CurrentDBTag.ID != 0 {
		_ = m.dbState.DeleteTagIfEmpty(m.dbState.CurrentDBTag.ID)
		m.tagStack = append(m.tagStack, m.dbState.CurrentDBTag.Name)
	}
	m.dbState.CurrentDBTag = tag
	m.cursor = 0
	m.selectedRows = make(map[int64]bool)
	m.refresh()
}

func (m *model) goToToday() { m.goToDate(time.Now()) }

func (m *model) goToDate(t time.Time) {
	tag, err := m.dbState.AddTag(t.Format("January 02 2006"))
	if err != nil {
		m.setErr(err.Error())
		return
	}
	m.status = ""
	m.switchTag(tag)
}

func (m *model) popTag() {
	if len(m.tagStack) == 0 {
		m.setErr("tag stack empty")
		return
	}
	l := len(m.tagStack)
	name := m.tagStack[l-1]
	m.tagStack = m.tagStack[:l-1]

	tag, err := m.dbState.AddTag(name)
	if err != nil {
		m.setErr(err.Error())
		return
	}
	if tag.ID != m.dbState.CurrentDBTag.ID {
		_ = m.dbState.DeleteTagIfEmpty(m.dbState.CurrentDBTag.ID)
	}
	m.dbState.CurrentDBTag = tag
	m.cursor = 0
	m.refresh()
	m.status = ""
}

func (m *model) moveDays(n int) {
	d, err := time.Parse("January 02 2006", m.dbState.CurrentDBTag.Name)
	if err != nil {
		m.setErr("not on a date tag")
		return
	}
	m.goToDate(d.AddDate(0, 0, n))
}

func (m *model) setErr(s string)    { m.status = s; m.isErr = true }
func (m *model) setStatus(s string) { m.status = s; m.isErr = false }

const maxUndoStack = 50

func (m *model) pushUndo(entry undoEntry) {
	m.undoStack = append(m.undoStack, entry)
	if len(m.undoStack) > maxUndoStack {
		m.undoStack = m.undoStack[1:]
	}
	m.redoStack = nil // new action clears redo history
}

func (m *model) applyUndo() {
	if len(m.undoStack) == 0 {
		m.setErr("nothing to undo")
		return
	}
	l := len(m.undoStack)
	entry := m.undoStack[l-1]
	m.undoStack = m.undoStack[:l-1]
	rowID, err := entry.undoFn(m.dbState.ExoDB)
	if err != nil {
		m.setErr("undo failed: " + err.Error())
		return
	}
	if entry.redoFn != nil {
		m.redoStack = append(m.redoStack, entry)
	}
	// Navigate to the tag where the change occurred, recreating it if auto-deleted.
	if tag := m.resolveUndoTag(entry); tag.ID != 0 {
		m.dbState.CurrentDBTag = tag
		m.cursor = 0
	}
	m.setStatus("undid: " + entry.desc)
	m.refresh()
	m.positionCursor(rowID)
}

func (m *model) applyRedo() {
	if len(m.redoStack) == 0 {
		m.setErr("nothing to redo")
		return
	}
	l := len(m.redoStack)
	entry := m.redoStack[l-1]
	m.redoStack = m.redoStack[:l-1]
	rowID, err := entry.redoFn(m.dbState.ExoDB)
	if err != nil {
		m.setErr("redo failed: " + err.Error())
		return
	}
	m.undoStack = append(m.undoStack, entry)
	// Navigate to the tag where the change occurred, recreating it if auto-deleted.
	if tag := m.resolveUndoTag(entry); tag.ID != 0 {
		m.dbState.CurrentDBTag = tag
		m.cursor = 0
	}
	m.setStatus("redid: " + entry.desc)
	m.refresh()
	m.positionCursor(rowID)
}

// positionCursor moves the cursor to the row with the given ID, if present.
func (m *model) positionCursor(rowID int64) {
	if rowID == 0 {
		return
	}
	for i, item := range m.rowItems {
		if item.row.ID == rowID {
			m.cursor = i
			return
		}
	}
}

// resolveUndoTag returns the tag for an undo entry, recreating it via AddTag
// if it was auto-deleted after becoming empty.
func (m *model) resolveUndoTag(entry undoEntry) db.Tag {
	if entry.tagID != 0 {
		if tag, err := m.dbState.GetTagByID(entry.tagID); err == nil && tag.ID != 0 {
			return tag
		}
	}
	if entry.tagName != "" {
		if tag, err := m.dbState.AddTag(entry.tagName); err == nil {
			return tag
		}
	}
	return db.Tag{}
}

func (m *model) updateFilteredTags() {
	f := strings.ToLower(m.tagInput.Value())
	m.filteredTags = nil
	for _, tag := range m.dbState.AllDBTags {
		if f == "" || strings.Contains(strings.ToLower(tag.Name), f) {
			m.filteredTags = append(m.filteredTags, tag)
		}
	}
	if m.tagCursor >= len(m.filteredTags) {
		m.tagCursor = len(m.filteredTags) - 1
	}
	if m.tagCursor < 0 {
		m.tagCursor = 0
	}
}

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

// ── tea.Model ────────────────────────────────────────────────────────────────

func (m model) Init() tea.Cmd { return textinput.Blink }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.mode == modeInput {
			m.setInputWidth()
		}
	case editorDoneMsg:
		cmd = m.handleEditorDone(msg)
	case tea.KeyMsg:
		switch m.mode {
		case modeMain:
			cmd = m.handleMainKey(msg)
		case modeInput:
			cmd = m.handleInputKey(msg)
		case modeTagSelect:
			cmd = m.handleTagSelectKey(msg)
		case modeCalendar:
			cmd = m.handleCalendarKey(msg)
		case modeHelp:
			m.mode = modeMain
		}
	}
	return m, cmd
}

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
	case actionInsertRow:
		row, err := m.dbState.AddRow(m.dbState.CurrentDBTag.ID, msg.text, 0)
		if err != nil {
			m.setErr(err.Error())
			return nil
		}
		_ = m.dbState.UpdateRowRank(row.ID, 0)
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
				return latestID, exoDB.UpdateRowRank(latestID, 0)
			},
		})
		m.status = ""
		m.refresh()
	case actionEditNote:
		oldNote, newNote, rowID := msg.row.Note, msg.text, msg.row.ID
		if err := m.dbState.UpdateRowNote(rowID, newNote); err != nil {
			m.setErr(err.Error())
			return nil
		}
		noteTagID, noteTagName := m.dbState.CurrentDBTag.ID, m.dbState.CurrentDBTag.Name
		m.pushUndo(undoEntry{
			desc:    "edit note",
			tagID:   noteTagID,
			tagName: noteTagName,
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
		editTagID, editTagName := m.dbState.CurrentDBTag.ID, m.dbState.CurrentDBTag.Name
		m.pushUndo(undoEntry{
			desc:    "edit row",
			tagID:   editTagID,
			tagName: editTagName,
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
		tag, err := m.dbState.AddTag(msg.text)
		if err != nil {
			m.setErr(err.Error())
			return nil
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
		renameTagID := tag.ID
		m.pushUndo(undoEntry{
			desc:  "rename tag",
			tagID: renameTagID,
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
		_ = m.dbState.DeleteTagIfEmpty(m.dbState.CurrentDBTag.ID)
		return tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.rowItems)-1 {
			m.cursor++
		}

	case "u":
		m.applyUndo()

	case "U":
		m.applyRedo()

	case "K": // move selected row up
		if len(m.rowItems) == 0 || m.cursor == 0 || m.rowItems[m.cursor].isRef {
			break
		}
		row := m.rowItems[m.cursor].row
		rowID, fromRank, toRank := row.ID, m.cursor, m.cursor-1
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

	case "J": // move selected row down
		next := m.cursor + 1
		if len(m.rowItems) == 0 || next >= len(m.rowItems) ||
			m.rowItems[m.cursor].isRef || m.rowItems[next].isRef {
			break
		}
		row := m.rowItems[m.cursor].row
		rowID, fromRank, toRank := row.ID, m.cursor, next
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
		if m.cursor < len(m.rowItems)-1 {
			m.cursor++
		}

	case "g":
		m.status = ""
		m.goToToday()

	case "b":
		m.popTag()

	case "<":
		m.moveDays(-1)

	case ">":
		m.moveDays(1)

	case "enter":
		if len(m.rowItems) == 0 || m.cursor >= len(m.rowItems) {
			break
		}
		matches := tagRe.FindAllStringSubmatch(m.rowItems[m.cursor].row.Text, -1)
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

	case "a":
		m.mode = modeInput
		m.pendingAction = actionAddRow
		m.pendingRank = m.afterCursorRank()
		m.setInputWidth()
		m.textInput.SetValue("")
		m.textInput.Placeholder = "row text (empty = open $EDITOR)"
		m.textInput.Focus()
		return textinput.Blink

	case "A":
		m.mode = modeInput
		m.pendingAction = actionInsertRow
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
		// Collect items to cut: selection if non-empty, else cursor row.
		var targets []rowItem
		if len(m.selectedRows) > 0 {
			for _, it := range m.rowItems {
				if !it.isRef && m.selectedRows[it.row.ID] {
					targets = append(targets, it)
				}
			}
		}
		if len(targets) == 0 {
			targets = []rowItem{m.rowItems[m.cursor]}
		}
		// Save info needed for undo before deleting.
		type savedRow struct {
			tagID   int64
			tagName string
			text    string
			note    string
			rank    int
		}
		var saved []savedRow
		for idx, it := range targets {
			tagID := m.dbState.CurrentDBTag.ID
			tagName := m.dbState.CurrentDBTag.Name
			if it.isRef {
				tagID = it.refTag.ID
				tagName = it.refTag.Name
			}
			// Rank approximation: position in rowItems minus ref rows before it.
			rank := idx
			for _, ri := range m.rowItems {
				if ri.row.ID == it.row.ID {
					break
				}
				if !ri.isRef {
					rank++
				}
			}
			saved = append(saved, savedRow{tagID, tagName, it.row.Text, it.row.Note, rank})
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
		m.pushUndo(undoEntry{
			desc:    fmt.Sprintf("cut %d row(s)", len(saved)),
			tagID:   saved[0].tagID,
			tagName: saved[0].tagName,
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

	case "D":
		if len(m.rowItems) == 0 {
			m.setErr("no row selected")
			break
		}
		// Collect targets: selection if non-empty, else cursor row.
		var dTargets []rowItem
		if len(m.selectedRows) > 0 {
			for _, it := range m.rowItems {
				if !it.isRef && m.selectedRows[it.row.ID] {
					dTargets = append(dTargets, it)
				}
			}
		}
		if len(dTargets) == 0 {
			dTargets = []rowItem{m.rowItems[m.cursor]}
		}
		doneTag, err := m.dbState.AddTag("done")
		if err != nil {
			m.setErr(err.Error())
			break
		}
		type doneRow struct {
			rowID       int64
			fromTagID   int64
			fromTagName string
			origText    string
			newText     string
		}
		var doneRows []doneRow
		errored := false
		for _, it := range dTargets {
			fromTagID := m.dbState.CurrentDBTag.ID
			fromTagName := m.dbState.CurrentDBTag.Name
			if it.isRef {
				fromTagID = it.refTag.ID
				fromTagName = it.refTag.Name
			}
			origText := it.row.Text
			newText := origText + " [[" + fromTagName + "]]"
			if err := m.dbState.MoveRowToTag(it.row.ID, doneTag.ID); err != nil {
				m.setErr(err.Error())
				errored = true
				break
			}
			if err := m.dbState.UpdateRowText(it.row.ID, newText); err != nil {
				m.setErr(err.Error())
				errored = true
				break
			}
			doneRows = append(doneRows, doneRow{it.row.ID, fromTagID, fromTagName, origText, newText})
		}
		if errored {
			break
		}
		m.pushUndo(undoEntry{
			desc:    fmt.Sprintf("move %d row(s) to done", len(doneRows)),
			tagID:   doneRows[0].fromTagID,
			tagName: doneRows[0].fromTagName,
			undoFn: func(exoDB *db.ExoDB) (int64, error) {
				var lastID int64
				for _, dr := range doneRows {
					t, err := exoDB.AddTag(dr.fromTagName)
					if err != nil {
						return 0, err
					}
					if err := exoDB.MoveRowToTag(dr.rowID, t.ID); err != nil {
						return 0, err
					}
					if err := exoDB.UpdateRowText(dr.rowID, dr.origText); err != nil {
						return 0, err
					}
					lastID = dr.rowID
				}
				return lastID, nil
			},
			redoFn: func(exoDB *db.ExoDB) (int64, error) {
				done, err := exoDB.AddTag("done")
				if err != nil {
					return 0, err
				}
				var lastID int64
				for _, dr := range doneRows {
					if err := exoDB.MoveRowToTag(dr.rowID, done.ID); err != nil {
						return 0, err
					}
					if err := exoDB.UpdateRowText(dr.rowID, dr.newText); err != nil {
						return 0, err
					}
					lastID = dr.rowID
				}
				return lastID, nil
			},
		})
		m.selectedRows = make(map[int64]bool)
		m.setStatus(fmt.Sprintf("moved %d row(s) to [[done]]", len(doneRows)))
		m.refresh()

	case "y":
		if len(m.rowItems) == 0 {
			m.setErr("no row selected")
			break
		}
		m.snarfedRows = nil
		if len(m.selectedRows) > 0 {
			for _, it := range m.rowItems {
				if !it.isRef && m.selectedRows[it.row.ID] {
					m.snarfedRows = append(m.snarfedRows, it.row)
				}
			}
		}
		if len(m.snarfedRows) == 0 {
			m.snarfedRows = []db.Row{m.rowItems[m.cursor].row}
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
		tagID2, tagName2 := m.dbState.CurrentDBTag.ID, m.dbState.CurrentDBTag.Name
		texts2 := make([]string, len(m.snarfedRows))
		notes2 := make([]string, len(m.snarfedRows))
		for i, r := range m.snarfedRows {
			texts2[i] = r.Text
			notes2[i] = r.Note
		}
		// Insert in reverse order so that after all rank-0 inserts the
		// original order is preserved (each insert pushes the rest down).
		var pastedIDs2 []int64
		errored2 := false
		for i := len(texts2) - 1; i >= 0; i-- {
			newRow, err := m.dbState.AddRow(tagID2, texts2[i], 0)
			if err != nil {
				m.setErr(err.Error())
				errored2 = true
				break
			}
			_ = m.dbState.UpdateRowRank(newRow.ID, 0)
			if notes2[i] != "" {
				_ = m.dbState.UpdateRowNote(newRow.ID, notes2[i])
			}
			pastedIDs2 = append(pastedIDs2, newRow.ID)
		}
		if errored2 {
			break
		}
		m.pushUndo(undoEntry{
			desc:    fmt.Sprintf("paste %d row(s) at start", len(pastedIDs2)),
			tagID:   tagID2,
			tagName: tagName2,
			undoFn: func(exoDB *db.ExoDB) (int64, error) {
				for _, id := range pastedIDs2 {
					if err := exoDB.DeleteRowByID(id); err != nil {
						return 0, err
					}
				}
				return 0, nil
			},
			redoFn: func(exoDB *db.ExoDB) (int64, error) {
				pastedIDs2 = pastedIDs2[:0]
				t, err := exoDB.AddTag(tagName2)
				if err != nil {
					return 0, err
				}
				for i := len(texts2) - 1; i >= 0; i-- {
					r, err := exoDB.AddRow(t.ID, texts2[i], 0)
					if err != nil {
						return 0, err
					}
					_ = exoDB.UpdateRowRank(r.ID, 0)
					if notes2[i] != "" {
						_ = exoDB.UpdateRowNote(r.ID, notes2[i])
					}
					pastedIDs2 = append(pastedIDs2, r.ID)
				}
				return pastedIDs2[len(pastedIDs2)-1], nil
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

	case "c":
		d, err := time.Parse("January 02 2006", m.dbState.CurrentDBTag.Name)
		if err != nil {
			d = time.Now()
		}
		m.calDate = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, d.Location())
		m.mode = modeCalendar

	case "?":
		m.mode = modeHelp

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
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = modeMain
		m.textInput.Blur()
		return nil
	case tea.KeyEnter:
		text := strings.TrimSpace(m.textInput.Value())
		m.mode = modeMain
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

func (m *model) handleCalendarKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "q", "esc":
		m.mode = modeMain
	case "enter":
		m.goToDate(m.calDate)
		m.mode = modeMain
	case "g":
		m.calDate = time.Now().Truncate(24 * time.Hour)
	case "left", "h":
		m.calDate = m.calDate.AddDate(0, 0, -1)
	case "right", "l":
		m.calDate = m.calDate.AddDate(0, 0, 1)
	case "up", "k":
		m.calDate = m.calDate.AddDate(0, 0, -7)
	case "down", "j":
		m.calDate = m.calDate.AddDate(0, 0, 7)
	case "<":
		m.calDate = m.calDate.AddDate(0, -1, 0)
	case ">":
		m.calDate = m.calDate.AddDate(0, 1, 0)
	}
	return nil
}

// ── views ────────────────────────────────────────────────────────────────────

func (m model) View() string {
	if m.width == 0 {
		return "loading..."
	}
	switch m.mode {
	case modeMain:
		return m.viewMain()
	case modeInput:
		return m.viewInput()
	case modeTagSelect:
		return m.viewTagSelect()
	case modeCalendar:
		return m.viewCalendar()
	case modeHelp:
		return m.viewHelp()
	}
	return ""
}

// textW returns the usable text width inside a full-screen box.
// border(1 each side) + padding(1 each side) = 4 cols total.
func (m model) textW() int { return m.width - 5 }

// lineCount returns the number of content lines that fit inside a full-screen box,
// given bottomLines reserved outside the box below it.
// border(1 top + 1 bottom) = 2 rows.
func (m model) lineCount(bottomLines int) int { return m.height - bottomLines - 2 }

// bordered returns a lipgloss style for a box.  Width is determined by the
// content (all lines pre-padded to textW by boxContent), so we only set Height.
// Height in lipgloss v1 is content-only (border not included).
func (m model) bordered(bottomLines int) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(0, 1).
		Height(m.height - bottomLines - 2)
}

func rule(w int, label string) string {
	if label == "" {
		return styleDim.Render(strings.Repeat("─", w))
	}
	pad := w - len(label) - 4 // 4 = "─ " prefix + " ─..." suffix chars
	if pad < 0 {
		pad = 0
	}
	return styleDim.Render("─ " + label + " " + strings.Repeat("─", pad))
}

func (m model) hints() string {
	return " " + styleDim.Render(strings.Join([]string{
		styleKey.Render("j/k") + " nav",
		styleKey.Render("enter") + " follow",
		styleKey.Render("a") + " add",
		styleKey.Render("d") + " cut",
		styleKey.Render("e") + " edit",
		styleKey.Render("u/U") + " undo/redo",
		styleKey.Render("t") + " tags",
		styleKey.Render("g") + " today",
		styleKey.Render("b") + " back",
		styleKey.Render("?") + " help",
		styleKey.Render("q") + " quit",
	}, "  "))
}

func (m model) statusLine() string {
	if m.status == "" {
		return ""
	}
	if m.isErr {
		return " " + styleErr.Render(m.status)
	}
	return " " + styleOK.Render(m.status)
}

func fitLines(lines []string, max int, w int) []string {
	for len(lines) < max {
		lines = append(lines, "")
	}
	if len(lines) > max {
		lines = lines[:max]
	}
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, w, "")
	}
	return lines
}

// boxContent joins header and content lines for use in bordered boxes.
// Every line is truncated then padded to exactly w display columns so the box
// is always exactly w+4 columns wide (padding 1 each side + border 1 each side).
func boxContent(header, content []string, w int) string {
	all := append(header, content...)
	for i, l := range all {
		l = ansi.Truncate(l, w, "")
		if sw := ansi.StringWidth(l); sw < w {
			l += strings.Repeat(" ", w-sw)
		}
		all[i] = l
	}
	return strings.Join(all, "\n")
}

// renderRowText renders row text, substituting [[tag]] links with styled output.
// bg, if non-empty, is applied as the background color to every span so that
// a selection highlight is consistent across the whole line.
func (m model) renderRowText(text string, bg string) string {
	var sb strings.Builder
	tagStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	plainStyle := lipgloss.NewStyle()
	if bg != "" {
		tagStyle = tagStyle.Background(lipgloss.Color(bg))
		plainStyle = plainStyle.Background(lipgloss.Color(bg))
	}
	for {
		loc := tagRe.FindStringIndex(text)
		if loc == nil {
			if bg != "" {
				sb.WriteString(plainStyle.Render(text))
			} else {
				sb.WriteString(text)
			}
			break
		}
		prefix := text[:loc[0]]
		if prefix != "" {
			if bg != "" {
				sb.WriteString(plainStyle.Render(prefix))
			} else {
				sb.WriteString(prefix)
			}
		}
		name := text[loc[0]+2 : loc[1]-2]
		if n, ok := m.tagNameToNum[name]; ok {
			sb.WriteString(tagStyle.Render(fmt.Sprintf("%s(%d)", name, n)))
		} else {
			sb.WriteString(tagStyle.Render(name))
		}
		text = text[loc[1]:]
	}
	return sb.String()
}

// wrapText splits text into lines of at most width display columns, breaking
// on word boundaries. A word longer than width is kept on its own line.
func wrapText(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	cur := ""
	curW := 0
	for _, word := range words {
		ww := ansi.StringWidth(word)
		if curW == 0 {
			cur = word
			curW = ww
		} else if curW+1+ww <= width {
			cur += " " + word
			curW += 1 + ww
		} else {
			lines = append(lines, cur)
			cur = word
			curW = ww
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// mainContentLines builds the row/ref lines for the main content area.
func (m model) mainContentLines(innerW int) []string {
	var lines []string
	if len(m.rowItems) == 0 {
		lines = append(lines, styleDim.Render(" (no rows)"))
		return lines
	}
	prevRefTag := db.Tag{}
	for i, item := range m.rowItems {
		if item.isRef && item.refTag != prevRefTag {
			if prevRefTag.ID == 0 {
				lines = append(lines, "")
				lines = append(lines, rule(innerW, "References"))
			}
			n := m.tagNameToNum[item.refTag.Name]
			lines = append(lines, " "+styleRefHeader.Render(fmt.Sprintf("%s(%d)", item.refTag.Name, n)))
			prevRefTag = item.refTag
		}
		indent := " "
		if item.isRef {
			indent = "   "
		}
		marked := !item.isRef && m.selectedRows[item.row.ID]
		hasNote := !item.isRef && item.row.Note != ""

		// Calculate widths for wrapping.
		prefixW := ansi.StringWidth(indent) + 2 // bullet is always 2 display cols
		contIndent := strings.Repeat(" ", prefixW)
		noteSuffixW := 0
		if hasNote {
			noteSuffixW = 2 // " ✎"
		}
		textW := innerW - prefixW - noteSuffixW
		if textW < 1 {
			textW = 1
		}
		chunks := wrapText(item.row.Text, textW)

		if i == m.cursor {
			var bullet string
			if marked {
				bullet = styleMarked.Render("◆") + " "
			} else {
				bullet = "• "
			}
			for ci, chunk := range chunks {
				isLast := ci == len(chunks)-1
				pfx := contIndent
				if ci == 0 {
					pfx = indent + bullet
				}
				noteSuffix := ""
				if isLast && hasNote {
					noteSuffix = styleSelected.Render(" ") + styleDim.Background(lipgloss.Color("240")).Render("✎")
				}
				raw := pfx + m.renderRowText(chunk, "240") + noteSuffix
				if gap := innerW - ansi.StringWidth(raw); gap > 0 {
					raw += styleSelected.Render(strings.Repeat(" ", gap))
				}
				lines = append(lines, raw)
			}
		} else if marked {
			for ci, chunk := range chunks {
				isLast := ci == len(chunks)-1
				noteSuffix := ""
				if isLast && hasNote {
					noteSuffix = " " + styleDim.Render("✎")
				}
				pfx := contIndent
				bullet := " "
				if ci == 0 {
					pfx = indent
					bullet = styleMarked.Render("◆")
				}
				lines = append(lines, pfx+bullet+" "+m.renderRowText(chunk, "")+noteSuffix)
			}
		} else {
			for ci, chunk := range chunks {
				isLast := ci == len(chunks)-1
				noteSuffix := ""
				if isLast && hasNote {
					noteSuffix = " " + styleDim.Render("✎")
				}
				pfx := contIndent
				bullet := " "
				if ci == 0 {
					pfx = indent
					bullet = "•"
				}
				lines = append(lines, pfx+bullet+" "+m.renderRowText(chunk, "")+noteSuffix)
			}
		}
	}
	return lines
}

func (m model) viewMain() string {
	tw := m.textW()
	lc := m.lineCount(2) // 2 lines below box: status + hints

	header := []string{
		styleHeader.Render(" " + m.dbState.CurrentDBTag.Name),
		rule(tw, ""),
	}
	content := fitLines(m.mainContentLines(tw), lc-len(header), tw)

	box := m.bordered(2).Render(
		boxContent(header, content, tw),
	)
	return box + "\n" + m.statusLine() + "\n" + m.hints()
}

func (m model) viewInput() string {
	tw := m.textW()
	lc := m.lineCount(2)

	header := []string{
		styleHeader.Render(" " + m.dbState.CurrentDBTag.Name),
		rule(tw, ""),
	}
	content := fitLines(m.mainContentLines(tw), lc-len(header), tw)

	box := m.bordered(2).Render(
		boxContent(header, content, tw),
	)

	prompt := inputPrompts[m.pendingAction] + ": "
	inputLine := " " + styleKey.Render(prompt) +
		m.textInput.View() +
		styleDim.Render("  esc to cancel")

	return box + "\n" + inputLine + "\n" + m.hints()
}

func (m model) viewTagSelect() string {
	tw := m.textW()
	lc := m.lineCount(2)

	header := []string{
		styleHeader.Render(" Select Tag"),
		rule(tw, ""),
		" " + m.tagInput.View(),
		rule(tw, ""),
	}

	var tagLines []string
	for i, tag := range m.filteredTags {
		line := " " + tag.Name
		if i == m.tagCursor {
			line = styleSelected.Render(line)
		}
		tagLines = append(tagLines, line)
	}
	if len(m.filteredTags) == 0 {
		tagLines = append(tagLines, styleDim.Render(" (no matching tags)"))
	}

	content := fitLines(tagLines, lc-len(header), tw)
	box := m.bordered(2).Render(
		boxContent(header, content, tw),
	)
	navHints := " " + styleDim.Render(
		styleKey.Render("↑↓")+" navigate  "+
			styleKey.Render("enter")+" select  "+
			styleKey.Render("esc")+" cancel",
	)
	return box + "\n" + m.statusLine() + "\n" + navHints
}

func (m model) viewCalendar() string {
	tw := m.textW()
	lc := m.lineCount(2)

	today := time.Now()
	year, month, _ := m.calDate.Date()

	start := time.Date(year, month, 1, 0, 0, 0, 0, m.calDate.Location())
	calRow := strings.Repeat("   ", int(start.Weekday()))
	var calLines []string
	calLines = append(calLines, "S  M  T  W  H  F  S")
	for d := start; d.Month() == month; d = d.AddDate(0, 0, 1) {
		isToday := d.Year() == today.Year() && d.Month() == today.Month() && d.Day() == today.Day()
		isSelected := d.Year() == m.calDate.Year() && d.Month() == m.calDate.Month() && d.Day() == m.calDate.Day()
		tagExists := m.allTagNames[d.Format("January 02 2006")]
		s := fmt.Sprintf("%-3d", d.Day())
		switch {
		case isSelected:
			s = styleSelected.Render(s)
		case isToday:
			s = styleHeader.Render(s)
		case tagExists:
			s = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render(s)
		}
		calRow += s
		if d.Weekday() == time.Saturday {
			calLines = append(calLines, calRow)
			calRow = ""
		}
	}
	if calRow != "" {
		calLines = append(calLines, calRow)
	}

	// fetch rows for the selected day
	dayLabel := m.calDate.Format("January 02 2006")
	var dayRows []string
	if tag, err := m.dbState.GetTagByName(dayLabel); err == nil && tag.ID != 0 {
		if rows, err := m.dbState.GetRowsForTagID(tag.ID); err == nil {
			for _, r := range rows {
				dayRows = append(dayRows, " • "+r.Text)
			}
		}
	}
	if len(dayRows) == 0 {
		dayRows = []string{styleDim.Render(" (no items)")}
	}

	header := []string{
		styleHeader.Render(fmt.Sprintf(" %s %d", month.String()[:3], year)),
		rule(tw, ""),
		"",
	}
	calSection := calLines
	calSection = append(calSection, "", rule(tw, dayLabel))
	calSection = append(calSection, dayRows...)
	content := fitLines(calSection, lc-len(header), tw)

	box := m.bordered(2).Render(
		boxContent(header, content, tw),
	)
	calHints := " " + styleDim.Render(
		styleKey.Render("arrows")+" navigate  "+
			styleKey.Render("<>")+" month  "+
			styleKey.Render("enter")+" go to day  "+
			styleKey.Render("g")+" today  "+
			styleKey.Render("q/esc")+" back",
	)
	return box + "\n" + m.statusLine() + "\n" + calHints
}

func (m model) viewHelp() string {
	tw := m.textW()
	lc := m.lineCount(2)

	header := []string{
		styleHeader.Render(" Help"),
		rule(tw, ""),
		"",
	}
	helpLines := []string{
		" " + styleKey.Render("[Tags]"),
		"   g          go to today",
		"   n          new tag",
		"   r          rename current tag",
		"   t          tag selector (type to filter)",
		"   c          calendar",
		"   < >        prev/next day",
		"   b          back in tag stack",
		"   1-9        jump to tag reference",
		"",
		" " + styleKey.Render("[Rows]"),
		"   j / k      move cursor down / up",
		"   enter      follow tag link in selected row",
		"   a          add row to end",
		"   A          insert row at beginning",
		"   space       toggle row in/out of selection",
		"   e          edit selected row",
		"   N          add/edit note on selected row",
		"   d          cut selected row (or all marked)",
		"   D          move to [[done]] (or all marked)",
		"   y          yank selected row",
		"   p / P      paste to end / beginning",
		"   J / K      move row down / up",
		"   u          undo last change",
		"   U          redo last undone change",
	}
	content := fitLines(helpLines, lc-len(header), tw)

	box := m.bordered(2).Render(
		boxContent(header, content, tw),
	)
	return box + "\n" + styleDim.Render(" press any key to close") + "\n" + m.hints()
}

// ── main ─────────────────────────────────────────────────────────────────────

func main() {
	exoDB := &db.ExoDB{}
	if err := exoDB.Open("./exocortex.db"); err != nil {
		fmt.Fprintln(os.Stderr, "open db:", err)
		os.Exit(1)
	}
	defer exoDB.Close()

	if err := exoDB.LoadSchema(); err != nil {
		fmt.Fprintln(os.Stderr, "load schema:", err)
		os.Exit(1)
	}

	if err := exoDB.Migrate(); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}

	p := tea.NewProgram(newModel(exoDB), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
