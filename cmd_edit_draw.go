package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gen2brain/raylib-go/raygui"
	rl "github.com/gen2brain/raylib-go/raylib"
)

// ── drawing ───────────────────────────────────────────────────────────────────
//
// Everything is laid out on the character grid: edDrawChar and edDrawStr put a
// glyph in a cell of the chrome font, edDrawTxtChar and edDrawTxtStr do the
// same in the zoomable source-text face. See the font section in cmd_edit.go.

// edDrawChar draws one glyph centred in the grid cell whose top-left is (x,y).
// Blanks are skipped so the caller's background shows through.
func edDrawChar(x, y int32, r rune, col rl.Color) {
	if r == ' ' || r == '\t' {
		return
	}
	rl.DrawTextCodepoint(edFont, r, rl.NewVector2(float32(x), float32(y+edGlyphDY)), edFontSize, col)
}

// edDrawStr lays text out on the fixed character grid rather than using the
// font's own advances, so columns line up everywhere.
func edDrawStr(x, y int32, text string, col rl.Color) {
	for i, r := range []rune(text) {
		edDrawChar(x+int32(i)*edCharW, y, r, col)
	}
}

// edDrawTxtChar and edDrawTxtStr are the same, in the source-text face.
func edDrawTxtChar(x, y int32, r rune, col rl.Color) {
	if r == ' ' || r == '\t' {
		return
	}
	rl.DrawTextCodepoint(edTxtFont, r, rl.NewVector2(float32(x), float32(y+edTxtGlyphDY)), edTxtSize, col)
}

func edDrawTxtStr(x, y int32, text string, col rl.Color) {
	for i, r := range []rune(text) {
		edDrawTxtChar(x+int32(i)*edTxtCharW, y, r, col)
	}
}

func edDrawScene(s *edState) {
	modal := s.dlg != edDlgNone || s.confirm.Active || s.showHelp || s.menuOpen >= 0

	rl.ClearBackground(edEgaBlack)
	edDrawMenuBar(s)
	edDrawFrame(s)
	edDrawText(s)

	if modal {
		raygui.Lock()
	}
	edDrawScrollbars(s)
	if modal {
		raygui.Unlock()
	}

	edDrawAssist(s)
	edDrawStatusBar(s)
	edDrawDialog(s)
	edDrawMenuDrop(s)
	edDrawHelp(s)
	s.toast.Draw()

	s.dlgFresh = false

	if s.confirm.Draw() {
		act := s.pendingAct
		s.pendingAct = edActNone
		s.confirmed = true // let this one action past the dirty guard
		edApply(s, act)
		s.confirmed = false
	}
}

func edDrawMenuBar(s *edState) {
	rl.DrawRectangle(0, 0, virtualW, edMenuH, edEgaLightGray)
	rl.DrawLine(0, edMenuH-1, virtualW, edMenuH-1, edEgaDarkGray)
	for i, m := range edMenus {
		r := edMenuTitleRect(i)
		fg, hot := edEgaBlack, edEgaRed
		if s.menuOpen == i {
			rl.DrawRectangle(int32(r.X), 0, int32(r.Width), edMenuH-1, edEgaBlue)
			fg, hot = edEgaWhite, edEgaYellow
		}
		x := int32(r.X) + edCharW
		edDrawStr(x, edMenuTextY, m.title, fg)
		edDrawChar(x, edMenuTextY, rune(m.title[0]), hot)
	}
}

func edDrawFrame(s *edState) {
	rl.DrawRectangle(edFrameX, edFrameY, edFrameW, edFrameH, edEgaBlue)
	rl.DrawRectangleLines(edFrameX, edFrameY, edFrameW, edFrameH, edEgaLightGray)

	// Title strip: a rule of '=' with the file name punched out of the middle.
	ty := edFrameY + 1
	inner := edFrameW - 2
	edDrawStr(edFrameX+1, ty, strings.Repeat("=", int(inner/edCharW)), edEgaLightGray)

	cb := edCloseBoxRect()
	rl.DrawRectangle(int32(cb.X), int32(cb.Y), int32(cb.Width), int32(cb.Height), edEgaBlue)
	edDrawStr(int32(cb.X), ty, "[x]", edEgaLightGray)

	title := " " + edBufferTitle(s) + " "
	tw := int32(len([]rune(title))) * edCharW
	tx := edFrameX + 1 + (inner-tw)/2
	rl.DrawRectangle(tx, ty, tw, edLineH, edEgaBlue)
	edDrawStr(tx, ty, title, edEgaWhite)
}

