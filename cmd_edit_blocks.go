package main

// ── block structure ───────────────────────────────────────────────────────────
//
// Forgetting an "end" is the classic Lua mistake, so the editor shows the shape
// of the code two ways: a box around the keyword that pairs with the one under
// the caret, and a faint rule down each level of indentation. Both read the
// highlighter's tokens, so keywords inside comments and strings do not count.

// edBlockSpan is where a keyword sits: a line and a half-open column range.
type edBlockSpan struct {
	line, from, to int
}

// edBlockOpens reports whether a keyword opens a block that "end" closes.
// "for" and "while" reach their block through "do", so only "do" counts here.
func edBlockOpens(word string) bool {
	return word == "function" || word == "if" || word == "do"
}

// edKeywordAt returns the keyword token covering a column, if there is one.
// The caret counts as being on a keyword when it sits anywhere in it, including
// just after its last character.
func edKeywordAt(s *edState, line, col int) (word string, span edBlockSpan, ok bool) {
	if line < 0 || line >= len(s.lines) || line >= len(s.hl) {
		return "", edBlockSpan{}, false
	}
	r := s.runes(line)
	kinds := s.hl[line]

	for i := 0; i < len(r); {
		if i >= len(kinds) || kinds[i] != edKindKeyword || !edIsIdentStart(r[i]) {
			i++
			continue
		}
		j := i
		for j < len(r) && edIsIdentChar(r[j]) {
			j++
		}
		if col >= i && col <= j {
			return string(r[i:j]), edBlockSpan{line: line, from: i, to: j}, true
		}
		i = j
	}
	return "", edBlockSpan{}, false
}

// edEachKeyword walks the block keywords of a line in order, or in reverse.
func edEachKeyword(s *edState, line int, reverse bool, fn func(word string, span edBlockSpan) bool) {
	if line < 0 || line >= len(s.lines) || line >= len(s.hl) {
		return
	}
	r := s.runes(line)
	kinds := s.hl[line]

	var found []struct {
		word string
		span edBlockSpan
	}
	for i := 0; i < len(r); {
		if i >= len(kinds) || kinds[i] != edKindKeyword || !edIsIdentStart(r[i]) {
			i++
			continue
		}
		j := i
		for j < len(r) && edIsIdentChar(r[j]) {
			j++
		}
		found = append(found, struct {
			word string
			span edBlockSpan
		}{string(r[i:j]), edBlockSpan{line: line, from: i, to: j}})
		i = j
	}

	if reverse {
		for i := len(found) - 1; i >= 0; i-- {
			if !fn(found[i].word, found[i].span) {
				return
			}
		}
		return
	}
	for _, f := range found {
		if !fn(f.word, f.span) {
			return
		}
	}
}

// edMatchBlock finds the keyword under the caret and the one that pairs with
// it. Landing on "for" or "while" matches the "end" of the block their "do"
// opens, which is what someone looking at a loop wants to be shown.
func edMatchBlock(s *edState) (a, b edBlockSpan, ok bool) {
	word, span, found := edKeywordAt(s, s.cy, s.cx)
	if !found {
		return a, b, false
	}

	switch {
	case word == "for" || word == "while":
		// Match from the "do" that follows, but point at the "for" itself.
		do, hasDo := edFindDo(s, span)
		if !hasDo {
			return a, b, false
		}
		end, hasEnd := edScanForward(s, do, "do")
		return span, end, hasEnd

	case edBlockOpens(word):
		end, hasEnd := edScanForward(s, span, word)
		return span, end, hasEnd

	case word == "repeat":
		until, hasUntil := edScanForward(s, span, "repeat")
		return span, until, hasUntil

	case word == "end" || word == "until":
		opener, hasOpener := edScanBackward(s, span, word)
		return span, opener, hasOpener
	}
	return a, b, false
}

// edFindDo locates the "do" that opens the block of a for or while statement.
func edFindDo(s *edState, from edBlockSpan) (edBlockSpan, bool) {
	for line := from.line; line < len(s.lines) && line <= from.line+4; line++ {
		var out edBlockSpan
		hit := false
		edEachKeyword(s, line, false, func(word string, span edBlockSpan) bool {
			if line == from.line && span.from <= from.from {
				return true // still left of the for/while itself
			}
			if word == "do" {
				out, hit = span, true
				return false
			}
			return true
		})
		if hit {
			return out, true
		}
	}
	return edBlockSpan{}, false
}

// edScanForward walks on from an opening keyword to the one that closes it.
func edScanForward(s *edState, from edBlockSpan, opener string) (edBlockSpan, bool) {
	closer := "end"
	if opener == "repeat" {
		closer = "until"
	}

	depth := 1
	var out edBlockSpan
	done := false
	for line := from.line; line < len(s.lines) && !done; line++ {
		edEachKeyword(s, line, false, func(word string, span edBlockSpan) bool {
			if line == from.line && span.from <= from.from {
				return true // skip the opener and anything before it
			}
			switch {
			case closer == "until" && word == "repeat",
				closer == "end" && edBlockOpens(word):
				depth++
			case word == closer:
				depth--
				if depth == 0 {
					out, done = span, true
					return false
				}
			}
			return true
		})
	}
	return out, done
}

// edScanBackward walks back from a closing keyword to the one it closes.
func edScanBackward(s *edState, from edBlockSpan, closer string) (edBlockSpan, bool) {
	opens := edBlockOpens
	if closer == "until" {
		opens = func(word string) bool { return word == "repeat" }
	}

	depth := 1
	var out edBlockSpan
	done := false
	for line := from.line; line >= 0 && !done; line-- {
		edEachKeyword(s, line, true, func(word string, span edBlockSpan) bool {
			if line == from.line && span.from >= from.from {
				return true // skip the closer and anything after it
			}
			switch {
			case word == closer:
				depth++
			case opens(word):
				depth--
				if depth == 0 {
					out, done = span, true
					return false
				}
			}
			return true
		})
	}
	return out, done
}

// ── indent guides ─────────────────────────────────────────────────────────────

// edGuideDepths returns the indentation to draw a guide for on each line of a
// range. A blank line takes the smaller of its neighbours' indents, so a rule
// runs unbroken through the gaps inside a block instead of dashing.
func edGuideDepths(lines []string, from, to int) []int {
	if to < from {
		return nil
	}
	out := make([]int, to-from+1)
	for i := from; i <= to; i++ {
		if indent, blank := edLineIndent(lines, i); !blank {
			out[i-from] = indent
			continue
		}
		above, below := edNearestIndent(lines, i, -1), edNearestIndent(lines, i, 1)
		out[i-from] = min(above, below)
	}
	return out
}

// edLineIndent measures a line's leading spaces and says whether it is blank.
func edLineIndent(lines []string, i int) (indent int, blank bool) {
	if i < 0 || i >= len(lines) {
		return 0, true
	}
	line := lines[i]
	for n, r := range line {
		if r != ' ' && r != '\t' {
			return n, false
		}
	}
	return 0, true
}

// edNearestIndent looks in one direction for the first line with text on it.
func edNearestIndent(lines []string, from, step int) int {
	for i := from + step; i >= 0 && i < len(lines); i += step {
		if indent, blank := edLineIndent(lines, i); !blank {
			return indent
		}
	}
	return 0
}
