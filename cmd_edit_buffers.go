package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ── open buffers ──────────────────────────────────────────────────────────────
//
// The editor always works on one live document, held directly in edState so the
// editing code stays unaware of any of this. Every other open file is parked in
// a edBuffer, and switching swaps the live fields out and the parked ones in.

type edBuffer struct {
	path      string
	lines     []string
	cx, cy    int
	goalX     int
	scrollX   int
	scrollY   int
	selX      int
	selY      int
	hasSel    bool
	dirty     bool
	savedMark edMark
	hl        [][]edTokKind
	hlDirty   bool
	undo      []edSnapshot
	redo      []edSnapshot
	typing    bool
}

// captureBuffer freezes the live document.
func (s *edState) captureBuffer() edBuffer {
	return edBuffer{
		path: s.path, lines: s.lines,
		cx: s.cx, cy: s.cy, goalX: s.goalX,
		scrollX: s.scrollX, scrollY: s.scrollY,
		selX: s.selX, selY: s.selY, hasSel: s.hasSel,
		dirty: s.dirty, savedMark: s.savedMark, hl: s.hl, hlDirty: s.hlDirty,
		undo: s.undo, redo: s.redo, typing: s.typing,
	}
}

// restoreBuffer makes b the live document.
func (s *edState) restoreBuffer(b edBuffer) {
	s.path, s.lines = b.path, b.lines
	s.cx, s.cy, s.goalX = b.cx, b.cy, b.goalX
	s.scrollX, s.scrollY = b.scrollX, b.scrollY
	s.selX, s.selY, s.hasSel = b.selX, b.selY, b.hasSel
	s.dirty, s.savedMark = b.dirty, b.savedMark
	s.hl, s.hlDirty = b.hl, b.hlDirty
	s.undo, s.redo, s.typing = b.undo, b.redo, b.typing
	s.selecting = false
	s.widthStale = true
	s.focus = edFocusWhole()
	s.refocus()
	s.clampCursor()
	s.ensureVisible()
	edSetTitle(s.path)
}

// syncBuffer writes the live document back into the buffer list, which the
// list dialog and the dirty checks read.
func (s *edState) syncBuffer() {
	if s.bufIndex >= 0 && s.bufIndex < len(s.buffers) {
		s.buffers[s.bufIndex] = s.captureBuffer()
	}
}

// anyDirty reports whether any open buffer has unsaved changes.
func (s *edState) anyDirty() bool {
	if s.dirty {
		return true
	}
	for i, b := range s.buffers {
		if i != s.bufIndex && b.dirty {
			return true
		}
	}
	return false
}

// edSwitchBuffer parks the live document and brings up buffer i.
func edSwitchBuffer(s *edState, i int) {
	if i < 0 || i >= len(s.buffers) || i == s.bufIndex {
		return
	}
	s.syncBuffer()
	s.bufIndex = i
	s.restoreBuffer(s.buffers[i])
}

// edCycleBuffer moves delta places through the buffer list, wrapping.
func edCycleBuffer(s *edState, delta int) {
	if len(s.buffers) < 2 {
		s.toast.Notify("Only one buffer open")
		return
	}
	n := len(s.buffers)
	edSwitchBuffer(s, ((s.bufIndex+delta)%n+n)%n)
	s.toast.Notify(fmt.Sprintf("[%d/%d] %s", s.bufIndex+1, n, s.path))
}

// edFindBuffer returns the index of the buffer holding path, or -1.
func edFindBuffer(s *edState, path string) int {
	want := filepath.ToSlash(path)
	for i, b := range s.buffers {
		if filepath.ToSlash(b.path) == want {
			return i
		}
	}
	return -1
}

// edOpenBuffer appends a buffer holding the given document and switches to it.
func edOpenBuffer(s *edState, path string, lines []string) {
	s.syncBuffer()
	s.buffers = append(s.buffers, edBuffer{})
	s.bufIndex = len(s.buffers) - 1
	s.restoreBuffer(edBuffer{lines: []string{""}})
	s.setDocument(path, lines)
	s.syncBuffer()
}

// edUntitledName returns a buffer name not already open, so several scratch
// buffers stay tellable apart.
func edUntitledName(s *edState) string {
	for n := 1; ; n++ {
		name := "untitled.lua"
		if n > 1 {
			name = fmt.Sprintf("untitled-%d.lua", n)
		}
		if edFindBuffer(s, name) < 0 {
			return name
		}
	}
}

// edCloseBuffer drops the current buffer and moves to a neighbour. Closing the
// last one leaves a single empty scratch buffer rather than no editor at all.
func edCloseBuffer(s *edState) {
	closed := s.path
	if len(s.buffers) <= 1 {
		s.buffers = s.buffers[:0]
		s.bufIndex = 0
		s.buffers = append(s.buffers, edBuffer{})
		s.restoreBuffer(edBuffer{lines: []string{""}})
		s.setDocument("untitled.lua", []string{""})
		s.syncBuffer()
		s.toast.Notify("Closed " + closed)
		return
	}

	s.buffers = append(s.buffers[:s.bufIndex], s.buffers[s.bufIndex+1:]...)
	if s.bufIndex >= len(s.buffers) {
		s.bufIndex = len(s.buffers) - 1
	}
	s.restoreBuffer(s.buffers[s.bufIndex])
	s.toast.Notify(fmt.Sprintf("Closed %s - %d open", closed, len(s.buffers)))
}

// edBufferRow renders one row of the buffer list: a dirty marker, the buffer
// number and its path.
func edBufferRow(i int, b edBuffer) string {
	mark := " "
	if b.dirty {
		mark = "*"
	}
	name := b.path
	if name == "" {
		name = "(untitled)"
	}
	return fmt.Sprintf("%s %2d  %s", mark, i+1, edEllipsis(filepath.ToSlash(name), 30))
}

// edBufferTitle is the window frame's title: the file name, its buffer position
// when more than one is open, and a dirty marker.
func edBufferTitle(s *edState) string {
	name := filepath.Base(s.path)
	if len(s.buffers) > 1 {
		name = fmt.Sprintf("[%d/%d] %s", s.bufIndex+1, len(s.buffers), name)
	}
	if s.dirty {
		name += " *"
	}
	if s.focus.active && s.focus.name != "" {
		name += " - " + s.focus.name + "()"
	}
	return name
}

// edReadFile reads a source file into editor lines. A trailing newline becomes
// a final empty line, so joining the lines back up round-trips exactly.
func edReadFile(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines, nil
}