// edSelCols returns the selected column range [from,to) on line ln. The range
// may extend one past the line end to mark a selected newline.
func (s *edState) edSelCols(ln int) (int, int) {
	if !s.hasSel {
		return -1, -1
	}
	ax, ay, bx, by := s.selRange()
	if ln < ay || ln > by {
		return -1, -1
	}
	from, to := 0, s.lineLen(ln)+1
	if ln == ay {
		from = ax
	}
	if ln == by {
		to = bx
	}
	return from, to
}

func edDrawText(s *edState) {
	cols := edCols(s)
	x0 := edTextX0(s)

	if s.showLineNums {
		rl.DrawRectangle(edFrameX+1, edTextY0, edGutterW(s), edTextH, edClrGutter)
	}

	top, bottom := s.viewTop(), s.viewBottom()
	var guides []int
	if s.showGuides {
		guides = edGuideDepths(s.lines, top+s.scrollY, min(bottom, top+s.scrollY+edRows-1))
	}

	for row := 0; row < edRows; row++ {
		ln := top + s.scrollY + row
		if ln > bottom {
			break
		}
		y := edTextY0 + int32(row)*edTxtLineH

		if s.showLineNums {
			num := edEgaDarkGray
			if d, ok := edDiagnosticAt(s, ln); ok {
				num = edDiagnosticColor(d)
			}
			edDrawTxtStr(edFrameX+1+edPad, y, fmt.Sprintf("%4d", ln+1), num)
		}
		edDrawDiagnosticMarks(s, ln, y)

		if s.focus.sealed(ln) {
			rl.DrawRectangle(x0, y, edVScrX-x0-edPad, edTxtLineH, edClrSealed)
		}
		if row < len(guides) {
			edDrawGuides(s, guides[row], x0, y)
		}

		runes := s.runes(ln)
		var kinds []edTokKind
		if s.showSyntax && ln < len(s.hl) {
			kinds = s.hl[ln]
		}
		selFrom, selTo := s.edSelCols(ln)

		for c := 0; c < cols; c++ {
			idx := s.scrollX + c
			cx := x0 + int32(c)*edTxtCharW
			selected := idx >= selFrom && idx < selTo
			if selected {
				rl.DrawRectangle(cx, y, edTxtCharW, edTxtLineH, edEgaLightGray)
			}
			if idx >= len(runes) {
				continue
			}
			col := edEgaYellow
			if kinds != nil && idx < len(kinds) {
				col = edKindColor(kinds[idx])
			}
			if selected {
				col = edEgaBlack
			}
			edDrawTxtChar(cx, y, runes[idx], col)
		}
	}

	edDrawBlockMatch(s)
	edDrawCursor(s)
}

// edDrawGuides rules a faint line down each level of indentation on one row.
func edDrawGuides(s *edState, indent int, x0, y int32) {
	for col := s.indentWidth; col < indent; col += s.indentWidth {
		if col < s.scrollX {
			continue
		}
		x := x0 + int32(col-s.scrollX)*edTxtCharW
		if x >= edVScrX-edPad {
			break
		}
		rl.DrawRectangle(x, y, 1, edTxtLineH, edClrGuide)
	}
}

// edDrawBlockMatch boxes the keyword under the caret and the one that closes
// it, so an "end" can be told from the block it belongs to at a glance.
func edDrawBlockMatch(s *edState) {
	if s.dlg != edDlgNone || s.showHelp {
		return
	}
	a, b, ok := edMatchBlock(s)
	if !ok {
		return
	}
	edOutlineSpan(s, a)
	edOutlineSpan(s, b)
}

