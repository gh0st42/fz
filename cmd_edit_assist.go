package main

import (
	"fmt"
	"sort"
	"strings"

	rl "github.com/gen2brain/raylib-go/raylib"
)

// ── code assistance ───────────────────────────────────────────────────────────
//
// Completion, hover documentation and signature help. Every request goes to the
// language server on a goroutine and comes back through a channel the render
// loop drains, so nothing here can stall a frame. Each request carries a
// sequence number and a reply for a stale one is dropped, which is what keeps
// the popup honest while the caret keeps moving.

type edAssistKind int

const (
	edAssistComplete edAssistKind = iota
	edAssistHover
	edAssistSignature
)

type edAssistResult struct {
	kind  edAssistKind
	seq   int
	items []edCompItem
	text  string
	from  int // active parameter span, for signatures
	to    int
	err   error
}

type edAssist struct {
	seq int // bumped per request; replies below it are stale

	// completion popup
	compOn     bool
	compAll    []edCompItem // everything the server offered
	comp       []edCompItem // filtered by what has been typed since
	compSel    int
	compScroll int
	compLine   int // buffer line the popup belongs to
	compCol    int // column the replaced word starts at

	// hover documentation
	infoOn bool
	info   []string

	// signature strip
	sigOn   bool
	sig     string
	sigFrom int
	sigTo   int
	sigLine int
}

// active reports whether any popup is on screen and should take keys.
func (a *edAssist) active() bool { return a.compOn || a.infoOn }

// dismiss closes everything and invalidates replies still in flight.
func (a *edAssist) dismiss() {
	a.seq++
	a.compOn, a.infoOn, a.sigOn = false, false, false
	a.compAll, a.comp, a.info = nil, nil, nil
}

// ── requests ──────────────────────────────────────────────────────────────────

// edWordStart returns the column where the identifier under the caret begins.
func edWordStart(line []rune, col int) int {
	i := min(col, len(line))
	for i > 0 && edIsIdentChar(line[i-1]) {
		i--
	}
	return i
}

// edStartComplete asks for the completions at the caret. Without a language
// server it falls back to the identifiers already in the buffer.
func edStartComplete(s *edState) {
	line := s.runes(s.cy)
	col := edWordStart(line, s.cx)

	s.assist.dismiss()
	s.assist.compLine, s.assist.compCol = s.cy, col
	s.assist.compSel, s.assist.compScroll = 0, 0

	if !edHaveLuaLSP() {
		s.assist.compAll = edBufferWords(s)
		s.assist.filter(s)
		s.assist.compOn = len(s.assist.comp) > 0
		if !s.assist.compOn {
			s.toast.Notify("No completions")
		}
		return
	}

	seq, path, src := s.assist.seq, s.path, strings.Join(s.lines, "\n")
	cy, cx, done := s.cy, s.cx, s.assistDone
	go func() {
		items, err := edLSPComplete(path, src, cy, cx)
		done <- edAssistResult{kind: edAssistComplete, seq: seq, items: items, err: err}
	}()
}

// edStartHover asks for the documentation of the symbol at the caret.
func edStartHover(s *edState) {
	if !edHaveLuaLSP() {
		s.toast.Notify("Inline help needs lua-language-server on PATH")
		return
	}
	s.assist.dismiss()
	s.toast.Notify("Looking up...")

	seq, path, src := s.assist.seq, s.path, strings.Join(s.lines, "\n")
	cy, cx, done := s.cy, s.cx, s.assistDone
	go func() {
		text, err := edLSPHover(path, src, cy, cx)
		done <- edAssistResult{kind: edAssistHover, seq: seq, text: text, err: err}
	}()
}

// edStartSignature asks what the call being typed expects. Triggered by "(" and
// "," rather than by every keystroke, which is what the protocol intends.
func edStartSignature(s *edState) {
	if !edHaveLuaLSP() {
		return
	}
	s.assist.seq++
	s.assist.sigLine = s.cy

	seq, path, src := s.assist.seq, s.path, strings.Join(s.lines, "\n")
	cy, cx, done := s.cy, s.cx, s.assistDone
	go func() {
		label, from, to, err := edLSPSignature(path, src, cy, cx)
		done <- edAssistResult{kind: edAssistSignature, seq: seq, text: label, from: from, to: to, err: err}
	}()
}

