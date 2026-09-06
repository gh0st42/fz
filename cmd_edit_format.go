package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
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