// edOutlineSpan draws a box around one keyword, when it is on screen.
func edOutlineSpan(s *edState, span edBlockSpan) {
	row := span.line - s.viewTop() - s.scrollY
	if row < 0 || row >= edRows {
		return
	}
	from := max(span.from, s.scrollX)
	to := min(span.to, s.scrollX+edCols(s))
	if to <= from {
		return
	}
	x := edTextX0(s) + int32(from-s.scrollX)*edTxtCharW
	y := edTextY0 + int32(row)*edTxtLineH
	rl.DrawRectangleLines(x-1, y, int32(to-from)*edTxtCharW+2, edTxtLineH, edClrMatch)
}

func edDrawCursor(s *edState) {
	if s.dlg != edDlgNone || s.menuOpen >= 0 || s.confirm.Active || s.showHelp {
		return
	}
	if int(rl.GetTime()*2)%2 == 1 {
		return
	}
	row := s.cy - s.viewTop() - s.scrollY
	col := s.cx - s.scrollX
	if row < 0 || row >= edRows || col < 0 || col >= edCols(s) {
		return
	}
	x := edTextX0(s) + int32(col)*edTxtCharW
	y := edTextY0 + int32(row)*edTxtLineH
	rl.DrawRectangle(x, y, edTxtCharW, edTxtLineH, edEgaWhite)
	if r := s.runes(s.cy); s.cx < len(r) {
		edDrawTxtChar(x, y, r[s.cx], edEgaBlue)
	}
}

func edDrawScrollbars(s *edState) {
	vr := rl.NewRectangle(float32(edVScrX), float32(edTextY0), float32(edScrollW), float32(edTextH))
	if maxY := int32(max(0, s.viewLen()-edRows)); maxY > 0 {
		s.scrollY = int(raygui.ScrollBar(vr, int32(s.scrollY), 0, maxY))
	} else {
		rl.DrawRectangle(int32(vr.X), int32(vr.Y), edScrollW, edTextH, edEgaDarkGray)
		s.scrollY = 0
	}

	// Corner between the two bars, so no editor blue shows through.
	rl.DrawRectangle(edVScrX, edHScrY, edScrollW, edScrollW, edEgaDarkGray)

	hw := edVScrX - edFrameX - 1
	hr := rl.NewRectangle(float32(edFrameX+1), float32(edHScrY), float32(hw), float32(edScrollW))
	if maxX := int32(max(0, s.maxLineLen()-edCols(s))); maxX > 0 {
		s.scrollX = int(raygui.ScrollBar(hr, int32(s.scrollX), 0, maxX))
	} else {
		rl.DrawRectangle(int32(hr.X), int32(hr.Y), hw, edScrollW, edEgaDarkGray)
		s.scrollX = 0
	}
}