// edApplyAssist takes a reply from the render loop and updates the popups.
func edApplyAssist(s *edState, res edAssistResult) {
	if res.seq != s.assist.seq {
		return // the caret moved on; this answer is about somewhere else
	}
	switch res.kind {
	case edAssistComplete:
		if res.err != nil {
			s.toast.Notify("Completion failed: " + edFirstLine(res.err.Error()))
			return
		}
		s.assist.compAll = res.items
		if len(s.assist.compAll) == 0 {
			s.assist.compAll = edBufferWords(s) // fall back rather than show nothing
		}
		s.assist.filter(s)
		s.assist.compOn = len(s.assist.comp) > 0
		if !s.assist.compOn {
			s.toast.Notify("No completions")
		}

	case edAssistHover:
		if res.err != nil {
			s.toast.Notify("Inline help failed: " + edFirstLine(res.err.Error()))
			return
		}
		if strings.TrimSpace(res.text) == "" {
			s.toast.Notify("Nothing known about that symbol")
			return
		}
		s.assist.info = edCleanMarkup(res.text, edInfoCols)
		s.assist.infoOn = len(s.assist.info) > 0

	case edAssistSignature:
		if res.err != nil || strings.TrimSpace(res.text) == "" {
			s.assist.sigOn = false
			return
		}
		s.assist.sig, s.assist.sigFrom, s.assist.sigTo = res.text, res.from, res.to
		s.assist.sigOn = true
	}
}

// filter narrows the offered items to those matching what has been typed since
// the request went out, so the list stays live without asking again.
func (a *edAssist) filter(s *edState) {
	prefix := ""
	if a.compLine < len(s.lines) {
		line := s.runes(a.compLine)
		if a.compCol <= len(line) && s.cx <= len(line) && s.cx >= a.compCol {
			prefix = strings.ToLower(string(line[a.compCol:s.cx]))
		}
	}
	a.comp = a.comp[:0]
	for _, it := range a.compAll {
		if prefix == "" || strings.HasPrefix(strings.ToLower(it.label), prefix) {
			a.comp = append(a.comp, it)
		}
	}
	a.compSel = max(0, min(a.compSel, len(a.comp)-1))
	a.compScroll = edScrollIntoView(a.compScroll, a.compSel, edCompRows, len(a.comp))
}

// edBufferWords is the no-server fallback: identifiers already present in the
// buffer, plus the language's own words.
func edBufferWords(s *edState) []edCompItem {
	seen := map[string]bool{}
	for _, line := range s.lines {
		r := []rune(line)
		for i := 0; i < len(r); {
			if !edIsIdentStart(r[i]) {
				i++
				continue
			}
			j := i
			for j < len(r) && edIsIdentChar(r[j]) {
				j++
			}
			if word := string(r[i:j]); len(word) > 2 {
				seen[word] = true
			}
			i = j
		}
	}
	for word := range edLuaKeywords {
		seen[word] = true
	}
	for word := range edLuaBuiltins {
		seen[word] = true
	}

	out := make([]edCompItem, 0, len(seen))
	for word := range seen {
		out = append(out, edCompItem{label: word, insert: word, detail: "buffer"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].label < out[j].label })
	return out
}

// edAcceptCompletion puts the selected item into the buffer, replacing the
// partial word the caret sits in.
func edAcceptCompletion(s *edState) {
	if !s.assist.compOn || s.assist.compSel >= len(s.assist.comp) {
		return
	}
	item := s.assist.comp[s.assist.compSel]
	s.assist.dismiss()

	if s.cy != s.assist.compLine {
		return
	}
	s.selY, s.selX = s.cy, s.assist.compCol
	s.hasSel = s.assist.compCol != s.cx
	s.beginEdit(false)
	s.insert(item.insert)
	s.ensureVisible()
}

// ── markdown ──────────────────────────────────────────────────────────────────

