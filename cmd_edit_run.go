package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// ── running the game ──────────────────────────────────────────────────────────
//
// Pressing Run saves and launches love, and watches what it says. A Lua error
// names its file and line, so the editor can open that file, put the caret on
// that line and show the message - rather than leaving the error in a terminal
// for you to go and find. The game's output still reaches the terminal too.

// edRunError is a Lua error located in the project.
type edRunError struct {
	path string
	line int // 0-based
	msg  string
}

// edRunEvent is what the watcher tells the editor: either an error it
// recognised, or that the game has finished.
type edRunEvent struct {
	err    *edRunError
	exited bool
	status int
}

// edLuaErrorRe matches the "file.lua:12: message" that every Lua error opens
// with, whatever love prefixes it with.
var edLuaErrorRe = regexp.MustCompile(`([^\s:"'()]+\.lua):(\d+):\s*(.*)$`)

// edParseLuaError pulls the location out of one line of output. Love prints the
// error itself before the stack traceback, so the caller takes the first match
// and ignores the rest: later matches are traceback frames.
func edParseLuaError(line string) (edRunError, bool) {
	m := edLuaErrorRe.FindStringSubmatch(line)
	if m == nil {
		return edRunError{}, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil || n < 1 {
		return edRunError{}, false
	}
	msg := strings.TrimSpace(m[3])
	if msg == "" {
		return edRunError{}, false
	}
	return edRunError{path: filepath.ToSlash(m[1]), line: n - 1, msg: msg}, true
}

// edLaunchGame saves the buffer and starts love, watching its output for an
// error worth jumping to.
func edLaunchGame(s *edState) {
	if s.dirty {
		if err := edSaveFile(s, s.path); err != nil {
			s.toast.Notify("Save failed: " + err.Error())
			return
		}
	}
	bin, err := findLoveBinary()
	if err != nil {
		s.toast.Notify("love/love2d not found in PATH")
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		s.toast.Notify(err.Error())
		return
	}

	cmd := exec.Command(bin, cwd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.toast.Notify("Run failed: " + err.Error())
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		s.toast.Notify("Run failed: " + err.Error())
		return
	}
	if err := cmd.Start(); err != nil {
		s.toast.Notify("Run failed: " + err.Error())
		return
	}

	s.clearRunError()
	go edWatchRun(cmd, stdout, stderr, s.runEvents)
	s.toast.Notify("Running love...")
}

// edWatchRun passes the game's output through to the terminal and reports the
// first Lua error in it.
//
// Both streams are read, because love does not agree with itself about which
// one an error belongs on. And the error will not arrive while the game is
// still up: with its output going to a pipe rather than a terminal, love block-
// buffers it and the whole lot lands when the process exits. So in practice the
// editor jumps to the fault when the crashed game is closed.
func edWatchRun(cmd *exec.Cmd, stdout, stderr io.ReadCloser, out chan<- edRunEvent) {
	lines := make(chan string, 64)

	var readers sync.WaitGroup
	readers.Add(2)
	pump := func(r io.ReadCloser, echo *os.File) {
		defer readers.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			fmt.Fprintln(echo, line) // the terminal still gets everything
			lines <- line
		}
	}
	go pump(stdout, os.Stdout)
	go pump(stderr, os.Stderr)
	go func() {
		readers.Wait()
		close(lines)
	}()

	reported := false
	for line := range lines {
		if reported || strings.HasPrefix(strings.TrimSpace(line), "stack traceback") {
			continue
		}
		if e, ok := edParseLuaError(line); ok {
			reported = true
			out <- edRunEvent{err: &e}
		}
	}

	status := 0
	if err := cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			status = exitErr.ExitCode()
		} else {
			status = -1
		}
	}
	out <- edRunEvent{exited: true, status: status}
}

// edApplyRunEvent takes a report from the render loop: an error jumps the
// editor to it, and a plain exit is only worth mentioning if it failed without
// one having been recognised.
func edApplyRunEvent(s *edState, ev edRunEvent) {
	if ev.exited {
		if ev.status != 0 && s.runErrPath == "" {
			s.toast.Notify(fmt.Sprintf("love exited with status %d", ev.status))
		}
		return
	}
	if ev.err == nil {
		return
	}
	e := *ev.err

	// The path love reports is relative to the project it was given, which is
	// the directory the editor is running in.
	path := e.path
	if !edFileExists(path) {
		// A frame from inside love itself, or a file that has since moved:
		// still say what happened, but there is nowhere to jump to.
		s.toast.Notify(edEllipsis(e.msg, 60))
		return
	}

	if !edGoToFile(s, path, e.line) {
		s.toast.Notify("Error in " + path + ": " + edEllipsis(e.msg, 40))
		return
	}
	s.setRunError(path, e.line, e.msg)
	s.toast.Notify(edEllipsis(e.msg, 60))
}

// edGoToFile brings a file up in a buffer and puts the caret on a line,
// reusing the buffer when the file is already open.
func edGoToFile(s *edState, path string, line int) bool {
	if i := edFindBuffer(s, path); i >= 0 {
		edSwitchBuffer(s, i)
	} else {
		lines, err := edReadFile(path)
		if err != nil {
			return false
		}
		edOpenBuffer(s, path, lines)
	}

	s.focus = edFocusWhole()
	s.cy = max(0, min(line, len(s.lines)-1))
	s.cx, s.goalX = 0, 0
	s.hasSel = false
	s.refocus()
	s.centerOnCursor()
	return true
}

// ── the error as a diagnostic ─────────────────────────────────────────────────
//
// A runtime error is shown the same way the language server's findings are: a
// coloured line number, an underline and the message in the status bar. It is
// kept apart from them so that the next thing the server publishes does not
// wipe it out - a crash stays on screen until the next run.

// setRunError records where the game died.
func (s *edState) setRunError(path string, line int, msg string) {
	s.runErrPath = path
	s.runErr = edDiagnostic{
		line: line, from: 0, to: -1, severity: 1,
		message: msg, source: "love",
	}
}

func (s *edState) clearRunError() {
	s.runErrPath = ""
	s.runErr = edDiagnostic{}
}

// runErrorFor returns the run error when it belongs to the buffer on screen.
func (s *edState) runErrorFor(line int) (edDiagnostic, bool) {
	if s.runErrPath == "" || filepath.ToSlash(s.path) != s.runErrPath || s.runErr.line != line {
		return edDiagnostic{}, false
	}
	return s.runErr, true
}

// hasRunErrorHere reports whether the run error belongs to this buffer at all.
func (s *edState) hasRunErrorHere() bool {
	return s.runErrPath != "" && filepath.ToSlash(s.path) == s.runErrPath
}

func edFileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// ── the other external things ─────────────────────────────────────────────────

// edStartBuild runs fz's own build in the background and reports the result
// through the render loop, so a slow archive step never stalls a frame.
func edStartBuild(s *edState) {
	if s.building {
		return
	}
	s.building = true
	s.toast.Notify("Building...")
	done := s.buildDone
	go func() { done <- runBuild(nil) }()
}

// edSpawnTool launches another fz editor as its own process: raylib holds one
// window per process, so gfx and map cannot open inside this one.
func edSpawnTool(s *edState, label string, args ...string) {
	exe, err := os.Executable()
	if err != nil {
		s.toast.Notify("Cannot find the fz binary: " + err.Error())
		return
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		s.toast.Notify("Could not open the " + label + ": " + err.Error())
		return
	}
	go func() { _ = cmd.Wait() }() // reap the child so it does not linger
	s.toast.Notify("Opened the " + label)
}