func edDrawStatusBar(s *edState) {
	y := virtualH - statusBarH
	rl.DrawRectangle(0, y, virtualW, statusBarH, edEgaLightGray)
	rl.DrawLine(0, y, virtualW, y, edEgaWhite)

	right := fmt.Sprintf("Ln %d/%d Col %d", s.cy+1, len(s.lines), s.cx+1)
	if s.dirty {
		right = "* " + right
	}
	room := int((virtualW - 2*edCharW - int32(len([]rune(right)))*edCharW) / edCharW)

	// A problem on the caret's line takes the status bar over from the key
	// hints; it is the more useful thing to be told at that moment.
	if d, ok := edDiagnosticAt(s, s.cy); ok {
		tag := "Warning: "
		if d.isError() {
			tag = "Error: "
		}
		edDrawStr(edCharW, y+edStatusTextY, tag, edDiagnosticColor(d))
		edDrawStr(edCharW+int32(len(tag))*edCharW, y+edStatusTextY,
			edEllipsis(d.message, room-len(tag)-1), edEgaBlack)
		edDrawStr(virtualW-edCharW-int32(len([]rune(right)))*edCharW, y+edStatusTextY, right, edEgaBlack)
		return
	}

	hints := []struct{ key, label string }{
		{"F1", "Help"}, {"F2", "Save"}, {"F3", "Open"},
		{"F5", "Run"}, {"F7", "Format"}, {"^Q", "Quit"},
	}
	x := edCharW
	for _, h := range hints {
		edDrawStr(x, y+edStatusTextY, h.key, edEgaRed)
		x += int32(len(h.key)+1) * edCharW
		edDrawStr(x, y+edStatusTextY, h.label, edEgaBlack)
		x += int32(len(h.label)+2) * edCharW
	}

	// Otherwise a short tally next to the position, so that problems elsewhere
	// in the file are not invisible. There is only room for a few characters
	// between the key hints and the line counter, hence "2E 3L".
	if errs, others := edDiagnosticCounts(s); errs+others > 0 {
		tally := ""
		if errs > 0 {
			tally = fmt.Sprintf("%dE", errs)
		}
		if others > 0 {
			if tally != "" {
				tally += " "
			}
			tally += fmt.Sprintf("%dL", others)
		}
		col := edClrWarn
		if errs > 0 {
			col = edClrError
		}
		if tx := virtualW - edCharW - int32(len([]rune(right))+2+len(tally))*edCharW; tx > x {
			edDrawStr(tx, y+edStatusTextY, tally, col)
		}
	}

	edDrawStr(virtualW-edCharW-int32(len([]rune(right)))*edCharW, y+edStatusTextY, right, edEgaBlack)
}

func edDrawMenuDrop(s *edState) {
	if s.menuOpen < 0 {
		return
	}
	r := edMenuDropRect(s.menuOpen)
	rx, ry, rw, rh := int32(r.X), int32(r.Y), int32(r.Width), int32(r.Height)

	rl.DrawRectangle(rx+3, ry+3, rw, rh, rl.NewColor(0, 0, 0, 120))
	rl.DrawRectangle(rx, ry, rw, rh, edEgaLightGray)
	rl.DrawRectangleLines(rx, ry, rw, rh, edEgaBlack)

	y := ry + edPad
	for i, it := range edMenus[s.menuOpen].items {
		if it.label == "" {
			rl.DrawLine(rx+2, y+1, rx+rw-2, y+1, edEgaDarkGray)
			y += 4
			continue
		}
		fg, hot, key := edEgaBlack, edEgaRed, edEgaDarkGray
		if s.menuHover == i {
			rl.DrawRectangle(rx+1, y, rw-2, edLineH, edEgaBlue)
			fg, hot, key = edEgaWhite, edEgaYellow, edEgaLightGray
		}
		if edMenuItemChecked(s, it.act) {
			edDrawStr(rx+edCharW, y, "*", fg)
		}
		x := rx + 3*edCharW
		edDrawStr(x, y, it.label, fg)
		edDrawChar(x, y, rune(it.label[0]), hot)
		if it.key != "" {
			edDrawStr(rx+rw-edCharW-int32(len(it.key))*edCharW, y, it.key, key)
		}
		y += edLineH
	}
}

// ── list dialogs ──────────────────────────────────────────────────────────────

// edListDlgRect is the panel of the Open/Outline/Buffers dialogs, and
// edListViewRect the list inside it. Both the drawing and the keyboard handler
// need them.
func edListDlgRect() (x, y, w, h int32) {
	w, h = int32(420), int32(250)
	return (virtualW - w) / 2, (virtualH - h) / 2, w, h
}

func edListViewRect() rl.Rectangle {
	x, y, w, h := edListDlgRect()
	return rl.NewRectangle(float32(x+8), float32(y+24), float32(w-16), float32(h-62))
}

// edListVisibleRows is how many rows fit inside a list dialog's border.
func edListVisibleRows() int {
	return max(int(edListViewRect().Height-2)/int(edLineH), 1)
}

