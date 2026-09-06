package main

import (
	"strings"
)

// ── line operations ───────────────────────────────────────────────────────────

// edLineRange is the block of lines an operation applies to: the selection when
// there is one, otherwise the caret's line. A selection resting at column 0 of
// its last line does not reach that line, which is how a downward drag with
// Shift+Down usually reads.
func (s *edState) edLineRange() (from, to int) {
	if !s.hasSel {
		return s.cy, s.cy
	}
	_, ay, bx, by := s.selRange()
	if by > ay && bx == 0 {
		by--
	}
	return ay, max(ay, by)
}

// sealedWithin reports whether function focus has locked any line in a range.
func (s *edState) sealedWithin(from, to int) bool {
	for i := from; i <= to; i++ {
		if s.focus.sealed(i) {
			return true
		}
	}
	return false
}

// duplicateLines copies the caret's line, or the selected block, and inserts
// the copy directly below it. The caret follows the copy, so pressing it twice
// gives two copies rather than the same one again.
func (s *edState) duplicateLines() {
	from, to := s.edLineRange()
	if s.sealedWithin(from, to) {
		s.refuseEdit()
		return
	}

	s.beginEdit(false)
	block := append([]string(nil), s.lines[from:to+1]...)
	out := make([]string, 0, len(s.lines)+len(block))
	out = append(out, s.lines[:to+1]...)
	out = append(out, block...)
	out = append(out, s.lines[to+1:]...)
	s.lines = out

	span := to - from + 1
	s.cy += span
	if s.hasSel {
		s.selY += span
	}
	s.clampCursor()
	s.touch()
	s.ensureVisible()
}

// moveLines shifts the caret's line, or the selected block, one line up or
// down, carrying the selection with it. It refuses to step outside the view or
// across a line function focus has sealed, so a function cannot be turned
// inside out from within it.
func (s *edState) moveLines(delta int) {
	from, to := s.edLineRange()
	swap := from - 1
	if delta > 0 {
		swap = to + 1
	}
	if swap < s.viewTop() || swap > s.viewBottom() {
		return
	}
	if s.sealedWithin(min(from, swap), max(to, swap)) {
		s.refuseEdit()
		return
	}

	s.beginEdit(false)
	block := append([]string(nil), s.lines[from:to+1]...)
	rest := append(s.lines[:from:from], s.lines[to+1:]...)

	at := from + delta
	out := make([]string, 0, len(s.lines))
	out = append(out, rest[:at]...)
	out = append(out, block...)
	out = append(out, rest[at:]...)
	s.lines = out

	s.cy += delta
	if s.hasSel {
		s.selY += delta
	}
	s.clampCursor()
	s.touch()
	s.ensureVisible()
}

// ── indentation ───────────────────────────────────────────────────────────────

// edIndentSteps are the widths the editor will detect or cycle through.
var edIndentSteps = []int{2, 4, 8}

// edDetectIndent guesses a file's indent step from the jumps in its own leading
// whitespace: every time a line is indented deeper than the one before it, that
// difference is a vote. The most voted-for width wins, so a file that is
// already consistent keeps its own convention.
func edDetectIndent(lines []string) int {
	votes := map[int]int{}
	previous := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if strings.HasPrefix(line, "\t") {
			continue // tab-indented; nothing to measure in spaces
		}
		if step := indent - previous; step > 0 && step <= 8 {
			votes[step]++
		}
		previous = indent
	}

	best, bestVotes := 0, 0
	for _, step := range edIndentSteps {
		if votes[step] > bestVotes {
			best, bestVotes = step, votes[step]
		}
	}
	if best == 0 {
		return edDefaultIndent
	}
	return best
}

// indentPad is one indent step's worth of spaces.
func (s *edState) indentPad() string {
	return strings.Repeat(" ", s.indentWidth)
}

// ── bracket pairing ───────────────────────────────────────────────────────────

// edAutoClose controls whether typing an opening bracket or quote also writes
// its partner. It is a taste that people hold strongly either way, so it sits
// in the Options menu.
var edAutoClose = true

// edPairFor returns the closing character for an opening one.
func edPairFor(r rune) (rune, bool) {
	switch r {
	case '(':
		return ')', true
	case '[':
		return ']', true
	case '{':
		return '}', true
	case '"':
		return '"', true
	case '\'':
		return '\'', true
	}
	return 0, false
}

// edIsCloser reports whether a character closes a pair.
func edIsCloser(r rune) bool {
	switch r {
	case ')', ']', '}', '"', '\'':
		return true
	}
	return false
}

// edTypeRune inserts one typed character, pairing brackets and quotes as it
// goes. It reports the text that ended up in the buffer, which the assistance
// triggers read to decide whether to offer completions or a signature.
func edTypeRune(s *edState, c rune) string {
	if !edAutoClose {
		s.insert(string(c))
		return string(c)
	}

	next := rune(0)
	if line := s.runes(s.cy); s.cx < len(line) {
		next = line[s.cx]
	}

	// Typing the closer that is already sitting there steps over it rather
	// than leaving a second one behind.
	if edIsCloser(c) && next == c && !s.hasSel {
		s.cx++
		s.goalX = s.cx
		return ""
	}

	closer, opens := edPairFor(c)
	switch {
	case !opens:
	case s.hasSel:
		// Wrap the selection instead of replacing it.
		text := s.selText()
		s.insert(string(c) + text + string(closer))
		return string(c)
	case (c == '"' || c == '\'') && (edIsIdentChar(next) || next == c):
		// An apostrophe inside a word is not the start of a string.
	default:
		s.insert(string(c) + string(closer))
		s.cx-- // sit between the pair
		s.goalX = s.cx
		return string(c)
	}

	s.insert(string(c))
	return string(c)
}

// edDeletePair reports whether the caret sits between an empty pair, so that
// backspace can take both halves at once.
func edDeletePair(s *edState) bool {
	if !edAutoClose || s.hasSel || s.cx == 0 {
		return false
	}
	line := s.runes(s.cy)
	if s.cx >= len(line) {
		return false
	}
	closer, opens := edPairFor(line[s.cx-1])
	return opens && line[s.cx] == closer
}

// edIndexOf returns the position of v in xs, or -1.
func edIndexOf(xs []int, v int) int {
	for i, x := range xs {
		if x == v {
			return i
		}
	}
	return -1
}
