package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ── Lua formatting ────────────────────────────────────────────────────────────
//
// Two formatters are possible: lua-language-server over the LSP session, and
// stylua as a plain pipe through stdin. The server is preferred when it is
// running, since it is already there and formats to the same rules it lints by;
// stylua can be forced from the Options menu for projects that keep a
// stylua.toml, and stands in whenever the server is unavailable or switched
// off. Both run off the render loop on a goroutine and hand their result back
// through a channel, so a slow formatter never stalls a frame.

type edFmtKind int

const (
	edFmtNone edFmtKind = iota
	edFmtStylua
	edFmtLSP
)

// edFormatter is one way of formatting the buffer.
type edFormatter struct {
	kind edFmtKind
	bin  string // absolute path to the tool
	name string // label for menus and messages
}

// edFmtResult carries a finished format back to the main loop. It names the
// tool that did the work, so the message stays true even if the choice changed
// while the format was in flight.
type edFmtResult struct {
	text string
	name string
	err  error
}

// edForceStylua overrides the preference for the language server. It only has
// an effect when stylua is actually installed.
var edForceStylua bool

// edStyluaPath is where stylua lives, looked up once; empty when not installed.
var edStyluaPath = func() string {
	bin, err := exec.LookPath("stylua")
	if err != nil {
		return ""
	}
	return bin
}()

// edPickFormatter chooses how to format right now, which depends on both the
// language server switch and the stylua override rather than on what was
// installed when the editor started.
func edPickFormatter() edFormatter {
	stylua := edFormatter{kind: edFmtStylua, bin: edStyluaPath, name: "stylua"}
	if edForceStylua && edStyluaPath != "" {
		return stylua
	}
	if edHaveLuaLSP() {
		return edFormatter{kind: edFmtLSP, name: "lua-language-server"}
	}
	if edStyluaPath != "" {
		return stylua
	}
	return edFormatter{kind: edFmtNone, name: "none"}
}

// format returns src formatted. It is called from a goroutine, never the
// render loop.
func (f edFormatter) format(path, src string) (string, error) {
	switch f.kind {
	case edFmtStylua:
		return edStyluaFormat(f.bin, path, src)
	case edFmtLSP:
		return edLSPFormat(path, src)
	default:
		return "", fmt.Errorf("no Lua formatter on PATH")
	}
}

// edStyluaFormat pipes src through `stylua -`. --stdin-filepath lets stylua
// locate the project's stylua.toml even though the source arrives on stdin.
func edStyluaFormat(bin, path, src string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	cmd := exec.Command(bin, "--stdin-filepath", abs, "-")
	cmd.Stdin = strings.NewReader(src)
	var out, errBuf strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(errBuf.String()); msg != "" {
			return "", fmt.Errorf("%s", edFirstLine(msg))
		}
		return "", err
	}
	if out.Len() == 0 {
		return "", fmt.Errorf("stylua returned nothing")
	}
	return out.String(), nil
}

func edFirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// ── formatting on save ────────────────────────────────────────────────────────

// edFormatOnSave tidies the buffer whenever it is written, so what lands on
// disk is what the formatter would produce and a file never drifts between
// saves. Some people want to decide when that happens, hence the Options entry.
var edFormatOnSave = true

// edFormatSaveWait is how long a save will wait for the formatter. Formatting
// takes tens of milliseconds in practice; the limit is there so that a wedged
// tool delays the write by a moment rather than hanging the editor, and the
// save still goes ahead with the text as it stands.
const edFormatSaveWait = 1500 * time.Millisecond

// edFormatBeforeSave rewrites the buffer in place, as one undoable edit, unless
// formatting is switched off, the file is not Lua, or the formatter declines.
// It is deliberately synchronous: the point is that the bytes being written are
// the formatted ones.
func edFormatBeforeSave(s *edState, path string) {
	if !edFormatOnSave || !strings.EqualFold(filepath.Ext(path), ".lua") {
		return
	}
	formatter := edPickFormatter()
	if formatter.kind == edFmtNone {
		return
	}

	src := strings.Join(s.lines, "\n")
	done := make(chan edFmtResult, 1)
	go func() {
		text, err := formatter.format(path, src)
		done <- edFmtResult{text: text, name: formatter.name, err: err}
	}()

	var res edFmtResult
	select {
	case res = <-done:
	case <-time.After(edFormatSaveWait):
		s.toast.Notify("Saved unformatted: " + formatter.name + " did not answer")
		return
	}

	if res.err != nil {
		// Usually a syntax error, which the formatter cannot do anything with.
		// Saving anyway is the right call: the text is the user's.
		s.toast.Notify("Saved unformatted: " + edFirstLine(res.err.Error()))
		return
	}
	text := strings.ReplaceAll(res.text, "\r\n", "\n")
	if text == src {
		return
	}

	line, col := s.cy, s.cx
	s.beginEdit(false)
	s.lines = strings.Split(text, "\n")
	s.cy = max(0, min(line, len(s.lines)-1))
	s.cx = min(col, s.lineLen(s.cy))
	s.goalX = s.cx
	s.hasSel = false
	s.touch()
	s.focus = edFocusWhole()
	s.refocus()
	s.ensureVisible()
}