// edListMove walks the selection in a list dialog and scrolls to follow it.
func edListMove(s *edState, delta int) {
	n := len(s.dlgList)
	if n == 0 {
		return
	}
	s.dlgActive = int32(max(0, min(int(s.dlgActive)+delta, n-1)))
	edListFollow(s)
}

// edListFollow scrolls the list so the selected row is visible.
func edListFollow(s *edState) {
	s.dlgScroll = int32(edScrollIntoView(
		int(s.dlgScroll), int(s.dlgActive), edListVisibleRows(), len(s.dlgList)))
}

// edScrollIntoView nudges a scroll offset just far enough to bring the selected
// row inside a window of the given height, and keeps it within the list.
func edScrollIntoView(scroll, selected, rows, count int) int {
	if selected < scroll {
		scroll = selected
	}
	if selected >= scroll+rows {
		scroll = selected - rows + 1
	}
	return max(0, min(scroll, max(0, count-rows)))
}

// edDrawListView paints the rows of a list dialog and handles clicking and
// wheel-scrolling in it. raygui's own list view draws every row in one colour,
// which the function outline needs to break, so the dialogs use this instead.
func edDrawListView(s *edState, r rl.Rectangle, empty string, rowColor func(int) rl.Color) {
	x, y := int32(r.X), int32(r.Y)
	w, h := int32(r.Width), int32(r.Height)
	rl.DrawRectangle(x, y, w, h, edEgaBlue)
	rl.DrawRectangleLines(x, y, w, h, edEgaBlack)

	if len(s.dlgList) == 0 {
		edDrawStr(x+edCharW, y+6, empty, edEgaYellow)
		return
	}

	vis, n := edListVisibleRows(), len(s.dlgList)
	rowW := w - 2
	if n > vis {
		bar := rl.NewRectangle(float32(x+w-1-edScrollW), float32(y+1), float32(edScrollW), float32(h-2))
		s.dlgScroll = raygui.ScrollBar(bar, s.dlgScroll, 0, int32(n-vis))
		rowW -= edScrollW
	}
	s.dlgScroll = int32(max(0, min(int(s.dlgScroll), max(0, n-vis))))

	rows := rl.NewRectangle(float32(x+1), float32(y+1), float32(rowW), float32(h-2))
	hover := -1
	if !s.dlgFresh && rl.CheckCollisionPointRec(rl.GetMousePosition(), rows) {
		if row := int((rl.GetMousePosition().Y - rows.Y) / float32(edLineH)); row >= 0 && row < vis {
			if i := int(s.dlgScroll) + row; i < n {
				hover = i
				if rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
					s.dlgActive = int32(i)
				}
			}
		}
		if wheel := rl.GetMouseWheelMove(); wheel != 0 {
			s.dlgScroll = int32(max(0, min(int(s.dlgScroll)-int(wheel*3), max(0, n-vis))))
		}
	}

	for row := 0; row < vis; row++ {
		i := int(s.dlgScroll) + row
		if i >= n {
			break
		}
		ry := y + 1 + int32(row)*edLineH
		fg := edEgaYellow
		if rowColor != nil {
			fg = rowColor(i)
		}
		switch i {
		case int(s.dlgActive):
			rl.DrawRectangle(x+1, ry, rowW, edLineH, edEgaLightGray)
			fg = edEgaBlack
		case hover:
			rl.DrawRectangle(x+1, ry, rowW, edLineH, edEgaCyan)
			fg = edEgaWhite
		}
		edDrawStr(x+4, ry, s.dlgList[i], fg)
	}
}

// edOutlineColor tells the outline's entries apart at a glance: the file-start
// entry, functions visible to the whole project, file-local ones, and those
// declared inside another function rather than at the top level.
func edOutlineColor(s *edState, i int) rl.Color {
	if i < 0 || i >= len(s.dlgFuncs) {
		return edEgaYellow
	}
	switch fn := s.dlgFuncs[i]; {
	case fn.name == edTopOfFile:
		return edEgaWhite
	case fn.nested():
		return edEgaLightGray
	case fn.local:
		return edEgaLtCyan
	default:
		return edEgaYellow
	}
}

