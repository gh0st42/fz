package main

import (
	"sort"
	"strings"
	"unicode"
)

// ── Lua syntax highlighting ───────────────────────────────────────────────────
//
// A deliberately small, allocation-light tokenizer: it classifies every rune of
// a line into one edTokKind so the renderer can pick a colour per character
// without re-parsing anything at draw time. Multi-line constructs (long strings
// and long comments, [[ ]] / --[==[ ]==]) are carried across lines in edLongCtx.

type edTokKind uint8

const (
	edKindNormal edTokKind = iota
	edKindKeyword
	edKindBuiltin
	edKindString
	edKindNumber
	edKindComment
	edKindOp
)

// edLongCtx is the highlighter state carried from one line to the next while a
// long bracket ([[ … ]] or --[[ … ]]) is still open.
type edLongCtx struct {
	active  bool
	level   int // number of '=' signs in the opening bracket
	comment bool
}

var edLuaKeywords = map[string]bool{
	"and": true, "break": true, "do": true, "else": true, "elseif": true,
	"end": true, "false": true, "for": true, "function": true, "goto": true,
	"if": true, "in": true, "local": true, "nil": true, "not": true,
	"or": true, "repeat": true, "return": true, "then": true, "true": true,
	"until": true, "while": true,
}

// edLuaBuiltins covers the Lua standard library plus the Love2D globals a
// friendzone project touches most often.
var edLuaBuiltins = map[string]bool{
	"assert": true, "collectgarbage": true, "dofile": true, "error": true,
	"getmetatable": true, "ipairs": true, "load": true, "loadstring": true,
	"next": true, "pairs": true, "pcall": true, "print": true, "rawequal": true,
	"rawget": true, "rawlen": true, "rawset": true, "require": true,
	"select": true, "setmetatable": true, "tonumber": true, "tostring": true,
	"type": true, "unpack": true, "xpcall": true, "self": true,
	"_G": true, "_VERSION": true,
	"coroutine": true, "debug": true, "io": true, "math": true, "os": true,
	"string": true, "table": true, "utf8": true,
	"love": true,
}

// edHighlightLine classifies every rune of src and returns the highlighter
// state to feed into the next line.
func edHighlightLine(src []rune, ctx edLongCtx) ([]edTokKind, edLongCtx) {
	n := len(src)
	out := make([]edTokKind, n)
	i := 0

	// Continuation of a long string/comment opened on an earlier line.
	if ctx.active {
		kind := edKindString
		if ctx.comment {
			kind = edKindComment
		}
		end, closed := edLongClose(src, 0, ctx.level)
		for ; i < end; i++ {
			out[i] = kind
		}
		if !closed {
			return out, ctx
		}
		ctx = edLongCtx{}
	}

	for i < n {
		c := src[i]

		// Comments: --[[ long ]] or -- to end of line.
		if c == '-' && i+1 < n && src[i+1] == '-' {
			if lvl, ok := edLongOpen(src, i+2); ok {
				end, closed := edLongClose(src, i+2+lvl+2, lvl)
				for j := i; j < end; j++ {
					out[j] = edKindComment
				}
				i = end
				if !closed {
					return out, edLongCtx{active: true, level: lvl, comment: true}
				}
				continue
			}
			for ; i < n; i++ {
				out[i] = edKindComment
			}
			break
		}

		switch {
		case c == '[':
			if lvl, ok := edLongOpen(src, i); ok {
				end, closed := edLongClose(src, i+lvl+2, lvl)
				for j := i; j < end; j++ {
					out[j] = edKindString
				}
				i = end
				if !closed {
					return out, edLongCtx{active: true, level: lvl}
				}
				continue
			}
			out[i] = edKindOp
			i++

		case c == '"' || c == '\'':
			out[i] = edKindString
			i++
			for i < n {
				out[i] = edKindString
				if src[i] == '\\' && i+1 < n {
					out[i+1] = edKindString
					i += 2
					continue
				}
				if src[i] == c {
					i++
					break
				}
				i++
			}

		case edIsDigit(c) || (c == '.' && i+1 < n && edIsDigit(src[i+1])):
			j := edScanNumber(src, i)
			for ; i < j; i++ {
				out[i] = edKindNumber
			}

		case edIsIdentStart(c):
			j := i
			for j < n && edIsIdentChar(src[j]) {
				j++
			}
			kind := edKindNormal
			switch word := string(src[i:j]); {
			case edLuaKeywords[word]:
				kind = edKindKeyword
			case edLuaBuiltins[word]:
				kind = edKindBuiltin
			}
			for ; i < j; i++ {
				out[i] = kind
			}

		case c == ' ' || c == '\t':
			out[i] = edKindNormal
			i++

		default:
			out[i] = edKindOp
			i++
		}
	}

	return out, edLongCtx{}
}

// edLongOpen reports whether a long bracket opens at i ("[", n×"=", "["), and
// returns the bracket level n.
func edLongOpen(src []rune, i int) (int, bool) {
	if i >= len(src) || src[i] != '[' {
		return 0, false
	}
	j := i + 1
	for j < len(src) && src[j] == '=' {
		j++
	}
	if j < len(src) && src[j] == '[' {
		return j - i - 1, true
	}
	return 0, false
}

// edLongClose scans from `from` for the matching closing bracket of the given
// level. It returns the index just past the closing bracket and whether one was
// found; when unterminated the index is len(src).
func edLongClose(src []rune, from, level int) (int, bool) {
	if from > len(src) {
		return len(src), false
	}
	for i := from; i < len(src); i++ {
		if src[i] != ']' {
			continue
		}
		j := i + 1
		for j < len(src) && src[j] == '=' {
			j++
		}
		if j-i-1 == level && j < len(src) && src[j] == ']' {
			return j + 1, true
		}
	}
	return len(src), false
}