// edCleanMarkup turns a hover reply into plain lines that fit the panel: the
// servers answer in markdown, and the editor has one font and no bold.
func edCleanMarkup(text string, width int) []string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || trimmed == "---" || trimmed == "***" {
			continue
		}
		line = strings.NewReplacer("`", "", "**", "", "*", "", "—", "-").Replace(line)
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			continue
		}
		out = append(out, edWrap(line, width)...)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	if len(out) > edInfoRows {
		out = append(out[:edInfoRows-1], "...")
	}
	return out
}

// edWrap breaks a line on word boundaries, keeping its indentation.
func edWrap(line string, width int) []string {
	if len([]rune(line)) <= width {
		return []string{line}
	}
	indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	var out []string
	current := indent
	for _, word := range strings.Fields(line) {
		switch {
		case current == indent:
			current += word
		case len([]rune(current))+1+len([]rune(word)) <= width:
			current += " " + word
		default:
			out = append(out, current)
			current = indent + word
		}
	}
	if strings.TrimSpace(current) != "" {
		out = append(out, current)
	}
	return out
}

// ── drawing ───────────────────────────────────────────────────────────────────

const (
	edCompRows = 8  // visible rows in the completion popup
	edInfoRows = 12 // maximum lines of hover documentation
	edInfoCols = 56 // wrap width for that documentation
)

// edCaretScreen is where the caret sits on the canvas, if it is visible at all.
func edCaretScreen(s *edState) (x, y int32, ok bool) {
	row := s.cy - s.viewTop() - s.scrollY
	col := s.cx - s.scrollX
	if row < 0 || row >= edRows || col < 0 || col > edCols(s) {
		return 0, 0, false
	}
	return edTextX0(s) + int32(col)*edCharW, edTextY0 + int32(row)*edLineH, true
}

// edPanelRect places a popup of the given size near the caret, flipping it
// above the caret line and sliding it left when there is no room.
func edPanelRect(caretX, caretY, w, h int32) (int32, int32) {
	x := min(caretX, edVScrX-w-2)
	x = max(x, edFrameX+2)
	y := caretY + edLineH
	if y+h > edFrameY+edFrameH-edScrollW {
		y = caretY - h
	}
	y = max(y, edTextY0)
	return x, y
}

func edDrawAssist(s *edState) {
	edDrawSignature(s)
	edDrawCompletion(s)
	edDrawInfo(s)
}

func edDrawCompletion(s *edState) {
	if !s.assist.compOn || len(s.assist.comp) == 0 {
		return
	}
	caretX, caretY, ok := edCaretScreen(s)
	if !ok {
		return
	}

	cols := 0
	for _, it := range s.assist.comp {
		if n := len([]rune(it.label)) + len([]rune(it.detail)) + 3; n > cols {
			cols = n
		}
	}
	cols = max(min(cols, 46), 12)
	rows := min(len(s.assist.comp), edCompRows)
	w, h := int32(cols)*edCharW+4, int32(rows)*edLineH+4
	x, y := edPanelRect(caretX, caretY, w, h)

	rl.DrawRectangle(x+2, y+2, w, h, rl.NewColor(0, 0, 0, 110))
	rl.DrawRectangle(x, y, w, h, edEgaBlue)
	rl.DrawRectangleLines(x, y, w, h, edEgaLightGray)

	for i := 0; i < rows; i++ {
		idx := s.assist.compScroll + i
		if idx >= len(s.assist.comp) {
			break
		}
		item := s.assist.comp[idx]
		ry := y + 2 + int32(i)*edLineH
		label, detail := edEgaYellow, edEgaCyan
		if idx == s.assist.compSel {
			rl.DrawRectangle(x+1, ry, w-2, edLineH, edEgaLightGray)
			label, detail = edEgaBlack, edEgaDarkGray
		}
		edDrawStr(x+2, ry, edEllipsis(item.label, cols-2), label)
		if item.detail != "" {
			d := edEllipsis(item.detail, 14)
			edDrawStr(x+w-2-int32(len([]rune(d)))*edCharW, ry, d, detail)
		}
	}

	if len(s.assist.comp) > rows {
		note := fmt.Sprintf("%d/%d", s.assist.compSel+1, len(s.assist.comp))
		edDrawStr(x+w-int32(len(note))*edCharW-2, y+h-edLineH, note, edEgaDarkGray)
	}
}

