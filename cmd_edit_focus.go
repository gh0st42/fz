package main

import (
	"strings"
	"sync"
)

// ── function focus ────────────────────────────────────────────────────────────
//
// QBasic and VB showed one procedure at a time rather than the whole file. The
// buffer here still holds the entire document - focus is purely a view over a
// line range, plus a rule about which parts of it are read-only: you may edit
// the body and the parameter list, but not the declaration around it or the
// closing "end", so the shape of the function cannot be broken from inside it.

// edFocus is the function currently on screen.
type edFocus struct {
	active bool
	from   int    // first visible buffer line (leading comments included)
	to     int    // last visible buffer line
	name   string // for the window title

	header    int // line holding the declaration, -1 when there is none
	paramFrom int // first editable column on the header line
	paramTo   int // last editable column on the header line
	tail      int // line holding the closing "end", -1 when there is none
}

// edFocusWhole is the unfocused view: the entire buffer, all of it editable.
func edFocusWhole() edFocus {
	return edFocus{header: -1, tail: -1}
}

// viewTopIn is the first line this focus would show in the given buffer, which
// is what scrollY is measured from.
func (f edFocus) viewTopIn(s *edState) int {
	if !f.active {
		return 0
	}
	return max(0, min(f.from, len(s.lines)-1))
}

// editableAt reports whether the caret may sit at (line, col) and change text
// there. Only the parameter list of the declaration is writable on the header
// line, and the closing "end" is sealed entirely.
func (f edFocus) editableAt(line, col int) bool {
	if !f.active {
		return true
	}
	switch line {
	case f.header:
		return col >= f.paramFrom && col <= f.paramTo
	case f.tail:
		return false
	}
	return true
}

// editableSpan reports whether a run of columns on one line may be replaced.
// On the declaration line only a span wholly inside the parameter list counts,
// so a selection reaching past the closing bracket is refused.
func (f edFocus) editableSpan(line, from, to int) bool {
	if !f.active {
		return true
	}
	switch line {
	case f.tail:
		return false
	case f.header:
		return from >= f.paramFrom && to <= f.paramTo
	}
	return true
}

// sealed reports whether a whole line is off limits, which is what line-wise
// operations like indent and comment toggling need to know.
func (f edFocus) sealed(line int) bool {
	return f.active && (line == f.header || line == f.tail)
}

// ── locating a function ───────────────────────────────────────────────────────

// edComputeFocus works out which function encloses the given line and returns
// the range to show. When the line sits outside every function - module-level
// code, say - the whole buffer stays visible, which is how you navigate back
// into one.
func edComputeFocus(s *edState, line int) edFocus {
	s.rehighlight()

	from, to, name := -1, -1, ""
	if sym, ok := edSymbolAt(s.path, line); ok {
		from, to, name = sym.from, sym.to, sym.name
	} else {
		// The innermost declaration whose block covers the line, which for a
		// nested function is the nested one rather than its parent.
		for _, fn := range edParseFunctions(s.lines, s.hl) {
			if fn.line > line || fn.end < line || fn.line < from {
				continue
			}
			from, to, name = fn.line, fn.end, fn.name
		}
	}
	if from < 0 || to < from {
		return edFocusWhole()
	}

	f := edFocus{active: true, from: from, to: to, name: name, header: from, tail: to}
	f.paramFrom, f.paramTo, _ = edParamRange(s.lines[from])
	if f.paramTo < f.paramFrom {
		f.paramFrom, f.paramTo = 0, -1 // no parameter list found; seal the line
	}

	// Pull in the comment block sitting directly above the declaration, so a
	// function arrives with its documentation.
	for i := from - 1; i >= 0; i-- {
		body := strings.TrimSpace(s.lines[i])
		if !strings.HasPrefix(body, "--") {
			break
		}
		f.from = i
	}
	return f
}

