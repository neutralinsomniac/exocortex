package main

import (
	"cmp"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
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
	styleMarked    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleRefHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	styleErr       = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleOK        = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleDim       = lipgloss.NewStyle().Faint(true)
	styleKey       = lipgloss.NewStyle().Bold(true)

	styleDone = lipgloss.NewStyle().Faint(true).Strikethrough(true)

	// priority tag styles: 1=red bold, 2=yellow, 3=green, 4=default, 5=dim
	stylePriority = [6]lipgloss.Style{
		{}, // 0: unused
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		lipgloss.NewStyle(),
		lipgloss.NewStyle().Faint(true),
	}
)

// ── types ────────────────────────────────────────────────────────────────────

type viewMode int

const (
	modeMain viewMode = iota
	modeInput
	modeTagSelect
	modeHelp
	modeSearch
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

type searchResult struct {
	row        db.Row
	tag        db.Tag
	matchStart int // rune index of match start in row.Text
	matchEnd   int // rune index of match end (exclusive)
}

type doneAnimTickMsg struct{}

func doneAnimTick() tea.Cmd {
	return tea.Tick(10*time.Millisecond, func(time.Time) tea.Msg { return doneAnimTickMsg{} })
}

type editorDoneMsg struct {
	text   string
	action pendingAction
	row    db.Row
	err    error
}

type tagStackEntry struct {
	tagName string
	rowID   int64 // row we were on when we navigated away (0 if unknown)
}

type undoEntry struct {
	desc    string
	tagID   int64
	tagName string // used to recreate the tag if it was auto-deleted
	// fns return the row ID that was created/modified (0 = no specific row, e.g. a deletion)
	undoFn func(exoDB *db.ExoDB) (int64, error)
	redoFn func(exoDB *db.ExoDB) (int64, error) // nil = not redoable
	// postUndoCursorRank positions the cursor at a specific rank after undo,
	// used when undoFn deletes a row (returns rowID=0). Only active when
	// postUndoCursorRankSet is true.
	postUndoCursorRank    int
	postUndoCursorRankSet bool
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
	hideDone    bool // when true, done items are hidden entirely
	tagStack    []tagStackEntry

	// done animation
	animRowID  int64  // row currently animating (0 = none)
	animText   string // original text of the animating row
	animPos    int    // strikethrough has drawn through this many runes
	undoStack   []undoEntry
	redoStack   []undoEntry

	// navigation
	cursor     int
	lineOffset int // scroll offset into mainContentLines
	mode       viewMode

	// input mode
	textInput     textinput.Model
	pendingAction pendingAction
	pendingRow    db.Row
	pendingRank int // rank at which the pending add/insert should land

	// tag select mode
	tagInput     textinput.Model
	filteredTags []db.Tag
	tagCursor    int

	// inline tag autocomplete (active in modeInput when typing [[)
	acActive bool
	acTags   []db.Tag
	acCursor int

	// search mode
	searchInput   textinput.Model
	allSearchRows []searchResult
	searchResults []searchResult
	searchCursor  int

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

	si := textinput.New()
	si.Placeholder = "fuzzy search all items..."
	si.CharLimit = 200

	m := model{
		textInput:       ti,
		tagInput:        tagi,
		searchInput:     si,
		hideDone:        true,
		tagShortcuts:    make(map[db.Tag]int),
		tagShortcutsRev: make(map[int]db.Tag),
		tagNameToNum:    make(map[string]int),
		allTagNames:     make(map[string]bool),
		selectedRows:    make(map[int64]bool),
	}
	m.dbState.ExoDB = exoDB
	if lastTag, err := exoDB.GetSetting("last_tag"); err == nil && lastTag != "" {
		if tag, err := exoDB.GetTagByName(lastTag); err == nil && tag.ID != 0 {
			m.dbState.CurrentDBTag = tag
			m.refresh()
			return m
		}
	}
	m.goToInbox()
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

// effectivePriority returns the sort key for a row's priority:
// explicit priorities 1-5 sort first; 0 (unset) sorts last.
func effectivePriority(p int) int {
	if p == 0 {
		return 6
	}
	return p
}

func (m *model) rebuildRows() {
	m.rowItems = nil

	for _, row := range m.dbState.CurrentDBRows {
		if m.hideDone && row.Done {
			continue
		}
		m.rowItems = append(m.rowItems, rowItem{row: row})
	}
	// Sort direct rows: not-done before done, then by priority, then by rank.
	slices.SortStableFunc(m.rowItems, func(a, b rowItem) int {
		aDone, bDone := 0, 0
		if a.row.Done {
			aDone = 1
		}
		if b.row.Done {
			bDone = 1
		}
		if n := cmp.Compare(aDone, bDone); n != 0 {
			return n
		}
		if n := cmp.Compare(effectivePriority(a.row.Priority), effectivePriority(b.row.Priority)); n != 0 {
			return n
		}
		return cmp.Compare(a.row.Rank, b.row.Rank)
	})
	for _, refTag := range m.dbState.SortedRefTagsKeys {
		m.assignTagShortcut(refTag)
		for _, row := range m.dbState.CurrentDBRefs[refTag] {
			if m.hideDone && row.Done {
				continue
			}
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
	w := max(m.textW()-1-ansi.StringWidth(prompt)-ansi.StringWidth(escHint), 1)
	m.textInput.Width = w
}

// afterCursorRank returns the rank at which a new row should be inserted so
// that it appears immediately after the cursor. When the cursor is on a ref
// row (or the list is empty) the row is appended after all direct rows.
func (m *model) afterCursorRank() int {
	if m.cursor < len(m.rowItems) && !m.rowItems[m.cursor].isRef {
		return m.rowItems[m.cursor].row.Rank + 1
	}
	n := 0
	for _, it := range m.rowItems {
		if !it.isRef {
			n++
		}
	}
	return n
}

// makeRoomForRank ensures no existing row with the given priority occupies
// targetRank. If one does, all same-priority rows with rank >= targetRank are
// shifted up by one (in descending order to avoid transient conflicts).
func (m *model) makeRoomForRank(priority, targetRank int) {
	occupied := false
	for _, it := range m.rowItems {
		if !it.isRef && it.row.Priority == priority && it.row.Rank == targetRank {
			occupied = true
			break
		}
	}
	if !occupied {
		return
	}
	type rowShift struct {
		id   int64
		rank int
	}
	var toShift []rowShift
	for _, it := range m.rowItems {
		if !it.isRef && it.row.Priority == priority && it.row.Rank >= targetRank {
			toShift = append(toShift, rowShift{it.row.ID, it.row.Rank})
		}
	}
	slices.SortFunc(toShift, func(a, b rowShift) int {
		return cmp.Compare(b.rank, a.rank) // descending to avoid conflicts
	})
	for _, r := range toShift {
		_ = m.dbState.UpdateRowRank(r.id, r.rank+1)
	}
}

// rowTagContext returns the tag ID and name that owns item: the current tag
// for direct rows, the ref tag for reference rows.
func (m *model) rowTagContext(item rowItem) (int64, string) {
	if item.isRef {
		return item.refTag.ID, item.refTag.Name
	}
	return m.dbState.CurrentDBTag.ID, m.dbState.CurrentDBTag.Name
}

// collectTargets returns the rowItems to operate on: selected rows if any are
// marked, otherwise just the cursor row.
func (m *model) collectTargets() []rowItem {
	if len(m.selectedRows) > 0 {
		var targets []rowItem
		for _, it := range m.rowItems {
			if !it.isRef && m.selectedRows[it.row.ID] {
				targets = append(targets, it)
			}
		}
		if len(targets) > 0 {
			return targets
		}
	}
	return []rowItem{m.rowItems[m.cursor]}
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
		var rowID int64
		if m.cursor < len(m.rowItems) {
			rowID = m.rowItems[m.cursor].row.ID
		}
		m.tagStack = append(m.tagStack, tagStackEntry{tagName: m.dbState.CurrentDBTag.Name, rowID: rowID})
	}
	m.dbState.CurrentDBTag = tag
	m.cursor = 0
	m.lineOffset = 0
	m.selectedRows = make(map[int64]bool)
	m.refresh()
}

func (m *model) goToInbox() {
	tag, err := m.dbState.AddTag("inbox")
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
	entry := m.tagStack[l-1]
	m.tagStack = m.tagStack[:l-1]

	tag, err := m.dbState.AddTag(entry.tagName)
	if err != nil {
		m.setErr(err.Error())
		return
	}
	if tag.ID != m.dbState.CurrentDBTag.ID {
		_ = m.dbState.DeleteTagIfEmpty(m.dbState.CurrentDBTag.ID)
	}
	m.dbState.CurrentDBTag = tag
	m.cursor = 0
	m.lineOffset = 0
	m.refresh()
	m.positionCursor(entry.rowID)
	m.status = ""
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
	if tag := m.resolveUndoTag(entry); tag.ID != 0 && tag.ID != m.dbState.CurrentDBTag.ID {
		var rowID int64
		if m.cursor < len(m.rowItems) {
			rowID = m.rowItems[m.cursor].row.ID
		}
		m.tagStack = append(m.tagStack, tagStackEntry{tagName: m.dbState.CurrentDBTag.Name, rowID: rowID})
		m.dbState.CurrentDBTag = tag
		m.cursor = 0
		m.lineOffset = 0
	}
	m.setStatus("undid: " + entry.desc)
	m.refresh()
	if rowID != 0 {
		m.positionCursor(rowID)
	} else if entry.postUndoCursorRankSet {
		m.positionCursorAtRank(entry.postUndoCursorRank)
	}
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
	if tag := m.resolveUndoTag(entry); tag.ID != 0 && tag.ID != m.dbState.CurrentDBTag.ID {
		var rowID int64
		if m.cursor < len(m.rowItems) {
			rowID = m.rowItems[m.cursor].row.ID
		}
		m.tagStack = append(m.tagStack, tagStackEntry{tagName: m.dbState.CurrentDBTag.Name, rowID: rowID})
		m.dbState.CurrentDBTag = tag
		m.cursor = 0
		m.lineOffset = 0
	}
	m.setStatus("redid: " + entry.desc)
	m.refresh()
	m.positionCursor(rowID)
}

// clampCursorToDirectRows moves the cursor back to the last non-ref row if it
// is currently sitting on a ref row (e.g. after the last direct row was deleted).
func (m *model) clampCursorToDirectRows() {
	if m.cursor < len(m.rowItems) && !m.rowItems[m.cursor].isRef {
		return
	}
	for i := m.cursor - 1; i >= 0; i-- {
		if !m.rowItems[i].isRef {
			m.cursor = i
			m.clampLineOffset()
			return
		}
	}
}

// positionCursorAtRank moves the cursor to the given rank (clamped to valid range).
func (m *model) positionCursorAtRank(rank int) {
	if len(m.rowItems) == 0 {
		return
	}
	if rank < 0 {
		rank = 0
	}
	if rank >= len(m.rowItems) {
		rank = len(m.rowItems) - 1
	}
	m.cursor = rank
	m.clampLineOffset()
}

// positionCursor moves the cursor to the row with the given ID, if present.
func (m *model) positionCursor(rowID int64) {
	if rowID == 0 {
		return
	}
	for i, item := range m.rowItems {
		if item.row.ID == rowID {
			m.cursor = i
			m.clampLineOffset()
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

// tagCompletePrefix returns the partial text after the last '[[' if the input
// looks like an open tag reference (no matching ']]' yet), else ("", false).
func tagCompletePrefix(val string) (string, bool) {
	idx := strings.LastIndex(val, "[[")
	if idx < 0 {
		return "", false
	}
	after := val[idx+2:]
	if strings.Contains(after, "]]") {
		return "", false
	}
	return after, true
}

func (m *model) updateTagComplete() {
	prefix, ok := tagCompletePrefix(m.textInput.Value())
	if !ok {
		m.acActive = false
		return
	}
	m.acActive = true
	f := strings.ToLower(prefix)
	m.acTags = nil
	for _, tag := range m.dbState.AllDBTags {
		if strings.Contains(strings.ToLower(tag.Name), f) {
			m.acTags = append(m.acTags, tag)
		}
	}
	if m.acCursor >= len(m.acTags) {
		m.acCursor = 0
	}
}

func (m *model) completeTag() {
	if !m.acActive || len(m.acTags) == 0 {
		return
	}
	tag := m.acTags[m.acCursor]
	val := m.textInput.Value()
	idx := strings.LastIndex(val, "[[")
	if idx < 0 {
		return
	}
	m.textInput.SetValue(val[:idx] + "[[" + tag.Name + "]]")
	m.textInput.CursorEnd()
	m.acActive = false
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

func (m *model) updateSearchResults() {
	q := strings.ToLower(m.searchInput.Value())
	m.searchResults = nil
	for _, sr := range m.allSearchRows {
		if q == "" {
			m.searchResults = append(m.searchResults, sr)
			continue
		}
		lower := strings.ToLower(sr.row.Text)
		byteIdx := strings.Index(lower, q)
		if byteIdx < 0 {
			continue
		}
		// Convert byte index to rune index.
		sr.matchStart = len([]rune(lower[:byteIdx]))
		sr.matchEnd = sr.matchStart + len([]rune(q))
		m.searchResults = append(m.searchResults, sr)
	}
	if m.searchCursor >= len(m.searchResults) {
		m.searchCursor = 0
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
	case doneAnimTickMsg:
		if m.animRowID != 0 {
			runes := []rune(m.animText)
			step := max(1, len(runes)/10)
			m.animPos += step
			if m.animPos >= len(runes) {
				m.animRowID = 0
				m.animText = ""
				m.animPos = 0
				m.refresh()
				m.clampCursorToDirectRows()
			} else {
				cmd = doneAnimTick()
			}
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
		case modeHelp:
			m.mode = modeMain
		case modeSearch:
			cmd = m.handleSearchKey(msg)
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
		m.makeRoomForRank(0, rank)
		_ = m.dbState.UpdateRowRank(row.ID, rank)
		tagID, tagName, text := m.dbState.CurrentDBTag.ID, m.dbState.CurrentDBTag.Name, msg.text
		var latestID int64 = row.ID
		m.pushUndo(undoEntry{
			desc:                  "add row",
			tagID:                 tagID,
			tagName:               tagName,
			postUndoCursorRank:    max(0, rank-1),
			postUndoCursorRankSet: true,
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
			desc:                  "insert row",
			tagID:                 tagID,
			tagName:               tagName,
			postUndoCursorRank:    max(0, rank-1),
			postUndoCursorRankSet: true,
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
	case "pgup", "ctrl+b":
		m.pageCursor(-1)
	case "pgdown", "ctrl+f":
		m.pageCursor(+1)

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
		targets := m.collectTargets()
		type priorChange struct {
			id       int64
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
			desc:    fmt.Sprintf("set priority on %d row(s)", len(changes)),
			tagID:   tagID,
			tagName: tagName,
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
		m.selectedRows = make(map[int64]bool)
		m.refresh()

	case "i":
		m.status = ""
		m.goToInbox()

	case "ctrl+t":
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
		targets := m.collectTargets()
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
			tagID, tagName := m.rowTagContext(it)
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
		m.clampCursorToDirectRows()

	case "D":
		if len(m.rowItems) == 0 {
			m.setErr("no row selected")
			break
		}
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
			desc:    fmt.Sprintf("toggle done on %d row(s)", len(changes)),
			tagID:   tagID,
			tagName: tagName,
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
	case modeHelp:
		return m.viewHelp()
	case modeSearch:
		return m.viewSearch()
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
	pad := max(
		// 4 = "─ " prefix + " ─..." suffix chars
		w-len(label)-4, 0)
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
		styleKey.Render("/") + " search",
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

// renderSearchMatch renders text with the rune range [matchStart, matchEnd)
// highlighted bold+yellow. bg, if non-empty, is applied to every span.
func renderSearchMatch(text string, matchStart, matchEnd int, bg string) string {
	matchStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3"))
	plainStyle := lipgloss.NewStyle()
	if bg != "" {
		matchStyle = matchStyle.Background(lipgloss.Color(bg))
		plainStyle = plainStyle.Background(lipgloss.Color(bg))
	}
	runes := []rune(text)
	if matchStart >= matchEnd || matchEnd > len(runes) {
		if bg != "" {
			return plainStyle.Render(text)
		}
		return text
	}
	pre := string(runes[:matchStart])
	mid := string(runes[matchStart:matchEnd])
	post := string(runes[matchEnd:])
	var sb strings.Builder
	if bg != "" {
		sb.WriteString(plainStyle.Render(pre))
	} else {
		sb.WriteString(pre)
	}
	sb.WriteString(matchStyle.Render(mid))
	if bg != "" {
		sb.WriteString(plainStyle.Render(post))
	} else {
		sb.WriteString(post)
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
		// prefix = indent + priority tag (2 cols: "X " or "- ") + bullet (2 cols: "• ")
		prefixW := ansi.StringWidth(indent) + 2 + 2
		contIndent := strings.Repeat(" ", prefixW)
		noteSuffixW := 0
		if hasNote {
			noteSuffixW = 2 // " ✎"
		}
		textW := max(innerW-prefixW-noteSuffixW, 1)
		chunks := wrapText(item.row.Text, textW)

		pri := item.row.Priority
		done := item.row.Done
		animating := m.animRowID != 0 && item.row.ID == m.animRowID
		// Build priority tag: "[X] " (4 cols) or "    " when unset/ref.
		priTag := styleDim.Render("-") + " "
		if !item.isRef && pri >= 1 && pri <= 5 {
			priTag = stylePriority[pri].Render(fmt.Sprintf("%d", pri)) + " "
		}
		if i == m.cursor {
			var bullet string
			if marked {
				bullet = styleMarked.Render("◆") + " "
			} else if done && !animating {
				bullet = styleDone.Render("✓") + " "
			} else {
				bullet = "• "
			}
			for ci, chunk := range chunks {
				isLast := ci == len(chunks)-1
				pfx := contIndent
				if ci == 0 {
					pfx = indent + priTag + bullet
				}
				noteSuffix := ""
				if isLast && hasNote {
					noteSuffix = styleSelected.Render(" ") + styleDim.Background(lipgloss.Color("240")).Render("✎")
				}
				var text string
				switch {
				case animating:
					runes := []rune(chunk)
					cutoff := m.animPos
					if cutoff > len(runes) {
						cutoff = len(runes)
					}
					doneStyle := styleDone.Background(lipgloss.Color("240"))
					text = doneStyle.Render(string(runes[:cutoff])) + styleSelected.Render(string(runes[cutoff:]))
				case done:
					text = styleDone.Background(lipgloss.Color("240")).Render(chunk)
				default:
					text = m.renderRowText(chunk, "240")
				}
				raw := pfx + text + noteSuffix
				if gap := innerW - ansi.StringWidth(raw); gap > 0 {
					raw += styleSelected.Render(strings.Repeat(" ", gap))
				}
				lines = append(lines, raw)
			}
		} else {
			var firstBullet string
			if marked {
				firstBullet = styleMarked.Render("◆")
			} else if done && !animating {
				firstBullet = styleDone.Render("✓")
			} else {
				firstBullet = "•"
			}
			for ci, chunk := range chunks {
				isLast := ci == len(chunks)-1
				noteSuffix := ""
				if isLast && hasNote {
					noteSuffix = " " + styleDim.Render("✎")
				}
				pfx := contIndent
				bullet := " "
				if ci == 0 {
					pfx = indent + priTag
					bullet = firstBullet
				}
				var text string
				switch {
				case animating:
					runes := []rune(chunk)
					cutoff := m.animPos
					if cutoff > len(runes) {
						cutoff = len(runes)
					}
					text = styleDone.Render(string(runes[:cutoff])) + string(runes[cutoff:])
				case done:
					text = styleDone.Render(chunk)
				default:
					text = m.renderRowText(chunk, "")
				}
				lines = append(lines, pfx+bullet+" "+text+noteSuffix)
			}
		}
	}
	return lines
}

// cursorLineRange returns the first and last line index (0-based) within
// mainContentLines that belong to the cursor row.
func (m model) cursorLineRange(innerW int) (first, last int) {
	line := 0
	prevRefTag := db.Tag{}
	for i, item := range m.rowItems {
		if item.isRef && item.refTag != prevRefTag {
			if prevRefTag.ID == 0 {
				line += 2
			}
			line++
			prevRefTag = item.refTag
		}
		indent := " "
		if item.isRef {
			indent = "   "
		}
		prefixW := ansi.StringWidth(indent) + 2 + 2 // priority tag (2) + bullet (2)
		noteSuffixW := 0
		if !item.isRef && item.row.Note != "" {
			noteSuffixW = 2
		}
		textW := max(innerW-prefixW-noteSuffixW, 1)
		nLines := len(wrapText(item.row.Text, textW))
		if i == m.cursor {
			return line, line + nLines - 1
		}
		line += nLines
	}
	return 0, 0
}

// pageSize returns the number of visible content lines (excluding header).
func (m model) pageSize() int { return max(m.lineCount(2)-2, 1) }

// pageCursor moves the cursor forward (dir=+1) or backward (dir=-1) by one
// page worth of content lines, then clamps the scroll offset.
func (m *model) pageCursor(dir int) {
	if len(m.rowItems) == 0 {
		return
	}
	tw := m.textW()
	pg := m.pageSize()

	// Build a slice of cumulative first-line indices per item, mirroring
	// cursorLineRange logic so we can binary-search by line count.
	type itemLine struct{ first, last int }
	items := make([]itemLine, len(m.rowItems))
	line := 0
	prevRefTag := db.Tag{}
	for i, item := range m.rowItems {
		if item.isRef && item.refTag != prevRefTag {
			if prevRefTag.ID == 0 {
				line += 2
			}
			line++
			prevRefTag = item.refTag
		}
		indent := " "
		if item.isRef {
			indent = "   "
		}
		prefixW := ansi.StringWidth(indent) + 2
		noteSuffixW := 0
		if !item.isRef && item.row.Note != "" {
			noteSuffixW = 2
		}
		textW := max(tw-prefixW-noteSuffixW, 1)
		nLines := len(wrapText(item.row.Text, textW))
		items[i] = itemLine{line, line + nLines - 1}
		line += nLines
	}

	cFirst := items[m.cursor].first
	target := cFirst + dir*pg // target first-line of new cursor row

	// Find the item whose first line is closest to target in the given direction.
	best := m.cursor
	if dir > 0 {
		for i := m.cursor + 1; i < len(items); i++ {
			best = i
			if items[i].first >= target {
				break
			}
		}
	} else {
		for i := m.cursor - 1; i >= 0; i-- {
			best = i
			if items[i].first <= target {
				break
			}
		}
	}
	m.cursor = best
	m.clampLineOffset()
}

// clampLineOffset adjusts m.lineOffset so the cursor row stays visible.
func (m *model) clampLineOffset() {
	tw := m.textW()
	visible := m.lineCount(2) - 2 // subtract 2 header lines
	if visible <= 0 {
		return
	}
	cFirst, cLast := m.cursorLineRange(tw)
	if cLast >= m.lineOffset+visible {
		m.lineOffset = cLast - visible + 1
	}
	if cFirst < m.lineOffset {
		m.lineOffset = cFirst
	}
	if m.lineOffset < 0 {
		m.lineOffset = 0
	}
}

func (m model) viewMain() string {
	tw := m.textW()
	lc := m.lineCount(2) // 2 lines below box: status + hints

	header := []string{
		styleHeader.Render(" " + m.dbState.CurrentDBTag.Name),
		rule(tw, ""),
	}
	allLines := m.mainContentLines(tw)
	off := min(m.lineOffset, len(allLines))
	content := fitLines(allLines[off:], lc-len(header), tw)

	box := m.bordered(2).Render(
		boxContent(header, content, tw),
	)
	return box + "\n" + m.statusLine() + "\n" + m.hints()
}

func (m model) viewInput() string {
	tw := m.textW()

	prompt := inputPrompts[m.pendingAction] + ": "
	inputLine := " " + styleKey.Render(prompt) +
		m.textInput.View() +
		styleDim.Render("  esc to cancel")

	if m.acActive {
		const maxVisible = 6
		var acLines []string
		for i, tag := range m.acTags {
			if i >= maxVisible {
				break
			}
			line := "  " + tag.Name
			if i == m.acCursor {
				line = styleSelected.Render(line)
			}
			acLines = append(acLines, line)
		}
		if len(m.acTags) == 0 {
			acLines = append(acLines, styleDim.Render("  (no matching tags)"))
		}
		hint := " " + styleDim.Render(
			styleKey.Render("↑↓")+" navigate  "+
				styleKey.Render("tab/enter")+" select  "+
				styleKey.Render("esc")+" dismiss",
		)
		// acRows = suggestion lines + hint line + input line
		acRows := len(acLines) + 1 + 1
		lc := m.lineCount(acRows)
		header := []string{
			styleHeader.Render(" " + m.dbState.CurrentDBTag.Name),
			rule(tw, ""),
		}
		allLines := m.mainContentLines(tw)
		off := min(m.lineOffset, len(allLines))
		content := fitLines(allLines[off:], lc-len(header), tw)
		box := m.bordered(acRows).Render(boxContent(header, content, tw))
		return box + "\n" + strings.Join(acLines, "\n") + "\n" + inputLine + "\n" + hint
	}

	lc := m.lineCount(2)
	header := []string{
		styleHeader.Render(" " + m.dbState.CurrentDBTag.Name),
		rule(tw, ""),
	}
	allLines := m.mainContentLines(tw)
	off := min(m.lineOffset, len(allLines))
	content := fitLines(allLines[off:], lc-len(header), tw)
	box := m.bordered(2).Render(boxContent(header, content, tw))
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

func (m model) viewSearch() string {
	tw := m.textW()
	lc := m.lineCount(2)

	header := []string{
		styleHeader.Render(" Search"),
		rule(tw, ""),
		" / " + m.searchInput.View(),
		rule(tw, ""),
	}

	var resultLines []string
	prevTag := db.Tag{}
	for i, sr := range m.searchResults {
		if sr.tag != prevTag {
			resultLines = append(resultLines, rule(tw, sr.tag.Name))
			prevTag = sr.tag
		}
		bg := ""
		if i == m.searchCursor {
			bg = "240"
		}
		pfx := "   • "
		if bg != "" {
			pfx = lipgloss.NewStyle().Background(lipgloss.Color(bg)).Render(pfx)
		}
		line := pfx + renderSearchMatch(sr.row.Text, sr.matchStart, sr.matchEnd, bg)
		resultLines = append(resultLines, line)
	}
	if len(m.searchResults) == 0 {
		if m.searchInput.Value() == "" {
			resultLines = append(resultLines, styleDim.Render(" (type to search all items)"))
		} else {
			resultLines = append(resultLines, styleDim.Render(" (no results)"))
		}
	}

	content := fitLines(resultLines, lc-len(header), tw)
	box := m.bordered(2).Render(boxContent(header, content, tw))
	navHints := " " + styleDim.Render(
		styleKey.Render("↑↓")+" navigate  "+
			styleKey.Render("enter")+" go to item  "+
			styleKey.Render("esc")+" cancel",
	)
	return box + "\n" + m.statusLine() + "\n" + navHints
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
		"   i          go to inbox",
		"   n          new tag",
		"   r          rename current tag",
		"   t          tag selector (type to filter)",
		"   /          fuzzy search all items",
		"   \\          toggle show/hide done items",
		"   ctrl-t     back in tag stack",
		"   1-9        jump to tag reference",
		"",
		" " + styleKey.Render("[Rows]"),
		"   j / k      move cursor down / up",
		"   enter      follow tag link in selected row",
		"   o          add row below cursor",
		"   O          insert row above cursor",
		"   space       toggle row in/out of selection",
		"   e          edit selected row",
		"   N          add/edit note on selected row",
		"   d          cut selected row (or all marked)",
		"   D          mark done (or all marked)",
		"   y          yank selected row",
		"   p / P      paste below / above cursor",
		"   J / K      move row down / up (within priority group)",
		"   !@#$%      set priority 1-5 (repeat to clear), ) to clear",
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

func dbPath() (string, error) {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	dir := filepath.Join(dataHome, "exocortex")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "exocortex.db"), nil
}

func main() {
	path, err := dbPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "resolve db path:", err)
		os.Exit(1)
	}
	exoDB := &db.ExoDB{}
	if err := exoDB.Open(path); err != nil {
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