func edDrawInfo(s *edState) {
	if !s.assist.infoOn || len(s.assist.info) == 0 {
		return
	}
	caretX, caretY, ok := edCaretScreen(s)
	if !ok {
		return
	}

	cols := 0
	for _, line := range s.assist.info {
		if n := len([]rune(line)); n > cols {
			cols = n
		}
	}
	cols = max(min(cols, edInfoCols), 16)
	w := int32(cols)*edCharW + 8
	h := int32(len(s.assist.info))*edLineH + edLineH + 6
	x, y := edPanelRect(caretX, caretY, w, h)

	rl.DrawRectangle(x+2, y+2, w, h, rl.NewColor(0, 0, 0, 110))
	rl.DrawRectangle(x, y, w, h, edEgaLightGray)
	rl.DrawRectangleLines(x, y, w, h, edEgaBlack)
	rl.DrawRectangle(x+1, y+1, w-2, edLineH, edEgaBlue)
	edDrawStr(x+4, y+1, "Help - Esc to close", edEgaWhite)

	for i, line := range s.assist.info {
		edDrawStr(x+4, y+edLineH+3+int32(i)*edLineH, line, edEgaBlack)
	}
}

// edDrawSignature shows the call being typed on the row above the caret, with
// the parameter currently being filled in picked out.
func edDrawSignature(s *edState) {
	if !s.assist.sigOn || s.assist.sig == "" || s.cy != s.assist.sigLine {
		return
	}
	caretX, caretY, ok := edCaretScreen(s)
	if !ok {
		return
	}

	label := []rune(edEllipsis(s.assist.sig, 60))
	w := int32(len(label))*edCharW + 8
	x := max(edFrameX+2, min(caretX, edVScrX-w-2))
	y := caretY - edLineH - 2
	if y < edTextY0 {
		y = caretY + edLineH + 2
	}

	rl.DrawRectangle(x, y, w, edLineH, edEgaLightGray)
	rl.DrawRectangleLines(x, y, w, edLineH, edEgaBlack)
	for i, r := range label {
		col := edEgaBlack
		if i >= s.assist.sigFrom && i < s.assist.sigTo {
			col = edEgaRed // the parameter being typed
		}
		edDrawChar(x+4+int32(i)*edCharW, y, r, col)
	}
}

// ── input ─────────────────────────────────────────────────────────────────────

// edAssistInput handles keys while a popup owns them. It reports whether the
// key was consumed.
func edAssistInput(s *edState) bool {
	a := &s.assist
	if !a.active() {
		return false
	}
	if rl.IsKeyPressed(rl.KeyEscape) {
		a.dismiss()
		return true
	}
	if a.infoOn {
		// The help panel is passive: any other key dismisses it and is then
		// handled normally.
		if rl.GetKeyPressed() != 0 || rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
			a.infoOn = false
		}
		return false
	}

	switch {
	case s.repeatKey(rl.KeyDown):
		a.moveSel(1)
	case s.repeatKey(rl.KeyUp):
		a.moveSel(-1)
	case s.repeatKey(rl.KeyPageDown):
		a.moveSel(edCompRows)
	case s.repeatKey(rl.KeyPageUp):
		a.moveSel(-edCompRows)
	case rl.IsKeyPressed(rl.KeyEnter), rl.IsKeyPressed(rl.KeyKpEnter), rl.IsKeyPressed(rl.KeyTab):
		edAcceptCompletion(s)
	default:
		return false // typing carries on into the buffer, and re-filters below
	}
	return true
}

func (a *edAssist) moveSel(delta int) {
	if len(a.comp) == 0 {
		return
	}
	a.compSel = max(0, min(a.compSel+delta, len(a.comp)-1))
	a.compScroll = edScrollIntoView(a.compScroll, a.compSel, edCompRows, len(a.comp))
}

