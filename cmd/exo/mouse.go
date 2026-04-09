package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/neutralinsomniac/exocortex/db"
)

func (m *model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil
	}

	// In the note popup, only dismiss if the close button was clicked.
	if m.mode == modeNotePopup {
		if m.notePopupCloseBtnHit(msg.X, msg.Y) {
			m.mode = modeMain
		}
		return nil
	}

	// Screen layout (0-indexed from top):
	//   y=0:            top border
	//   y=1:            header (tag name)
	//   y=2:            rule separator
	//   y=3..height-4:  mainContentLines
	//   y=height-3:     bottom border
	//   y=height-2:     status line
	//   y=height-1:     hints
	if msg.Y < 3 || msg.Y > m.height-4 {
		return nil
	}

	// Map screen y to 0-based index into mainContentLines (accounting for scroll).
	contentLine := (msg.Y - 3) + m.lineOffset

	itemIdx, chunkIdx, found := m.rowItemAtLine(contentLine)
	if !found {
		return nil
	}

	// Move cursor to clicked row.
	m.cursor = itemIdx
	m.clampLineOffset()

	// x=0: left border, x=1: left padding, x=2+: content.
	xInContent := msg.X - 2
	if xInContent < 0 {
		return nil
	}

	item := m.rowItems[itemIdx]
	tw := m.textW()
	indent := " "
	if item.isRef {
		indent = "   "
	}
	// indent + priTag(2 visual cols) + bullet+space(2 visual cols)
	prefixW := ansi.StringWidth(indent) + 2 + 2

	if xInContent < prefixW {
		return nil
	}

	hasNote := !item.isRef && item.row.Note != ""
	noteSuffixW := 0
	if hasNote {
		noteSuffixW = 2
	}
	textW := max(tw-prefixW-noteSuffixW, 1)
	chunks := wrapText(item.row.Text, textW)
	if chunkIdx >= len(chunks) {
		return nil
	}

	// Check if click is past the actual rendered text on the last chunk of a
	// note row. The ✎ appears immediately after the text; boxContent pads the
	// rest of the line with spaces. Any click past the text (on the ✎, the
	// space before it, or the padding after it) opens the note popup.
	isLastChunk := chunkIdx == len(chunks)-1
	if hasNote && isLastChunk {
		renderedChunkW := ansi.StringWidth(m.renderRowText(chunks[chunkIdx], ""))
		if xInContent >= prefixW+renderedChunkW && xInContent < prefixW+renderedChunkW+noteSuffixW+2 {
			m.notePopupRow = item.row
			m.mode = modeNotePopup
			return nil
		}
	}

	// Check if click is on a tag reference.
	textCol := xInContent - prefixW
	tagName, tagFound := m.tagAtVisualCol(chunks[chunkIdx], textCol)
	if !tagFound {
		return nil
	}

	tag, err := m.dbState.GetTagByName(tagName)
	if err != nil || tag.ID == 0 {
		return nil
	}
	m.status = ""
	m.switchTag(tag)
	return nil
}

// notePopupCloseBtnHit reports whether screen coordinate (x, y) falls on the
// "[ close ]" button of the note popup. The geometry mirrors viewNotePopup.
func (m model) notePopupCloseBtnHit(x, y int) bool {
	noteContentW := m.width - 12
	noteContentH := m.height - 6 // border(1)*2 + margin(2)*2 = 6
	popupH := noteContentH + 2   // add top+bottom borders
	startY := (m.height - popupH) / 2
	startX := (m.width - (noteContentW + 4)) / 2 // noteContentW + padding(2) + border(2)

	closeBtn := "[ close ]"
	closePad := max((noteContentW-len(closeBtn))/2, 0)

	// Close button is the last content line: rawLines[noteContentH-1].
	// In the rendered box: row 0=top border, rows 1..noteContentH=content.
	btnY := startY + noteContentH // = startY + 1 + (noteContentH-1)
	btnX0 := startX + 2 + closePad
	btnX1 := btnX0 + len(closeBtn) - 1

	return y == btnY && x >= btnX0 && x <= btnX1
}

// rowItemAtLine returns the rowItem index and wrapped-chunk index for the given
// 0-based content line index (into mainContentLines). Returns found=false for
// separator/header lines (blank line, rule, ref-section header).
func (m model) rowItemAtLine(targetLine int) (itemIdx, chunkIdx int, found bool) {
	line := 0
	prevRefTag := db.Tag{}
	tw := m.textW()
	for i, item := range m.rowItems {
		if item.isRef && item.refTag != prevRefTag {
			if prevRefTag.ID == 0 {
				line += 2 // blank line + "─ References ─" rule
			}
			line++ // ref-section header "tagname(N)"
			prevRefTag = item.refTag
		}
		indent := " "
		if item.isRef {
			indent = "   "
		}
		prefixW := ansi.StringWidth(indent) + 2 + 2
		noteSuffixW := 0
		if !item.isRef && item.row.Note != "" {
			noteSuffixW = 2
		}
		textW := max(tw-prefixW-noteSuffixW, 1)
		nLines := len(wrapText(item.row.Text, textW))
		if targetLine >= line && targetLine < line+nLines {
			return i, targetLine - line, true
		}
		line += nLines
	}
	return 0, 0, false
}

// tagAtVisualCol returns the tag name if the given visual column offset (from
// the start of the row's text content, after the prefix) falls on a [[tagname]]
// reference. The reference is displayed as "tagname(N)" where N is the numeric
// shortcut, so widths are computed against that rendered form.
func (m model) tagAtVisualCol(text string, col int) (tagName string, found bool) {
	pos := 0
	remaining := text
	for {
		loc := tagRe.FindStringIndex(remaining)
		if loc == nil {
			return "", false
		}
		pos += ansi.StringWidth(remaining[:loc[0]])

		name := remaining[loc[0]+2 : loc[1]-2]
		var rendered string
		if n, ok := m.tagNameToNum[name]; ok {
			rendered = fmt.Sprintf("%s(%d)", name, n)
		} else {
			rendered = name
		}
		renderedW := ansi.StringWidth(rendered)

		if col >= pos && col < pos+renderedW {
			return name, true
		}
		pos += renderedW
		remaining = remaining[loc[1]:]
	}
}