// edBtnH is the dialog button height for the current font's cell.
func edBtnH() int32 { return edLineH + 6 }

// edDialogFrame paints the shared modal chrome and returns the panel origin.
func edDialogFrame(title string, w, h int32) (int32, int32) {
	rl.DrawRectangle(0, 0, virtualW, virtualH, rl.NewColor(0, 0, 0, 140))
	x, y := (virtualW-w)/2, (virtualH-h)/2
	rl.DrawRectangle(x+4, y+4, w, h, rl.NewColor(0, 0, 0, 120))
	rl.DrawRectangle(x, y, w, h, edEgaLightGray)
	rl.DrawRectangleLines(x, y, w, h, edEgaBlack)
	rl.DrawRectangle(x+1, y+1, w-2, edLineH+2, edEgaBlue)
	t := " " + title + " "
	edDrawStr(x+(w-int32(len([]rune(t)))*edCharW)/2, y+2, t, edEgaWhite)
	return x, y
}

// edMenuItemChecked reports whether a menu item shows a tick: the view and
// option toggles when on, and whichever font is currently loaded.
func edMenuItemChecked(s *edState, act edAction) bool {
	switch act {
	case edActLineNums:
		return s.showLineNums
	case edActSyntax:
		return s.showSyntax
	case edActGuides:
		return s.showGuides
	case edActFocus:
		return s.focusMode
	case edActFontUnscii:
		return edFontKindID == edFontUnscii
	case edActFontVGA:
		return edFontKindID == edFontVGA
	case edActUseLSP:
		return edUseLSP
	case edActForceStylua:
		return edForceStylua
	case edActFormatOnSave:
		return edFormatOnSave
	case edActAutoClose:
		return edAutoClose
	}
	return false
}