// edScanNumber returns the index just past the numeric literal starting at i.
func edScanNumber(src []rune, i int) int {
	n := len(src)
	j := i
	if src[j] == '0' && j+1 < n && (src[j+1] == 'x' || src[j+1] == 'X') {
		j += 2
		for j < n && edIsHexDigit(src[j]) {
			j++
		}
		return j
	}
	for j < n && edIsDigit(src[j]) {
		j++
	}
	if j < n && src[j] == '.' {
		j++
		for j < n && edIsDigit(src[j]) {
			j++
		}
	}
	if j < n && (src[j] == 'e' || src[j] == 'E') {
		k := j + 1
		if k < n && (src[k] == '+' || src[k] == '-') {
			k++
		}
		if k < n && edIsDigit(src[k]) {
			for k < n && edIsDigit(src[k]) {
				k++
			}
			j = k
		}
	}
	return j
}

func edIsDigit(r rune) bool { return r >= '0' && r <= '9' }

func edIsHexDigit(r rune) bool {
	return edIsDigit(r) || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

// edIsIdentStart and edIsIdentChar accept letters beyond ASCII so that words
// like "grüße" stay one token. Lua itself only allows ASCII in identifiers, but
// treating the rest as punctuation would shred any accented word on screen -
// and these two also drive word-jumps and completion, where splitting a word at
// its umlaut is simply wrong.
func edIsIdentStart(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		(r >= 0x80 && unicode.IsLetter(r))
}

func edIsIdentChar(r rune) bool {
	return edIsIdentStart(r) || edIsDigit(r) || (r >= 0x80 && unicode.IsDigit(r))
}

// ── function outline ──────────────────────────────────────────────────────────

// edFunc is one function declaration found in the buffer.
type edFunc struct {
	name   string
	line   int // 0-based
	end    int // last line of its block, -1 when it could not be found
	local  bool
	parent string // the top-level function it is declared inside, "" if none
}

// nested reports whether the declaration sits inside another function rather
// than at the top level of the file.
func (f edFunc) nested() bool { return f.parent != "" }

// edParseFunctions lists the function declarations in lines. It recognises
// `function f()`, `local function f()`, `function M.f()`, `function M:f()` and
// the assignment forms `f = function()` / `M.f = function()`.
//
// hl is the highlighter's per-line token kinds; when a line's first non-blank
// rune sits inside a comment or a long string, that line is skipped, so
// commented-out code stays out of the outline. A nil or short hl is tolerated.
func edParseFunctions(lines []string, hl [][]edTokKind) []edFunc {
	var out []edFunc

	for i, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "--") {
			continue
		}
		if i < len(hl) && edLineStartsInText(line, hl[i]) {
			continue
		}

		var name string
		local := false

		if rest, ok := strings.CutPrefix(t, "local function "); ok {
			name, local = edFuncName(rest), true
		} else if rest, ok := strings.CutPrefix(t, "function "); ok {
			name = edFuncName(rest)
		} else if idx := strings.Index(t, "= function"); idx > 0 {
			// Assignment form: [local] lhs = function(...)
			lhs := strings.TrimSpace(t[:idx])
			if rest, ok := strings.CutPrefix(lhs, "local "); ok {
				lhs, local = strings.TrimSpace(rest), true
			}
			if edValidFuncName(lhs) {
				name = lhs
			}
		}

		if name != "" {
			out = append(out, edFunc{name: name, line: i, end: -1, local: local})
		}
	}

	edResolveNesting(lines, hl, out)

	sort.SliceStable(out, func(a, b int) bool {
		la, lb := strings.ToLower(out[a].name), strings.ToLower(out[b].name)
		if la != lb {
			return la < lb
		}
		return out[a].line < out[b].line
	})
	return out
}

// edResolveNesting fills in each declaration's block extent and, for any that
// turns out to live inside another one, the name of the top-level function it
// belongs to. The input must still be in line order.
func edResolveNesting(lines []string, hl [][]edTokKind, fns []edFunc) {
	for i := range fns {
		fns[i].end = edBlockEnd(lines, hl, fns[i].line)
		if fns[i].end < 0 {
			fns[i].end = fns[i].line
		}
	}

	for i := range fns {
		// Walk outwards to the outermost declaration whose block contains this
		// one; that is the top-level function it belongs to.
		top := -1
		for j := range fns {
			if j == i || fns[j].line >= fns[i].line || fns[j].end < fns[i].end {
				continue
			}
			if top < 0 || fns[j].line < fns[top].line {
				top = j
			}
		}
		if top >= 0 {
			fns[i].parent = fns[top].name
		}
	}
}

// edLineStartsInText reports whether the line's first non-blank rune is part of
// a comment or a string, i.e. the line is not code.
func edLineStartsInText(line string, kinds []edTokKind) bool {
	for i, r := range []rune(line) {
		if r == ' ' || r == '\t' {
			continue
		}
		return i < len(kinds) && (kinds[i] == edKindComment || kinds[i] == edKindString)
	}
	return false
}

// edFuncName pulls the (possibly dotted or colon-qualified) identifier out of
// the text right after "function ".
func edFuncName(rest string) string {
	end := strings.IndexAny(rest, "( \t")
	if end <= 0 {
		return ""
	}
	if name := rest[:end]; edValidFuncName(name) {
		return name
	}
	return ""
}

func edValidFuncName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !edIsIdentChar(r) && r != '.' && r != ':' {
			return false
		}
	}
	return true
}
