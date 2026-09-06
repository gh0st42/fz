package main

import (
	"fmt"
	"strings"
)

// ── search and replace ────────────────────────────────────────────────────────
//
// Matching follows Find: case-insensitive, and confined to whatever the view
// currently shows, so a replace-all inside function focus cannot reach past the
// function on screen. Replacements go in literally.

// edMatchAt reports whether the needle sits at a column, ignoring case.
func edMatchAt(line []rune, col int, needle []rune) bool {
	if col < 0 || col+len(needle) > len(line) {
		return false
	}
	return edIndexRunes(line[col:col+len(needle)], needle) == 0
}

// edSelectionIsMatch reports whether the selection is exactly one occurrence of
// the search term, which is what makes Replace act on it rather than skip past.
func edSelectionIsMatch(s *edState) bool {
	if !s.hasSel || s.findTerm == "" {
		return false
	}
	ax, ay, bx, by := s.selRange()
	if ay != by {
		return false
	}
	needle := []rune(strings.ToLower(s.findTerm))
	return bx-ax == len(needle) && edMatchAt([]rune(strings.ToLower(s.lines[ay])), ax, needle)
}

// edReplaceOnce swaps the highlighted match for the replacement and moves to
// the next one. With nothing matched it just finds, so the button walks through
// the file the way Find Next does.
func edReplaceOnce(s *edState) {
	if s.findTerm == "" {
		s.toast.Notify("No search term")
		return
	}
	if !edSelectionIsMatch(s) {
		edFind(s, false)
		return
	}
	if !s.mayEditHere() {
		return
	}

	s.beginEdit(false)
	s.insert(s.replaceTerm) // replaces the selection, which is the match
	s.ensureVisible()
	edFind(s, false)
}

// edReplaceAll swaps every occurrence in one undoable step and reports how many
// there were. Lines sealed by function focus are left alone.
func edReplaceAll(s *edState) {
	if s.findTerm == "" {
		s.toast.Notify("No search term")
		return
	}

	from, to := s.viewTop(), s.viewBottom()
	scope := "file"
	if s.replaceFrom >= 0 {
		from, to, scope = s.replaceFrom, s.replaceTo, "selection"
	}
	from, to = max(from, s.viewTop()), min(to, s.viewBottom())

	needle := []rune(strings.ToLower(s.findTerm))
	replacement := []rune(s.replaceTerm)

	// Survey first, so a run that changes nothing leaves no undo step behind.
	count := 0
	for i := from; i <= to; i++ {
		if s.focus.sealed(i) {
			continue
		}
		count += edCountMatches([]rune(strings.ToLower(s.lines[i])), needle)
	}
	if count == 0 {
		s.toast.Notify("Not found: " + s.findTerm)
		return
	}

	s.beginEdit(false)
	for i := from; i <= to; i++ {
		if s.focus.sealed(i) {
			continue
		}
		s.lines[i] = edReplaceInLine(s.lines[i], needle, replacement)
	}
	s.hasSel = false
	s.clampCursor()
	s.touch()
	s.ensureVisible()
	s.toast.Notify(fmt.Sprintf("Replaced %d in the %s", count, scope))
}

// edCountMatches counts non-overlapping occurrences of an already-lowercased
// needle in an already-lowercased line.
func edCountMatches(line, needle []rune) int {
	if len(needle) == 0 {
		return 0
	}
	count, i := 0, 0
	for i+len(needle) <= len(line) {
		if edIndexRunes(line[i:i+len(needle)], needle) == 0 {
			count++
			i += len(needle)
			continue
		}
		i++
	}
	return count
}

// edReplaceInLine rewrites one line, matching case-insensitively against an
// already-lowercased needle.
func edReplaceInLine(line string, needle, replacement []rune) string {
	if len(needle) == 0 {
		return line
	}
	src := []rune(line)
	lower := []rune(strings.ToLower(line))

	var out []rune
	for i := 0; i < len(src); {
		if edMatchAt(lower, i, needle) {
			out = append(out, replacement...)
			i += len(needle)
			continue
		}
		out = append(out, src[i])
		i++
	}
	return string(out)
}

// edShowReplaceDialog opens Replace, scoping a later Replace All to the
// selection when one spans several lines - which is the only time "in
// selection" differs usefully from "in the file".
func edShowReplaceDialog(s *edState) {
	s.dlgInput, s.dlgReplace = s.findTerm, s.replaceTerm
	s.dlgField = 0
	s.replaceFrom, s.replaceTo = -1, -1
	if s.hasSel {
		if _, ay, _, by := s.selRange(); by > ay {
			s.replaceFrom, s.replaceTo = ay, by
		}
	}
	s.openDialog(edDlgReplace)
}

// edTakeReplaceTerms copies the dialog's fields into the editor's own, so the
// buttons and Find Next all work from the same pair.
func edTakeReplaceTerms(s *edState) {
	s.findTerm, s.replaceTerm = s.dlgInput, s.dlgReplace
}
