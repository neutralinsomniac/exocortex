package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/neutralinsomniac/exocortex/db"
)

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
	case modeNotePopup:
		return m.viewNotePopup()
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
		styleKey.Render("J/K") + " move",
		styleKey.Render("o/O") + " add",
		styleKey.Render("d") + " cut",
		styleKey.Render("p") + " paste",
		styleKey.Render("D") + " done",
		styleKey.Render("e") + " edit",
		styleKey.Render("u/U") + " undo/redo",
		styleKey.Render("/") + " search",
		styleKey.Render("t") + " tags",
		styleKey.Render("ctrl-t, b") + " back",
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
		if pri >= 1 && pri <= 5 {
			priTag = stylePriority[pri].Render(fmt.Sprintf("%d", pri)) + " "
		}
		if i == m.cursor {
			var bullet string
			if marked {
				bullet = styleMarked.Render("◆") + " "
			} else if done && !animating {
				bullet = styleDoneCheck.Render("✓") + " "
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
				firstBullet = styleDoneCheck.Render("✓")
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
		prefixW := ansi.StringWidth(indent) + 2 + 2 // priority tag (2) + bullet (2)
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

func (m model) viewNotePopup() string {
	bg := m.viewMain()

	// Leave a 4-col margin on each side and a 2-row margin top/bottom.
	// border(1 each side) + padding(1 each side) = 4 cols / 2 rows overhead.
	noteContentW := m.width - 8 - 4   // total width - margins - border/padding
	noteContentH := m.height - 4 - 2  // total height - margins - border/padding
	// Reserve 4 lines for: header, rule, blank, close button.
	textH := max(noteContentH-4, 1)

	// Word-wrap each paragraph in the note separately.
	var noteLines []string
	for _, para := range strings.Split(m.notePopupRow.Note, "\n") {
		if para == "" {
			noteLines = append(noteLines, "")
			continue
		}
		noteLines = append(noteLines, wrapText(para, noteContentW)...)
	}
	noteLines = fitLines(noteLines, textH, noteContentW)

	closeBtn := "[ close ]"
	closePad := strings.Repeat(" ", max((noteContentW-len(closeBtn))/2, 0))
	closeLine := closePad + styleKey.Render(closeBtn)

	rawLines := []string{
		styleHeader.Render(" Note"),
		rule(noteContentW, ""),
	}
	rawLines = append(rawLines, noteLines...)
	rawLines = append(rawLines, "", closeLine)

	// Pad every line to noteContentW so the box has a fixed, known width.
	for i, l := range rawLines {
		l = ansi.Truncate(l, noteContentW, "")
		if sw := ansi.StringWidth(l); sw < noteContentW {
			l += strings.Repeat(" ", noteContentW-sw)
		}
		rawLines[i] = l
	}

	popupBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(0, 1).
		Render(strings.Join(rawLines, "\n"))

	return overlayCenter(bg, popupBox, m.width, m.height)
}

// overlayCenter renders popup centered over bg (a newline-separated string of
// bgH lines, each bgW visual columns wide).
func overlayCenter(bg, popup string, bgW, bgH int) string {
	bgLines := strings.Split(bg, "\n")
	popLines := strings.Split(popup, "\n")

	popH := len(popLines)
	popW := 0
	for _, l := range popLines {
		if w := ansi.StringWidth(l); w > popW {
			popW = w
		}
	}

	startY := (bgH - popH) / 2
	startX := (bgW - popW) / 2
	if startX < 0 {
		startX = 0
	}

	for i, popLine := range popLines {
		y := startY + i
		if y < 0 || y >= len(bgLines) {
			continue
		}
		bgLines[y] = overlayLine(bgLines[y], popLine, startX)
	}
	return strings.Join(bgLines, "\n")
}

// overlayLine replaces the visual columns [startX, startX+width(overlay)) in bg
// with overlay, padding bg with spaces if it is shorter than startX.
func overlayLine(bg, overlay string, startX int) string {
	left := ansi.Truncate(bg, startX, "")
	if lw := ansi.StringWidth(left); lw < startX {
		left += strings.Repeat(" ", startX-lw)
	}
	right := ansi.TruncateLeft(bg, startX+ansi.StringWidth(overlay), "")
	return left + overlay + right
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
		"   \\         toggle show/hide done items",
		"   ctrl-t, b  back in tag stack",
		"   1-9        jump to tag reference",
		"",
		" " + styleKey.Render("[Rows]"),
		"   j / k      move cursor down / up",
		"   enter      follow tag link in selected row",
		"   o          add row below cursor",
		"   O          insert row above cursor",
		"   space      toggle row in/out of selection",
		"   ;          clear selection",
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