func edDrawDialog(s *edState) {
	if s.dlg == edDlgNone {
		return
	}
	if s.dlgFresh {
		// Draw it, but inert: the input that opened it is still live.
		raygui.Lock()
		defer raygui.Unlock()
	}

	switch s.dlg {
	case edDlgOpen, edDlgOutline, edDlgBuffers:
		title, empty, okLabel := "Open Lua File", "No .lua files found here", "Open"
		switch s.dlg {
		case edDlgOutline:
			title = "Functions in " + edEllipsis(filepath.Base(s.path), 24)
			empty = "No functions in this file"
			okLabel = "Go To"
		case edDlgBuffers:
			title = "Open Buffers"
			empty = "No buffers open"
			okLabel = "Switch"
		}
		_, _, w, h := edListDlgRect()
		x, y := edDialogFrame(title, w, h)

		var rowColor func(int) rl.Color
		if s.dlg == edDlgOutline {
			rowColor = func(i int) rl.Color { return edOutlineColor(s, i) }
		}
		edDrawListView(s, edListViewRect(), empty, rowColor)

		// Buttons are laid out from the right edge, so the extra Close button
		// on the buffer list just pushes the others along.
		by := float32(y+h) - float32(edBtnH()) - 6
		bh, bw := float32(edBtnH()), float32(66)
		right := float32(x + w - 10)
		btn := func(label string) bool {
			right -= bw
			hit := raygui.Button(rl.NewRectangle(right, by, bw, bh), label)
			right -= 8
			return hit
		}

		if btn("Cancel") {
			s.dlg = edDlgNone
		}
		if s.dlg == edDlgBuffers && btn("Close") {
			// Close acts on the highlighted buffer: switch to it first so the
			// unsaved-changes prompt is about the buffer being closed.
			edSwitchBuffer(s, int(s.dlgActive))
			s.dlg = edDlgNone
			edApply(s, edActCloseBuf)
			return
		}
		if btn(okLabel) {
			edDialogConfirm(s)
		}

	case edDlgReplace:
		const w = int32(340)
		boxH := edLineH + 8
		stride := edLineH + boxH + 10 // label, gap, box, gap
		h := 2*edLineH + 2*stride + edBtnH() + 6

		title := "Replace"
		if s.replaceFrom >= 0 {
			title = fmt.Sprintf("Replace in lines %d-%d", s.replaceFrom+1, s.replaceTo+1)
		}
		x, y := edDialogFrame(title, w, h)

		// Two fields, only the focused one in edit mode: raygui hands every
		// text box in edit mode the same keystrokes, so a second live one would
		// type into both. Tab moves between them, as does a click.
		box := func(row int, label string, text *string) bool {
			ly := y + edLineH + 8 + int32(row)*stride
			edDrawStr(x+edCharW, ly, label, edEgaBlack)

			r := rl.NewRectangle(float32(x+edCharW), float32(ly+edLineH+2),
				float32(w-2*edCharW), float32(boxH))
			focused := s.dlgField == row
			if !focused && rl.CheckCollisionPointRec(rl.GetMousePosition(), r) &&
				rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
				s.dlgField = row
			}
			// raygui only fills a text box while it is in edit mode, leaving an
			// idle one transparent over the grey panel, so paint the field here
			// and let raygui draw the text and caret on top.
			rl.DrawRectangle(int32(r.X), int32(r.Y), int32(r.Width), int32(r.Height), edEgaBlue)
			return raygui.TextBox(r, text, 256, focused) && focused &&
				(rl.IsKeyPressed(rl.KeyEnter) || rl.IsKeyPressed(rl.KeyKpEnter))
		}
		enter := box(0, "Search for:", &s.dlgInput)
		enter = box(1, "Replace with:", &s.dlgReplace) || enter
		if enter {
			edDialogConfirm(s)
			return
		}

		by := float32(y+h) - float32(edBtnH()) - 6
		bh, bw := float32(edBtnH()), float32(74)
		right := float32(x + w - 10)
		btn := func(label string) bool {
			right -= bw
			hit := raygui.Button(rl.NewRectangle(right, by, bw, bh), label)
			right -= 6
			return hit
		}
		if btn("Cancel") {
			s.dlg = edDlgNone
			return
		}
		if btn("All") {
			edTakeReplaceTerms(s)
			edReplaceAll(s)
			s.dlg = edDlgNone
			return
		}
		if btn("Replace") {
			edTakeReplaceTerms(s)
			edReplaceOnce(s)
		}

	case edDlgSaveAs, edDlgFind, edDlgGoto:
		title, prompt := "Save As", "File name:"
		switch s.dlg {
		case edDlgFind:
			title, prompt = "Find", "Search for:"
		case edDlgGoto:
			title, prompt = "Go to Line", "Line number:"
		}
		// Rows are stacked from the character cell so the panel fits either font.
		const w = int32(300)
		boxH := edLineH + 8
		h := 2*edLineH + boxH + 46
		x, y := edDialogFrame(title, w, h)

		promptY := y + edLineH + 8
		edDrawStr(x+edCharW, promptY, prompt, edEgaBlack)
		// raygui ends edit mode on ENTER *or* on a click outside the box, and
		// reports both the same way. Only the key counts as confirmation here,
		// so a stray click on the backdrop cannot trigger a Save As.
		box := rl.NewRectangle(float32(x+edCharW), float32(promptY+edLineH+4),
			float32(w-2*edCharW), float32(boxH))
		if raygui.TextBox(box, &s.dlgInput, 256, true) &&
			(rl.IsKeyPressed(rl.KeyEnter) || rl.IsKeyPressed(rl.KeyKpEnter)) {
			edDialogConfirm(s)
			return
		}

		by := float32(y+h) - float32(edBtnH()) - 6
		if raygui.Button(rl.NewRectangle(float32(x+w-150), by, 66, float32(edBtnH())), "OK") {
			edDialogConfirm(s)
			return
		}
		if raygui.Button(rl.NewRectangle(float32(x+w-76), by, 66, float32(edBtnH())), "Cancel") {
			s.dlg = edDlgNone
		}
	}
}
