package main

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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

	styleDone      = lipgloss.NewStyle().Faint(true).Strikethrough(true)
	styleDoneCheck = lipgloss.NewStyle().Faint(true)

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
	modeNotePopup
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

type syncResultMsg struct{ err error }

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
	// savedCursor is the cursor index at the time of the operation; restored on undo.
	savedCursor int
	// postUndoSelection restores a multi-selection after undo, if non-empty.
	postUndoSelection map[int64]bool
	// postUndoSelectionFn, if set, is called after undoFn runs to get the
	// selection to restore. Takes precedence over postUndoSelection.
	postUndoSelectionFn func() map[int64]bool
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
	animRowID int64  // row currently animating (0 = none)
	animText  string // original text of the animating row
	animPos   int    // strikethrough has drawn through this many runes
	undoStack []undoEntry
	redoStack []undoEntry

	// navigation
	cursor     int
	lineOffset int // scroll offset into mainContentLines
	mode       viewMode

	// note popup
	notePopupRow db.Row // row whose note is being shown; zero value = no popup

	// input mode
	textInput     textinput.Model
	pendingAction pendingAction
	pendingRow    db.Row
	pendingRank   int // rank at which the pending add/insert should land

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

// snapshotSelection returns a copy of selectedRows if non-empty, else nil.
// Used to capture the selection for undo restoration.
func (m *model) snapshotSelection() map[int64]bool {
	if len(m.selectedRows) == 0 {
		return nil
	}
	snap := make(map[int64]bool, len(m.selectedRows))
	for id, v := range m.selectedRows {
		snap[id] = v
	}
	return snap
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
	entry.savedCursor = m.cursor
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
	_, err := entry.undoFn(m.dbState.ExoDB)
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
	m.positionCursorAtRank(entry.savedCursor)
	if entry.postUndoSelectionFn != nil {
		if sel := entry.postUndoSelectionFn(); len(sel) > 0 {
			m.selectedRows = sel
		}
	} else if len(entry.postUndoSelection) > 0 {
		m.selectedRows = entry.postUndoSelection
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

// tagCompletePrefix returns the partial text after the last '[[' if the text
// before the cursor looks like an open tag reference (no matching ']]' yet), else ("", false).
func tagCompletePrefix(beforeCursor string) (string, bool) {
	idx := strings.LastIndex(beforeCursor, "[[")
	if idx < 0 {
		return "", false
	}
	after := beforeCursor[idx+2:]
	if strings.Contains(after, "]]") {
		return "", false
	}
	return after, true
}

func (m *model) updateTagComplete() {
	runes := []rune(m.textInput.Value())
	beforeCursor := string(runes[:m.textInput.Position()])
	prefix, ok := tagCompletePrefix(beforeCursor)
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
	runes := []rune(m.textInput.Value())
	pos := m.textInput.Position()
	beforeCursor := string(runes[:pos])
	afterCursor := string(runes[pos:])
	idx := strings.LastIndex(beforeCursor, "[[")
	if idx < 0 {
		return
	}
	closing := "]]"
	if strings.HasPrefix(afterCursor, "]]") {
		closing = ""
	}
	newVal := beforeCursor[:idx] + "[[" + tag.Name + closing + afterCursor
	m.textInput.SetValue(newVal)
	newPos := len([]rune(beforeCursor[:idx])) + 2 + len([]rune(tag.Name)) + 2
	m.textInput.SetCursor(newPos)
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
	case syncResultMsg:
		if msg.err != nil {
			m.setErr("sync: " + msg.err.Error())
		} else {
			m.setStatus("sync complete")
			m.refresh()
		}
	case editorDoneMsg:
		// Re-enable mouse after ExecProcess returns the terminal to us.
		// RestoreTerminal restores altscreen but not mouse mode.
		cmd = tea.Batch(m.handleEditorDone(msg), func() tea.Msg { return tea.EnableMouseCellMotion() })
	case tea.MouseMsg:
		if m.mode == modeMain || m.mode == modeNotePopup {
			cmd = m.handleMouse(msg)
		}
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
		case modeNotePopup:
			m.mode = modeMain
		}
	}
	return m, cmd
}
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

	p := tea.NewProgram(newModel(exoDB), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