// edBlockEnd returns the line holding the "end" that closes the function
// declared on headerLine, or -1 if the file runs out first. Keywords inside
// comments and strings do not count, which the highlighter's tokens tell us.
func edBlockEnd(lines []string, hl [][]edTokKind, headerLine int) int {
	depth := 0
	for i := headerLine; i < len(lines); i++ {
		var kinds []edTokKind
		if i < len(hl) {
			kinds = hl[i]
		}
		for _, w := range edCodeKeywords(lines[i], kinds) {
			switch w {
			case "function", "if", "do":
				// "for"/"while" always reach their block through "do", so
				// counting "do" alone keeps the depth right.
				depth++
			case "end":
				depth--
				if depth == 0 {
					return i
				}
			}
		}
	}
	return -1
}

// edCodeKeywords lists the keyword tokens on a line, skipping anything the
// highlighter classified as comment or string.
func edCodeKeywords(line string, kinds []edTokKind) []string {
	r := []rune(line)
	var out []string
	for i := 0; i < len(r); {
		if i < len(kinds) && kinds[i] == edKindKeyword && edIsIdentStart(r[i]) {
			j := i
			for j < len(r) && edIsIdentChar(r[j]) {
				j++
			}
			out = append(out, string(r[i:j]))
			i = j
			continue
		}
		i++
	}
	return out
}

// edParamRange returns the editable span inside a declaration's parentheses:
// the first column after "(" and the column of the matching ")".
func edParamRange(line string) (from, to int, ok bool) {
	r := []rune(line)
	open := -1
	for i, c := range r {
		if c == '(' {
			open = i
			break
		}
	}
	if open < 0 {
		return 0, -1, false
	}
	depth := 0
	for i := open; i < len(r); i++ {
		switch r[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return open + 1, i, true
			}
		}
	}
	return 0, -1, false
}

// ── LSP symbol bounds ─────────────────────────────────────────────────────────
//
// When lua-language-server is installed its document symbols give exact bounds,
// including for shapes the line scanner above has to guess at. Asking for them
// is slow the first time, so it happens on a goroutine and the scanner's answer
// is used until the reply lands.

type edSymbol struct {
	name     string
	from, to int // 0-based, inclusive
}

var edSyms struct {
	sync.Mutex
	byPath  map[string][]edSymbol
	pending map[string]bool
	enabled bool
	checked bool
}

// edSymbolsAvailable reports whether a language server is worth asking.
func edSymbolsAvailable() bool {
	edSyms.Lock()
	defer edSyms.Unlock()
	if !edSyms.checked {
		edSyms.checked = true
		edSyms.enabled = edHaveLuaLSP()
	}
	return edSyms.enabled
}

// edResetSymbolCache forgets what the server told us, so that turning it back
// on re-asks and turning it off falls straight back to the line scanner.
func edResetSymbolCache() {
	edSyms.Lock()
	defer edSyms.Unlock()
	edSyms.byPath = nil
	edSyms.checked = false
}

// edSymbolAt returns the innermost cached symbol covering a line.
func edSymbolAt(path string, line int) (edSymbol, bool) {
	edSyms.Lock()
	defer edSyms.Unlock()
	best, found := edSymbol{}, false
	for _, sym := range edSyms.byPath[path] {
		if line < sym.from || line > sym.to {
			continue
		}
		if !found || (sym.to-sym.from) < (best.to-best.from) {
			best, found = sym, true
		}
	}
	return best, found
}

// edRequestSymbols refreshes the symbol cache for a document in the background.
// Repeat calls while one is in flight are dropped.
func edRequestSymbols(path, src string) {
	if path == "" || !edSymbolsAvailable() {
		return
	}
	edSyms.Lock()
	if edSyms.pending == nil {
		edSyms.pending = map[string]bool{}
	}
	if edSyms.pending[path] {
		edSyms.Unlock()
		return
	}
	edSyms.pending[path] = true
	edSyms.Unlock()

	go func() {
		syms, err := edLSPDocumentSymbols(path, src)
		edSyms.Lock()
		defer edSyms.Unlock()
		delete(edSyms.pending, path)
		if err != nil {
			edSyms.enabled = false // stop pestering a server that will not answer
			return
		}
		if edSyms.byPath == nil {
			edSyms.byPath = map[string][]edSymbol{}
		}
		edSyms.byPath[path] = syms
	}()
}