// edAssistAfterEdit keeps the popup in step with the text: it re-filters as the
// word grows, and gives up once the caret leaves it.
func edAssistAfterEdit(s *edState) {
	a := &s.assist
	if !a.compOn {
		return
	}
	if s.cy != a.compLine || s.cx < a.compCol {
		a.dismiss()
		return
	}
	a.filter(s)
	if len(a.comp) == 0 {
		a.dismiss()
	}
}

// edAssistAfterTyping fires the automatic triggers: a member access offers
// completions, and opening or continuing an argument list asks what it expects.
func edAssistAfterTyping(s *edState, typed string) {
	if typed == "" {
		return
	}
	switch typed[len(typed)-1] {
	case '.', ':':
		if edHaveLuaLSP() {
			edStartComplete(s)
		}
	case '(', ',':
		edStartSignature(s)
	case ')':
		s.assist.sigOn = false
	}
}

// ── diagnostics ───────────────────────────────────────────────────────────────

// edSyncDiagnostics pushes settled edits to the server and picks up whatever it
// has published since. Both halves are cheap enough to run every frame.
func edSyncDiagnostics(s *edState) {
	if !edHaveLuaLSP() {
		if len(s.diags) > 0 {
			s.diags = nil
		}
		return
	}

	if s.diagPush > 0 && float64(rl.GetTime()) >= s.diagPush {
		s.diagPush = 0
		edPushDocument(s.path, strings.Join(s.lines, "\n"))
	}

	if seq := edPublishSeq(); seq != s.diagSeq {
		s.diags, s.diagSeq = edDiagnosticsFor(s.path)
	}
}

// edDiagnosticAt returns the most serious problem reported on a line.
func edDiagnosticAt(s *edState, line int) (edDiagnostic, bool) {
	best, found := s.runErrorFor(line)
	for _, d := range s.diags {
		if d.line != line {
			continue
		}
		if !found || d.severity < best.severity {
			best, found = d, true
		}
	}
	return best, found
}

// edDiagnosticColor is red for errors and yellow for everything softer.
func edDiagnosticColor(d edDiagnostic) rl.Color {
	if d.isError() {
		return edClrError
	}
	return edClrWarn
}

// edDiagnosticCounts totals the errors and the rest, for the status bar.
func edDiagnosticCounts(s *edState) (errors, others int) {
	if s.hasRunErrorHere() {
		errors++
	}
	for _, d := range s.diags {
		if d.isError() {
			errors++
		} else {
			others++
		}
	}
	return errors, others
}

// edNextProblem moves the caret to the next diagnostic after it, wrapping.
func edNextProblem(s *edState) {
	if len(s.diags) == 0 {
		s.toast.Notify("No problems reported")
		return
	}
	best, found := edDiagnostic{line: 1 << 30}, false
	first := edDiagnostic{line: 1 << 30}
	for _, d := range s.diags {
		if d.line < first.line {
			first = d
		}
		if d.line > s.cy && d.line < best.line {
			best, found = d, true
		}
	}
	if !found {
		best, found = first, first.line < 1<<30
	}
	if !found {
		return
	}

	s.focus = edFocusWhole()
	s.cy = max(0, min(best.line, len(s.lines)-1))
	s.cx = max(0, min(best.from, s.lineLen(s.cy)))
	s.goalX, s.hasSel = s.cx, false
	s.refocus()
	s.centerOnCursor()
	s.toast.Notify(edEllipsis(best.message, 60))
}

// edDrawDiagnosticMarks underlines the reported ranges on the visible lines.
// The line numbers themselves are coloured in edDrawText, where they are drawn.
func edDrawDiagnosticMarks(s *edState, line int, y int32) {
	d, ok := edDiagnosticAt(s, line)
	if !ok {
		return
	}
	from, to := d.from, d.to
	if to < 0 || to <= from {
		to = s.lineLen(line) // spans lines, or is empty: mark to end of line
	}
	from = max(from, s.scrollX)
	to = min(to, s.scrollX+edCols(s))
	if to <= from {
		return
	}
	x := edTextX0(s) + int32(from-s.scrollX)*edCharW
	w := int32(to-from) * edCharW
	rl.DrawRectangle(x, y+edLineH-1, w, 1, edDiagnosticColor(d))
}
