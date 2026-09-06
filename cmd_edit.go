package main

import (
	_ "embed"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gen2brain/raylib-go/raygui"
	rl "github.com/gen2brain/raylib-go/raylib"
)

// ── fz edit ───────────────────────────────────────────────────────────────────
//
// A Lua source editor drawn on the same 640×480 virtual canvas as the gfx and
// map editors, using the same raygui widgets for its dialogs. The look is
// deliberately borrowed from Turbo Pascal / QBasic / PICO-8: a light-gray menu
// bar with red hotkeys, an EGA-blue editing window with a framed title, and a
// key-hint status bar along the bottom.

// ── EGA palette ───────────────────────────────────────────────────────────────

var (
	edEgaBlack     = rl.NewColor(0, 0, 0, 255)
	edEgaBlue      = rl.NewColor(0, 0, 170, 255)
	edEgaCyan      = rl.NewColor(0, 170, 170, 255)
	edEgaRed       = rl.NewColor(170, 0, 0, 255)
	edEgaLightGray = rl.NewColor(170, 170, 170, 255)
	edEgaDarkGray  = rl.NewColor(85, 85, 85, 255)
	edEgaLtGreen   = rl.NewColor(85, 255, 85, 255)
	edEgaLtCyan    = rl.NewColor(85, 255, 255, 255)
	edEgaLtRed     = rl.NewColor(255, 85, 85, 255)
	edEgaLtMagenta = rl.NewColor(255, 85, 255, 255)
	edEgaYellow    = rl.NewColor(255, 255, 85, 255)
	edEgaWhite     = rl.NewColor(255, 255, 255, 255)

	// Gutter is a shade darker than the editing background so line numbers
	// read as chrome rather than as code, and the read-only rows of a focused
	// function are tinted the same way.
	edClrGutter = rl.NewColor(0, 0, 120, 255)
	edClrSealed = rl.NewColor(0, 0, 128, 255)

	// Problem markers: errors shout, lints murmur.
	edClrError = rl.NewColor(255, 85, 85, 255)
	edClrWarn  = rl.NewColor(255, 255, 85, 255)
)

// edKindColor maps a highlighter token kind to its on-screen colour.
func edKindColor(k edTokKind) rl.Color {
	switch k {
	case edKindKeyword:
		return edEgaWhite
	case edKindBuiltin:
		return edEgaLtCyan
	case edKindString:
		return edEgaLtGreen
	case edKindNumber:
		return edEgaLtMagenta
	case edKindComment:
		return edEgaLightGray
	case edKindOp:
		return edEgaLtRed
	default:
		return edEgaYellow
	}
}

// ── layout ────────────────────────────────────────────────────────────────────
//
// Everything is laid out on a character grid, so most of the geometry follows
// from the active font's cell size rather than being fixed. The values below
// the const block are recomputed by edRecalcLayout whenever the font changes.

const (
	edFrameX  = int32(2)
	edFrameW  = virtualW - 2*edFrameX // 636
	edPad     = int32(2)
	edScrollW = int32(10)
	edVScrX   = edFrameX + edFrameW - 1 - edScrollW // vertical scrollbar x

	// How a held key behaves: the wait before it starts repeating, and the
	// interval between repeats after that.
	edRepeatDelay = 0.25
	edRepeatRate  = 0.02

	// How close together two clicks have to be to count as a double click.
	edDoubleClick = 0.4

	// How long the typing has to settle before the server is asked to re-check.
	edDiagDelay = 0.4

	edDefaultIndent = 2
	edMaxUndo       = 200
	edCommentMark   = "-- "

	// The outline's synthetic first entry, always present so the picker is
	// never empty and there is always a jump back to line 1.
	edTopOfFile = "(top of file)"
)

var (
	edLineH  = int32(12) // one text row, including leading
	edMenuH  = int32(16)
	edFrameY = int32(18)
	edFrameH = int32(440)

	// Text area: below the framed title strip, above the horizontal scrollbar.
	edTextY0 int32
	edTextH  int32
	edRows   int   // visible text rows
	edHScrY  int32 // horizontal scrollbar y

	// Vertical centring of a text cell inside the menu and status bars.
	edMenuTextY   int32
	edStatusTextY int32
)

// edRecalcLayout derives the window geometry from the current text row height.
func edRecalcLayout() {
	edMenuH = edLineH + 4
	edMenuTextY = (edMenuH - edLineH) / 2
	edFrameY = edMenuH + 2
	edFrameH = virtualH - statusBarH - edFrameY - 2
	edTextY0 = edFrameY + 1 + edLineH
	edTextH = edFrameH - 2 - edLineH - edScrollW
	edRows = int(edTextH / edTxtLineH)
	edHScrY = edFrameY + edFrameH - 1 - edScrollW
	edStatusTextY = max((statusBarH-edLineH)/2, 0)
}

// ── fonts ─────────────────────────────────────────────────────────────────────

// The editor draws every glyph on a fixed character grid. Two bitmap-style
// faces are embedded and switchable from the Options menu; if neither loads we
// fall back to raylib's built-in font and widen the cell enough that its
// variable-width glyphs still line up.

//go:embed 3pp/fonts/Flexi_IBM_VGA_True.ttf
var vgaFontTTF []byte

type edFontKind int

const (
	edFontUnscii edFontKind = iota
	edFontVGA
)

// edFontStep is one entry on a face's size ladder: the raylib font size, the
// character cell it fills, and where the glyph sits inside that cell.
type edFontStep struct {
	size    int32
	lineH   int32
	glyphDY int32
}

type edFontDef struct {
	label string
	steps []edFontStep // ascending
	def   int          // index of the default step, also used for the chrome
}

// Both faces end up 8 px wide at zoom 1; unscii-8 is an 8x8 cell, the IBM VGA
// face an 8x16 one, which is what gives the latter a true 80x25 DOS window.
//
// The base sizes are not the cell heights. raylib scales a face by
// ascent-descent, and the VGA font declares a 1600-unit em around a character
// cell that is only 675x1350 units, so asking for 16 renders the cell at
// 6.75x13.5 px - every stem on a half pixel. 19 is the size at which its
// advance comes out at exactly 8 px and its ink at exactly 16.
//
// Zoom steps multiply the base size, because a pixel face only stays even at
// whole multiples: 1.5x would draw some stems one pixel wide and others two.
// The cells are design values, not measurements: both faces draw ink outside
// their character cell (unscii's descenders fill all 8 rows, and the VGA face's
// '$' overshoots its 16-row cell into the em's padding), so measuring the ink
// extent would give 19 rows for what is an 8x16 font.
//
// unscii-8 is an 8px bitmap face, so it only has its native size and a clean
// double. The VGA face is an outline approximation of an 8x16 cell on a
// 1600-unit em, and rasterises unevenly wherever a design pixel does not land
// near a whole screen pixel - at 19 (its nominal 8x16) some stems come out two
// pixels wide and others one. 16 and 18 land better; the ladder keeps 19 for
// the authentic 80x25 window and offers the cleaner sizes either side.
var edFontDefs = [...]edFontDef{
	edFontUnscii: {"unscii-8", []edFontStep{
		{8, 12, 2},
		{16, 24, 4},
	}, 0},
	edFontVGA: {"IBM VGA", []edFontStep{
		{10, 8, -1},
		{12, 10, -2},
		{16, 14, -1},
		{18, 15, -2},
		{19, 16, -2},
		{24, 20, -3},
		{38, 32, -4},
	}, 2},
}

// The chrome - menu bar, frame, dialogs, status bar - always renders at the
// face's base size, so zooming the source text does not push the menu bar off
// the 640px canvas. Only the text area follows the size ladder.
var (
	edFont        rl.Font        // chrome
	edFontGlyphs  []rl.GlyphInfo // backing store for edFont; freed on switch
	edFontLoaded  bool
	edFontKindID  = edFontUnscii
	edFontStepIdx int
	edFontSize    = float32(10)
	edCharW       = int32(8)
	edGlyphDY     = int32(1) // vertical placement of a glyph inside its cell

	edTxtFont    rl.Font // source text
	edTxtGlyphs  []rl.GlyphInfo
	edTxtSize    = float32(10)
	edTxtCharW   = int32(8)
	edTxtLineH   = int32(12)
	edTxtGlyphDY = int32(1)
)

// edFontData returns the embedded TTF for a face.
func edFontData(kind edFontKind) []byte {
	if kind == edFontVGA {
		return vgaFontTTF
	}
	data, err := templatesFS.ReadFile("templates/assets/fonts/unscii-8.ttf")
	if err != nil {
		return nil
	}
	return data
}

// edFontCodepoints is the glyph set the atlas is built from. Passing nil here
// would get raylib's default 95 ASCII glyphs, which is why umlauts used to come
// out as "?" - both bundled faces carry far more than that. This covers Latin-1
// and Latin Extended-A, plus the punctuation that turns up in pasted text.
func edFontCodepoints() []rune {
	out := make([]rune, 0, 340)
	for r := rune(32); r < 127; r++ { // ASCII
		out = append(out, r)
	}
	for r := rune(0xA0); r <= 0xFF; r++ { // Latin-1 Supplement: äöüÄÖÜß and friends
		out = append(out, r)
	}
	for r := rune(0x100); r <= 0x17F; r++ { // Latin Extended-A
		out = append(out, r)
	}
	return append(out, '\u2013', '\u2014', '\u2018', '\u2019', '\u201C', '\u201D', '\u2026', '\u20AC')
}

// edBuildFont rasterises a face as a bitmap font. Going through LoadFontData
// rather than LoadFontFromMemory is what lets us pass FONT_BITMAP, which
// thresholds the rasteriser's coverage to fully on or off - without it even a
// pixel-aligned face picks up a grey fringe on every stem.
func edBuildFont(data []byte, size int32) (rl.Font, []rl.GlyphInfo) {
	cps := edFontCodepoints()
	glyphs := rl.LoadFontData(data, size, cps, int32(len(cps)), rl.FontBitmap)
	if len(glyphs) == 0 {
		return rl.Font{}, nil
	}
	recs := make([]*rl.Rectangle, 1) // out-param: C allocates the rec array
	atlas := rl.GenImageFontAtlas(glyphs, recs, size, 1, 0)
	tex := rl.LoadTextureFromImage(&atlas)
	rl.UnloadImage(&atlas)
	if tex.ID == 0 {
		rl.UnloadFontData(glyphs)
		return rl.Font{}, nil
	}
	rl.SetTextureFilter(tex, rl.FilterPoint)

	// The glyph images stay allocated: unlike LoadFontEx we do not swap them
	// for sub-images of the atlas, which costs a few KB and keeps the glyph
	// metrics readable.
	return rl.Font{
		BaseSize:     size,
		CharsCount:   int32(len(glyphs)),
		CharsPadding: 1,
		Texture:      tex,
		Recs:         recs[0],
		Chars:        &glyphs[0],
	}, glyphs
}

// edSetFont switches the editor face, re-deriving the character cell, the
// window layout and the raygui style from it. It reports whether the requested
// face loaded; on failure the previous font is kept.
func edSetFont(kind edFontKind, step int) bool {
	def := edFontDefs[kind]
	if step < 0 || step >= len(def.steps) {
		return false
	}
	data := edFontData(kind)
	if data == nil {
		return false
	}

	// The chrome always renders at the face's default step, so changing the
	// text size never pushes the menu bar off the canvas.
	chrome, txtStep := def.steps[def.def], def.steps[step]
	ui, uiGlyphs := edBuildFont(data, chrome.size)
	if uiGlyphs == nil {
		return false
	}
	txt, txtGlyphs := ui, uiGlyphs
	if step != def.def {
		if txt, txtGlyphs = edBuildFont(data, txtStep.size); txtGlyphs == nil {
			rl.UnloadTexture(ui.Texture)
			rl.UnloadFontData(uiGlyphs)
			return false
		}
	}

	edUnloadFont()
	edFont, edFontGlyphs, edFontLoaded = ui, uiGlyphs, true
	edTxtFont, edTxtGlyphs = txt, txtGlyphs
	edFontKindID, edFontStepIdx = kind, step

	edFontSize = float32(chrome.size)
	edCharW = edMeasureCharW(ui, edFontSize, 0)
	edLineH, edGlyphDY = chrome.lineH, chrome.glyphDY

	edTxtSize = float32(txtStep.size)
	edTxtCharW = edMeasureCharW(txt, edTxtSize, 0)
	edTxtLineH, edTxtGlyphDY = txtStep.lineH, txtStep.glyphDY

	edRecalcLayout()
	edApplyGuiStyle()
	return true
}

// edLayoutUsable reports whether the current cell still leaves a workable text
// area. Used to refuse a zoom step rather than render an unusable window.
func edLayoutUsable() bool {
	const minRows, minCols = 8, 24
	cols := (edVScrX - edFrameX - 1 - edPad - 5*edTxtCharW) / edTxtCharW // widest gutter
	return edRows >= minRows && cols >= minCols
}

// edApplyFontZoom walks the active face's size ladder, reverting when a step
// would leave too little room to edit in.
func edApplyFontZoom(s *edState, delta int) {
	prev := edFontStepIdx
	want := prev + delta
	if want < 0 || want >= len(edFontDefs[edFontKindID].steps) {
		s.toast.Notify(edEdgeOfLadder(delta))
		return
	}
	if !edSetFont(edFontKindID, want) {
		s.toast.Notify("Could not load that size")
		return
	}
	if !edLayoutUsable() {
		edSetFont(edFontKindID, prev)
		s.toast.Notify(edEdgeOfLadder(delta))
		return
	}
	s.ensureVisible()
	s.toast.Notify(fmt.Sprintf("%dpx - %dx%d cell, %d cols x %d rows",
		int(edTxtSize), edTxtCharW, edTxtLineH, edCols(s), edRows))
}

func edEdgeOfLadder(delta int) string {
	if delta < 0 {
		return "Already at the smallest size"
	}
	return "Already at the largest size"
}

// edUnloadFont releases the active face, if it is one of ours.
func edUnloadFont() {
	if !edFontLoaded {
		return
	}
	if edTxtFont.Texture.ID != edFont.Texture.ID {
		rl.UnloadTexture(edTxtFont.Texture)
		rl.UnloadFontData(edTxtGlyphs)
	}
	rl.UnloadTexture(edFont.Texture)
	rl.UnloadFontData(edFontGlyphs)
	edFontLoaded = false
}

// edInitFont installs the default face, falling back to raylib's built-in font
// when neither embedded face can be loaded.
func edInitFont() {
	if edSetFont(edFontUnscii, edFontDefs[edFontUnscii].def) {
		return
	}
	edFont = rl.GetFontDefault()
	edFontLoaded = false
	edFontSize = 10
	edLineH = 12
	edCharW = edMeasureCharW(edFont, edFontSize, 1)
	edGlyphDY = 1
	edTxtFont, edTxtSize, edTxtLineH = edFont, edFontSize, edLineH
	edTxtCharW, edTxtGlyphDY = edCharW, edGlyphDY
	edRecalcLayout()
	edApplyGuiStyle()
}

// edMeasureCharW returns the widest glyph advance in the current font, plus
// tracking, rounded up to a whole pixel.
func edMeasureCharW(f rl.Font, size float32, tracking int32) int32 {
	var maxW float32
	for r := rune(33); r < 127; r++ {
		if w := rl.MeasureTextEx(f, string(r), size, 0).X; w > maxW {
			maxW = w
		}
	}
	w := int32(maxW)
	if float32(w) < maxW {
		w++
	}
	w += tracking
	// Sanity bound only: the cell scales with the zoom, so this cannot be a
	// fixed range. Falling back to the nominal 8px cell keeps the layout sane
	// if a face ever measures as nonsense.
	if w < 4 || w > 64 {
		return int32(size)
	}
	return w
}

func edGutterW(s *edState) int32 {
	if !s.showLineNums {
		return 0
	}
	return 5 * edTxtCharW // 4 digits + separating space
}

func edTextX0(s *edState) int32 { return edFrameX + 1 + edPad + edGutterW(s) }

func edCols(s *edState) int {
	w := edVScrX - edTextX0(s) - edPad
	if w < edTxtCharW {
		return 1
	}
	return int(w / edTxtCharW)
}

// ── state ─────────────────────────────────────────────────────────────────────

type edAction int

const (
	edActNone edAction = iota
	edActNew
	edActOpen
	edActSave
	edActSaveAs
	edActRun
	edActQuit
	edActUndo
	edActRedo
	edActCut
	edActCopy
	edActPaste
	edActSelectAll
	edActIndent
	edActUnindent
	edActDupLine
	edActMoveUp
	edActMoveDown
	edActIndentWidth
	edActAutoClose
	edActToggleComment
	edActComplete
	edActInlineHelp
	edActFind
	edActReplace
	edActFindNext
	edActGoto
	edActOutline
	edActLineNums
	edActSyntax
	edActFocus
	edActUseLSP
	edActForceStylua
	edActNextProblem
	edActFontUnscii
	edActFontVGA
	edActFontBigger
	edActFontSmaller
	edActFormat
	edActBuild
	edActGfx
	edActMap
	edActNextBuf
	edActPrevBuf
	edActCloseBuf
	edActBufList
	edActHelp
	edActMenuBar
)

type edDialogKind int

const (
	edDlgNone edDialogKind = iota
	edDlgOpen
	edDlgOutline
	edDlgBuffers
	edDlgSaveAs
	edDlgFind
	edDlgReplace
	edDlgGoto
)

type edSnapshot struct {
	lines  []string
	cx, cy int
}

// edState is the whole editor. The document itself is held directly here
// rather than behind a buffer pointer, so the editing code never has to reach
// through anything; the other open buffers are parked in .buffers and swapped
// in and out around these fields. See cmd_edit_buffers.go.
type edState struct {
	// ── the document ──
	path  string
	lines []string
	hl    [][]edTokKind // one token kind per rune, per line

	// ── caret and viewport ──
	cx, cy     int // cursor: rune column, line index
	goalX      int // desired column preserved across vertical movement
	scrollX    int
	scrollY    int
	selX, selY int // selection anchor
	hasSel     bool
	selecting  bool // left button held down inside the text area

	// ── history ──
	undo   []edSnapshot
	redo   []edSnapshot
	typing bool // consecutive typed characters share one undo step

	// ── saved state ──
	dirty     bool
	savedMark edMark // fingerprint of the text as last loaded or saved

	// Work deferred to the next frame rather than repeated per keystroke: each
	// flag is set by touch() and cleared by the loop in runEdit.
	hlDirty    bool
	dirtyStale bool // re-compare the buffer against savedMark
	focusStale bool // re-derive the focused function's range
	widthStale bool // re-measure the widest line

	// ── view options ──
	showLineNums bool
	showSyntax   bool
	showHelp     bool
	indentWidth  int     // spaces per indent step, detected per document
	focusMode    bool    // show one function at a time
	focus        edFocus // the function on screen; inactive means the whole buffer

	// ── widest line, for the horizontal scrollbar; see maxLineLen ──
	widthCache  int
	widthTop    int
	widthBottom int

	// ── mouse, for spotting a double click ──
	clickAt float64
	clickX  int
	clickY  int

	// ── held-key repeat, on the editor's own clock; see repeatKey ──
	repeatID int32
	repeatAt float64

	// ── menu bar ──
	menuOpen  int // index into edMenus, -1 when closed
	menuHover int // hovered item within the open menu, -1 for none

	// ── modal dialogs ──
	dlg       edDialogKind
	dlgInput  string
	dlgList   []string // rows shown by the list dialogs
	dlgFuncs  []edFunc // outline targets, parallel to dlgList
	dlgActive int32
	dlgScroll int32
	dlgFresh  bool // opened this frame; see openDialog

	findTerm    string
	replaceTerm string
	dlgReplace  string // the replacement field while the dialog is open
	dlgField    int    // which of the two fields has the keyboard
	replaceFrom int    // line range Replace All is confined to, -1 for the view
	replaceTo   int

	confirm    edConfirm
	pendingAct edAction
	confirmed  bool // set while re-running an action the user confirmed

	// ── background work, all answered through channels the loop drains ──
	formatting bool // a format request is in flight
	fmtDone    chan edFmtResult
	building   bool // a build is in flight
	buildDone  chan error
	assist     edAssist
	assistDone chan edAssistResult

	// ── language server diagnostics ──
	diags    []edDiagnostic // problems published for this buffer
	diagSeq  int            // last publish counter seen
	diagPush float64        // when to hand the server the latest text, 0 for never

	// Where the game last died, kept apart from the server's findings so a
	// later publish cannot wipe it out; see cmd_edit_run.go.
	runErr     edDiagnostic
	runErrPath string
	runEvents  chan edRunEvent

	// ── the other open documents ──
	buffers  []edBuffer // buffers[bufIndex] mirrors the live one above
	bufIndex int

	toast    Toast
	wantQuit bool
}

// edMark fingerprints buffer contents; size guards the hash against collisions.
type edMark struct {
	hash uint64
	size int
}

// edConfirm is a modal yes/no prompt in the editor's own DOS palette. The
// shared ConfirmDialog in ui.go carries the dark gfx/map theme, which would
// look out of place on the blue desktop, but the API is the same.
type edConfirm struct {
	Active   bool
	fresh    bool // shown this frame; see openDialog for why that matters
	msg      string
	yesLabel string
	noLabel  string
}

func (d *edConfirm) Show(msg, yesLabel, noLabel string) {
	if d.Active {
		return
	}
	d.Active, d.fresh = true, true
	d.msg, d.yesLabel, d.noLabel = msg, yesLabel, noLabel
}

// Draw renders the prompt and reports true once, when the user confirms.
func (d *edConfirm) Draw() bool {
	if !d.Active {
		return false
	}
	if d.fresh {
		raygui.Lock()
		defer func() {
			raygui.Unlock()
			d.fresh = false
		}()
	}
	const w, h = int32(336), int32(96)
	x, y := edDialogFrame("Confirm", w, h)
	edDrawStr(x+(w-int32(len([]rune(d.msg)))*edCharW)/2, y+28, d.msg, edEgaBlack)

	by := float32(y+h) - float32(edBtnH()) - 6
	if raygui.Button(rl.NewRectangle(float32(x+w-166), by, 74, float32(edBtnH())), d.yesLabel) {
		d.Active = false
		return true
	}
	if raygui.Button(rl.NewRectangle(float32(x+w-84), by, 74, float32(edBtnH())), d.noLabel) ||
		rl.IsKeyPressed(rl.KeyEscape) {
		d.Active = false
	}
	return false
}

// ── menus ─────────────────────────────────────────────────────────────────────

type edMenuItem struct {
	label string // empty label = separator
	key   string // shortcut hint, right-aligned
	act   edAction
}

type edMenu struct {
	title string
	items []edMenuItem
}

var edMenus = []edMenu{
	{"File", []edMenuItem{
		{"New", "^N", edActNew},
		{"Open...", "F3", edActOpen},
		{"Save", "F2", edActSave},
		{"Save As...", "F12", edActSaveAs},
		{"", "", edActNone},
		{"Run", "F5", edActRun},
		{"", "", edActNone},
		{"Quit", "^Q", edActQuit},
	}},
	{"Edit", []edMenuItem{
		{"Undo", "^Z", edActUndo},
		{"Redo", "^Y", edActRedo},
		{"", "", edActNone},
		{"Cut", "^X", edActCut},
		{"Copy", "^C", edActCopy},
		{"Paste", "^V", edActPaste},
		{"", "", edActNone},
		{"Select All", "^A", edActSelectAll},
		{"Indent", "Tab", edActIndent},
		{"Unindent", "S-Tab", edActUnindent},
		{"Toggle Comment", "^/", edActToggleComment},
		{"Duplicate Line", "^D", edActDupLine},
		{"Move Line Up", "A-Up", edActMoveUp},
		{"Move Line Down", "A-Dn", edActMoveDown},
		{"", "", edActNone},
		{"Complete", "^Space", edActComplete},
		{"Inline Help", "^I", edActInlineHelp},
		{"Format", "F7", edActFormat},
	}},
	{"Search", []edMenuItem{
		{"Find...", "^F", edActFind},
		{"Replace...", "^H", edActReplace},
		{"Find Next", "F4", edActFindNext},
		{"Go to Line...", "^G", edActGoto},
		{"Next Problem", "^E", edActNextProblem},
		{"Outline...", "A-F2", edActOutline},
	}},
	{"View", []edMenuItem{
		{"Line Numbers", "", edActLineNums},
		{"Syntax Colors", "", edActSyntax},
		{"", "", edActNone},
		{"Function Focus", "F8", edActFocus},
	}},
	{"Project", []edMenuItem{
		{"Run", "F5", edActRun},
		{"", "", edActNone},
		{"Build", "F9", edActBuild},
	}},
	{"Tools", []edMenuItem{
		{"Sprite Editor", "", edActGfx},
		{"Map Editor", "", edActMap},
	}},
	{"Window", []edMenuItem{
		{"Next", "F6", edActNextBuf},
		{"Previous", "A-F6", edActPrevBuf},
		{"", "", edActNone},
		{"List...", "A-0", edActBufList},
		{"", "", edActNone},
		{"Close", "^W", edActCloseBuf},
	}},
	{"Options", []edMenuItem{
		{"Language Server", "", edActUseLSP},
		{"Format with stylua", "", edActForceStylua},
		{"Auto-close Brackets", "", edActAutoClose},
		{"Indent Width", "", edActIndentWidth},
		{"", "", edActNone},
		{"Font: unscii-8", "", edActFontUnscii},
		{"Font: IBM VGA", "", edActFontVGA},
		{"", "", edActNone},
		{"Larger Font", "A-+", edActFontBigger},
		{"Smaller Font", "A--", edActFontSmaller},
	}},
	{"Help", []edMenuItem{
		{"Keys", "F1", edActHelp},
	}},
}

func edMenuTitleRect(i int) rl.Rectangle {
	x := edCharW
	for k := 0; k < i; k++ {
		x += int32(len(edMenus[k].title)+2) * edCharW
	}
	w := int32(len(edMenus[i].title)+2) * edCharW
	return rl.NewRectangle(float32(x), 0, float32(w), float32(edMenuH))
}

func edMenuDropRect(i int) rl.Rectangle {
	t := edMenuTitleRect(i)
	cols := 0
	for _, it := range edMenus[i].items {
		if n := len(it.label) + len(it.key) + 6; n > cols {
			cols = n
		}
	}
	h := edPad * 2
	for _, it := range edMenus[i].items {
		if it.label == "" {
			h += 4
		} else {
			h += edLineH
		}
	}
	return rl.NewRectangle(t.X, float32(edMenuH), float32(int32(cols)*edCharW), float32(h))
}

// edMenuItemAt returns the index of the item under p, or -1.
func edMenuItemAt(menu int, p rl.Vector2) int {
	r := edMenuDropRect(menu)
	if !rl.CheckCollisionPointRec(p, r) {
		return -1
	}
	y := r.Y + float32(edPad)
	for i, it := range edMenus[menu].items {
		h := float32(edLineH)
		if it.label == "" {
			h = 4
		}
		if p.Y >= y && p.Y < y+h {
			if it.label == "" {
				return -1
			}
			return i
		}
		y += h
	}
	return -1
}

// edMenuStep moves the highlight within the open menu, skipping separators.
func edMenuStep(s *edState, dir int) {
	items := edMenus[s.menuOpen].items
	i := s.menuHover
	for range items {
		i += dir
		if i < 0 {
			i = len(items) - 1
		}
		if i >= len(items) {
			i = 0
		}
		if items[i].label != "" {
			s.menuHover = i
			return
		}
	}
}

// ── text model ────────────────────────────────────────────────────────────────

// viewTop, viewBottom and viewLen bound what is on screen. Without function
// focus that is the whole buffer; with it, one function.
func (s *edState) viewTop() int {
	if s.focus.active {
		return max(0, min(s.focus.from, len(s.lines)-1))
	}
	return 0
}

func (s *edState) viewBottom() int {
	if s.focus.active {
		return max(s.viewTop(), min(s.focus.to, len(s.lines)-1))
	}
	return len(s.lines) - 1
}

func (s *edState) viewLen() int { return s.viewBottom() - s.viewTop() + 1 }

func (s *edState) runes(i int) []rune { return []rune(s.lines[i]) }

// lineLen counts runes without building a slice of them: it is called for every
// visible line every frame, and by maxLineLen for every line in the view.
func (s *edState) lineLen(i int) int { return utf8.RuneCountInString(s.lines[i]) }

// touch marks the buffer modified and the highlight cache stale. Editing can
// also move a focused function's boundaries, and can put the text back the way
// it was on disk, so both the range and the dirty flag are re-derived on the
// next frame rather than on every keystroke.
func (s *edState) touch() {
	s.dirty = true
	s.hlDirty = true
	s.focusStale = true
	s.dirtyStale = true
	s.widthStale = true
	if edHaveLuaLSP() {
		// Re-check once the typing settles rather than on every keystroke.
		s.diagPush = float64(rl.GetTime()) + edDiagDelay
	}
}

// contentMark fingerprints the buffer. Comparing it against the mark taken at
// the last load or save is what lets the dirty flag clear again when an edit is
// undone by hand - a plain "has been edited" bool never can.
func (s *edState) contentMark() edMark {
	h := fnv.New64a()
	size := 0
	for i, line := range s.lines {
		if i > 0 {
			_, _ = h.Write([]byte{'\n'})
			size++
		}
		_, _ = io.WriteString(h, line)
		size += len(line)
	}
	return edMark{hash: h.Sum64(), size: size}
}

// markSaved records the buffer as matching what is on disk.
func (s *edState) markSaved() {
	s.savedMark = s.contentMark()
	s.dirty = false
	s.dirtyStale = false
}

// recheckDirty compares the buffer against the last saved state.
func (s *edState) recheckDirty() {
	s.dirtyStale = false
	s.dirty = s.contentMark() != s.savedMark
}

// refocus re-derives the visible range from the caret's position. Called when
// the caret may have landed in another function, or the buffer changed shape.
func (s *edState) refocus() {
	s.focusStale = false

	before := s.focus
	if s.focusMode {
		s.focus = edComputeFocus(s, s.cy)
	} else {
		s.focus = edFocusWhole()
	}
	s.clampCursor()

	// scrollY is measured from the top of the view, so when the view itself
	// moves - focus turned on or off, or a jump to another function - the old
	// offset means something else entirely and the caret would be left off
	// screen. Re-centre on it. Editing inside the focused function only moves
	// its far end, leaving the origin alone, so typing does not keep jumping.
	if s.focus.active != before.active || s.viewTop() != before.viewTopIn(s) {
		s.centerOnCursor()
		return
	}
	s.ensureVisible()
}

func (s *edState) clampCursor() {
	if len(s.lines) == 0 {
		s.lines = []string{""}
	}
	s.cy = max(s.viewTop(), min(s.cy, s.viewBottom()))
	n := s.lineLen(s.cy)
	if s.cx < 0 {
		s.cx = 0
	}
	if s.cx > n {
		s.cx = n
	}
}

// selRange returns the selection normalised so (ax,ay) precedes (bx,by).
func (s *edState) selRange() (ax, ay, bx, by int) {
	ax, ay, bx, by = s.selX, s.selY, s.cx, s.cy
	if ay > by || (ay == by && ax > bx) {
		ax, ay, bx, by = bx, by, ax, ay
	}
	return
}

func (s *edState) selText() string {
	if !s.hasSel {
		return ""
	}
	ax, ay, bx, by := s.selRange()
	if ay == by {
		r := s.runes(ay)
		return string(r[ax:bx])
	}
	parts := []string{string(s.runes(ay)[ax:])}
	for i := ay + 1; i < by; i++ {
		parts = append(parts, s.lines[i])
	}
	parts = append(parts, string(s.runes(by)[:bx]))
	return strings.Join(parts, "\n")
}

func (s *edState) deleteSelection() bool {
	if !s.hasSel {
		return false
	}
	ax, ay, bx, by := s.selRange()
	if ay == by {
		r := s.runes(ay)
		s.lines[ay] = string(r[:ax]) + string(r[bx:])
	} else {
		merged := string(s.runes(ay)[:ax]) + string(s.runes(by)[bx:])
		tail := append([]string{merged}, s.lines[by+1:]...)
		s.lines = append(s.lines[:ay], tail...)
	}
	s.cy, s.cx = ay, ax
	s.hasSel = false
	s.touch()
	return true
}

// insert splices text (which may contain newlines) at the cursor, replacing the
// selection if there is one.
func (s *edState) insert(text string) {
	if !s.mayEditHere() {
		return
	}
	s.deleteSelection()
	cur := s.runes(s.cy)
	head, tail := string(cur[:s.cx]), string(cur[s.cx:])
	parts := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")

	if len(parts) == 1 {
		s.lines[s.cy] = head + parts[0] + tail
		s.cx += len([]rune(parts[0]))
	} else {
		out := make([]string, 0, len(s.lines)+len(parts)-1)
		out = append(out, s.lines[:s.cy]...)
		out = append(out, head+parts[0])
		out = append(out, parts[1:len(parts)-1]...)
		last := parts[len(parts)-1]
		out = append(out, last+tail)
		out = append(out, s.lines[s.cy+1:]...)
		s.lines = out
		s.cy += len(parts) - 1
		s.cx = len([]rune(last))
	}
	s.touch()
	s.goalX = s.cx
}

// newline splits the current line, carrying the current indentation over.
func (s *edState) newline() {
	if s.focus.sealed(s.cy) || !s.mayEditHere() {
		if s.focus.active {
			s.refuseEdit()
		}
		return
	}
	s.deleteSelection()
	cur := s.runes(s.cy)
	head, tail := string(cur[:s.cx]), string(cur[s.cx:])
	indent := edLeadingWS(head)
	s.lines[s.cy] = head
	rest := append([]string{indent + tail}, s.lines[s.cy+1:]...)
	s.lines = append(s.lines[:s.cy+1], rest...)
	s.cy++
	s.cx = len([]rune(indent))
	s.goalX = s.cx
	s.touch()
}

func edLeadingWS(s string) string {
	for i, r := range s {
		if r != ' ' && r != '\t' {
			return s[:i]
		}
	}
	return s
}

func (s *edState) backspace() {
	if s.hasSel {
		if !s.mayEditHere() {
			return
		}
		s.deleteSelection()
		return
	}
	if s.cx == 0 {
		if s.cy == 0 || !s.mayJoin(s.cy-1, s.cy) {
			return
		}
	} else if !s.focus.editableAt(s.cy, s.cx-1) {
		s.refuseEdit()
		return
	}
	if edDeletePair(s) {
		// The caret sits between "()" or "" that were written as a pair; take
		// both, so undoing the auto-close is one keystroke.
		r := s.runes(s.cy)
		s.lines[s.cy] = string(r[:s.cx-1]) + string(r[s.cx+1:])
		s.cx--
		s.goalX = s.cx
		s.touch()
		return
	}
	switch {
	case s.cx > 0:
		r := s.runes(s.cy)
		// Backspace over a full indent step when only whitespace precedes.
		n := 1
		if strings.TrimSpace(string(r[:s.cx])) == "" && s.cx%s.indentWidth == 0 {
			n = s.indentWidth
		}
		if n > s.cx {
			n = s.cx
		}
		s.lines[s.cy] = string(r[:s.cx-n]) + string(r[s.cx:])
		s.cx -= n
	case s.cy > 0:
		prev := s.lineLen(s.cy - 1)
		s.lines[s.cy-1] += s.lines[s.cy]
		s.lines = append(s.lines[:s.cy], s.lines[s.cy+1:]...)
		s.cy--
		s.cx = prev
	default:
		return
	}
	s.goalX = s.cx
	s.touch()
}

func (s *edState) deleteForward() {
	if s.hasSel {
		if !s.mayEditHere() {
			return
		}
		s.deleteSelection()
		return
	}
	if s.cx >= s.lineLen(s.cy) {
		if s.cy >= len(s.lines)-1 || !s.mayJoin(s.cy, s.cy+1) {
			return
		}
	} else if !s.focus.editableAt(s.cy, s.cx) {
		s.refuseEdit()
		return
	}
	r := s.runes(s.cy)
	switch {
	case s.cx < len(r):
		s.lines[s.cy] = string(r[:s.cx]) + string(r[s.cx+1:])
	case s.cy < len(s.lines)-1:
		s.lines[s.cy] += s.lines[s.cy+1]
		s.lines = append(s.lines[:s.cy+1], s.lines[s.cy+2:]...)
	default:
		return
	}
	s.touch()
}

// indentSelection shifts every line of the selection (or the current line) by
// one tab stop; dir -1 removes indentation.
func (s *edState) indentSelection(dir int) {
	from, to := s.cy, s.cy
	if s.hasSel {
		_, ay, _, by := s.selRange()
		from, to = ay, by
	}
	pad := s.indentPad()
	for i := from; i <= to; i++ {
		if s.focus.sealed(i) {
			continue
		}
		if dir > 0 {
			if s.lines[i] != "" {
				s.lines[i] = pad + s.lines[i]
			}
			continue
		}
		trimmed := strings.TrimLeft(s.lines[i], " ")
		if n := len(s.lines[i]) - len(trimmed); n > 0 {
			cut := s.indentWidth
			if n < cut {
				cut = n
			}
			s.lines[i] = s.lines[i][cut:]
		}
	}
	s.clampCursor()
	if s.hasSel {
		s.selY = min(max(s.selY, 0), len(s.lines)-1)
		s.selX = min(s.selX, s.lineLen(s.selY))
	}
	s.touch()
}

// mayEditHere reports whether the caret, and the selection if there is one, sit
// in territory the current view allows changing. It complains when they do not.
func (s *edState) mayEditHere() bool {
	if !s.focus.active {
		return true
	}
	if s.hasSel {
		ax, ay, bx, by := s.selRange()
		for i := ay; i <= by; i++ {
			from, to := 0, s.lineLen(i)
			if i == ay {
				from = ax
			}
			if i == by {
				to = bx
			}
			if !s.focus.editableSpan(i, from, to) {
				return s.refuseEdit()
			}
		}
		return true
	}
	if !s.focus.editableAt(s.cy, s.cx) {
		return s.refuseEdit()
	}
	return true
}

// mayJoin reports whether two adjacent lines may be merged, which backspace at
// column 0 and delete at end of line both do.
func (s *edState) mayJoin(upper, lower int) bool {
	if !s.focus.active {
		return true
	}
	if upper < s.viewTop() || lower > s.viewBottom() ||
		s.focus.sealed(upper) || s.focus.sealed(lower) {
		return s.refuseEdit()
	}
	return true
}

func (s *edState) refuseEdit() bool {
	s.toast.Notify("Function focus: only the body and the parameters can change")
	return false
}

// toggleComment comments or uncomments the selected lines, or the current line
// when there is no selection. The whole range is treated as one block: if every
// non-blank line in it is already commented the markers come off, otherwise a
// marker goes on at the shallowest indentation in the range, so the operation
// round-trips on a mixed selection.
func (s *edState) toggleComment() {
	from, to := s.cy, s.cy
	if s.hasSel {
		_, ay, bx, by := s.selRange()
		from, to = ay, by
		if by > ay && bx == 0 {
			to = by - 1 // a selection resting at column 0 does not reach that line
		}
	}

	// Survey the range first: is it all commented, and where is the shallowest
	// indentation to line the markers up on?
	allCommented, blank := true, true
	indent := -1
	for i := from; i <= to; i++ {
		body := strings.TrimLeft(s.lines[i], " \t")
		if body == "" || s.focus.sealed(i) {
			continue
		}
		blank = false
		if col := len([]rune(s.lines[i])) - len([]rune(body)); indent < 0 || col < indent {
			indent = col
		}
		if !strings.HasPrefix(body, "--") {
			allCommented = false
		}
	}
	if blank {
		return
	}

	s.beginEdit(false)

	// Each line records where text was spliced and by how much, so positions
	// before the splice point stay put.
	type splice struct{ at, delta int }
	splices := make([]splice, to-from+1)

	for i := from; i <= to; i++ {
		r := []rune(s.lines[i])
		body := strings.TrimLeft(string(r), " \t")
		if body == "" || s.focus.sealed(i) {
			continue
		}
		col := len(r) - len([]rune(body))

		if allCommented {
			rest := strings.TrimPrefix(body, "--")
			rest = strings.TrimPrefix(rest, " ") // the space this tool adds
			s.lines[i] = string(r[:col]) + rest
			splices[i-from] = splice{col, len([]rune(rest)) - len([]rune(body))}
		} else {
			s.lines[i] = string(r[:indent]) + edCommentMark + string(r[indent:])
			splices[i-from] = splice{indent, len([]rune(edCommentMark))}
		}
	}

	// Keep the caret and the selection anchor over the same text.
	shift := func(x, y int) int {
		if y < from || y > to {
			return x
		}
		sp := splices[y-from]
		switch {
		case sp.delta == 0 || x <= sp.at:
			// Positions at the splice point stay put, so a selection anchored
			// at the start of a line still covers the marker afterwards.
			return x
		case sp.delta < 0 && x < sp.at-sp.delta:
			return sp.at // the caret sat inside the marker that just went away
		}
		return max(0, min(x+sp.delta, s.lineLen(y)))
	}
	s.cx = shift(s.cx, s.cy)
	if s.hasSel {
		s.selX = shift(s.selX, s.selY)
	}
	s.goalX = s.cx
	s.clampCursor()
	s.touch()
}

// ── undo ──────────────────────────────────────────────────────────────────────

func (s *edState) snapshot() edSnapshot {
	cp := make([]string, len(s.lines))
	copy(cp, s.lines)
	return edSnapshot{lines: cp, cx: s.cx, cy: s.cy}
}

// beginEdit records an undo step. Set coalesce for plain typing so a run of
// characters collapses into a single undo.
func (s *edState) beginEdit(coalesce bool) {
	if coalesce && s.typing {
		return
	}
	s.undo = append(s.undo, s.snapshot())
	if len(s.undo) > edMaxUndo {
		s.undo = s.undo[1:]
	}
	s.redo = nil
	s.typing = coalesce
}

func (s *edState) restore(from, to *[]edSnapshot) bool {
	if len(*from) == 0 {
		return false
	}
	*to = append(*to, s.snapshot())
	snap := (*from)[len(*from)-1]
	*from = (*from)[:len(*from)-1]
	s.lines = snap.lines
	s.cx, s.cy = snap.cx, snap.cy
	s.hasSel = false
	s.typing = false
	s.hlDirty = true
	s.dirtyStale = true
	s.widthStale = true
	s.focus = edFocusWhole()
	s.refocus()
	s.clampCursor()
	s.ensureVisible()
	return true
}

// ── highlighting cache ────────────────────────────────────────────────────────

func (s *edState) rehighlight() {
	if !s.hlDirty && len(s.hl) == len(s.lines) {
		return
	}
	s.hl = make([][]edTokKind, len(s.lines))
	var ctx edLongCtx
	for i, ln := range s.lines {
		s.hl[i], ctx = edHighlightLine([]rune(ln), ctx)
	}
	s.hlDirty = false
}

// ── viewport ──────────────────────────────────────────────────────────────────

func (s *edState) ensureVisible() {
	cols := edCols(s)
	row := s.cy - s.viewTop() // the caret's row within the view
	if row < s.scrollY {
		s.scrollY = row
	}
	if row >= s.scrollY+edRows {
		s.scrollY = row - edRows + 1
	}
	if s.cx < s.scrollX {
		s.scrollX = s.cx
	}
	if s.cx >= s.scrollX+cols {
		s.scrollX = s.cx - cols + 1
	}
	s.scrollY = max(0, min(s.scrollY, max(0, s.viewLen()-edRows)))
	s.scrollX = max(0, s.scrollX)
}

// maxLineLen is what the horizontal scrollbar is sized against. The scan is
// over every line in the view, so the answer is kept until the text or the
// view range actually changes.
func (s *edState) maxLineLen() int {
	top, bottom := s.viewTop(), s.viewBottom()
	if !s.widthStale && s.widthTop == top && s.widthBottom == bottom {
		return s.widthCache
	}
	n := 0
	for i := top; i <= bottom; i++ {
		if l := s.lineLen(i); l > n {
			n = l
		}
	}
	s.widthCache, s.widthTop, s.widthBottom, s.widthStale = n, top, bottom, false
	return n
}

// ── file I/O ──────────────────────────────────────────────────────────────────

// edSetTitle puts the open file's name in the window title. The guard keeps the
// file helpers callable before InitWindow, where GLFW would abort.
func edSetTitle(path string) {
	if rl.IsWindowReady() {
		rl.SetWindowTitle("fz edit — " + filepath.Base(path))
	}
}

// setDocument replaces the live buffer's contents and resets its view state.
func (s *edState) setDocument(path string, lines []string) {
	s.path, s.lines = path, lines
	s.cx, s.cy, s.goalX = 0, 0, 0
	s.scrollX, s.scrollY = 0, 0
	s.hasSel = false
	s.hlDirty = true
	s.typing = false
	s.undo, s.redo = nil, nil
	s.indentWidth = edDetectIndent(lines)
	s.widthStale = true
	s.markSaved()
	s.assist.dismiss()
	s.focus = edFocusWhole()
	s.refocus()
	text := strings.Join(lines, "\n")
	edRequestSymbols(path, text)
	edWarmLSP(path, text)
	edSetTitle(path)
}

func edLoadFile(s *edState, path string) error {
	lines, err := edReadFile(path)
	if err != nil {
		return err
	}
	s.setDocument(path, lines)
	return nil
}

func edSaveFile(s *edState, path string) error {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(path, []byte(strings.Join(s.lines, "\n")), 0o644); err != nil {
		return err
	}
	s.path = path
	s.typing = false
	s.markSaved()
	edSetTitle(path)
	return nil
}

// edListLuaFiles collects the project's .lua files for the Open dialog.
func edListLuaFiles() []string {
	var out []string
	_ = filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "dist", "build", "node_modules", ".vscode":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(p), ".lua") {
			out = append(out, filepath.ToSlash(p))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// edResolvePath maps the command-line argument to a file to open. A bare name
// gains a .lua extension and falls back to lib/ before being treated as new.
func edResolvePath(name string) string {
	if name == "" {
		return "main.lua"
	}
	if filepath.Ext(name) == "" {
		name += ".lua"
	}
	if _, err := os.Stat(name); err == nil {
		return name
	}
	if filepath.Dir(name) == "." {
		if alt := filepath.Join("lib", name); fileExists(alt) {
			return alt
		}
	}
	return name
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// ── entry point ───────────────────────────────────────────────────────────────

func runEdit(args []string) error {
	var name string
	if len(args) > 0 {
		name = args[0]
	}
	path := edResolvePath(name)

	rl.SetConfigFlags(rl.FlagWindowResizable | rl.FlagWindowHighdpi)
	rl.InitWindow(virtualW*2, virtualH*2, "fz edit — "+filepath.Base(path))
	fixRetinaStartupScale()
	rl.SetTargetFPS(60)
	defer rl.CloseWindow()

	edInitFont()
	defer edUnloadFont()

	s := &edState{
		path:         path,
		lines:        []string{""},
		fmtDone:      make(chan edFmtResult, 1),
		buildDone:    make(chan error, 1),
		runEvents:    make(chan edRunEvent, 8),
		assistDone:   make(chan edAssistResult, 4),
		showLineNums: true,
		showSyntax:   true,
		indentWidth:  edDefaultIndent,
		menuOpen:     -1,
		menuHover:    -1,
		hlDirty:      true,
	}
	if err := edLoadFile(s, path); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		s.toast.Notify("New file: " + path)
	}
	s.buffers = []edBuffer{s.captureBuffer()}

	defer edStopLSP()

	canvas := rl.LoadRenderTexture(virtualW, virtualH)
	defer rl.UnloadRenderTexture(canvas)

	rl.SetExitKey(0)
	running := true
	osClosed := false

	for running {
		if rl.WindowShouldClose() && !osClosed {
			osClosed = true
			if s.anyDirty() {
				s.pendingAct = edActQuit
				s.confirm.Show("Unsaved changes - quit anyway?", "Quit", "Cancel")
			} else {
				running = false
			}
		}

		scale, offsetX, offsetY := virtualScale()
		// BeginDrawing() with FLAG_WINDOW_HIGHDPI draws in screen (logical)
		// coords, but virtualScale() uses render (physical) coords.
		dpi := float32(rl.GetRenderWidth()) / float32(rl.GetScreenWidth())
		ss := scale / dpi
		sox := offsetX / dpi
		soy := offsetY / dpi

		rl.SetMouseOffset(int(-sox), int(-soy))
		rl.SetMouseScale(1/ss, 1/ss)

		select {
		case res := <-s.fmtDone:
			edApplyFormat(s, res)
		case res := <-s.assistDone:
			edApplyAssist(s, res)
		case ev := <-s.runEvents:
			edApplyRunEvent(s, ev)
		case err := <-s.buildDone:
			s.building = false
			if err != nil {
				s.toast.Notify("Build failed: " + edFirstLine(err.Error()))
			} else {
				s.toast.Notify("Build complete - see dist/")
			}
		default:
		}

		edHandleInput(s)
		s.rehighlight()
		if s.focusStale {
			s.refocus()
		}
		if s.dirtyStale {
			s.recheckDirty()
		}
		edSyncDiagnostics(s)

		rl.BeginTextureMode(canvas)
		edDrawScene(s)
		rl.EndTextureMode()

		if s.wantQuit {
			running = false
		}

		rl.SetMouseOffset(0, 0)
		rl.SetMouseScale(1, 1)

		rl.BeginDrawing()
		rl.ClearBackground(rl.Black)
		src := rl.NewRectangle(0, 0, float32(virtualW), -float32(virtualH))
		dst := rl.NewRectangle(sox, soy, float32(virtualW)*ss, float32(virtualH)*ss)
		rl.DrawTexturePro(canvas.Texture, src, dst, rl.NewVector2(0, 0), 0, rl.White)
		rl.EndDrawing()
	}

	return nil
}

// edApplyGuiStyle repaints raygui in EGA colours so the dialogs match the
// DOS-era chrome around them.
func edApplyGuiStyle() {
	set := func(ctrl raygui.ControlID, prop raygui.PropertyID, c rl.Color) {
		raygui.SetStyle(ctrl, prop, raygui.NewColorPropertyValue(c))
	}
	set(raygui.DEFAULT, raygui.BORDER_COLOR_NORMAL, edEgaBlack)
	set(raygui.DEFAULT, raygui.BASE_COLOR_NORMAL, edEgaLightGray)
	set(raygui.DEFAULT, raygui.TEXT_COLOR_NORMAL, edEgaBlack)
	set(raygui.DEFAULT, raygui.BORDER_COLOR_FOCUSED, edEgaBlack)
	set(raygui.DEFAULT, raygui.BASE_COLOR_FOCUSED, edEgaWhite)
	set(raygui.DEFAULT, raygui.TEXT_COLOR_FOCUSED, edEgaBlack)
	set(raygui.DEFAULT, raygui.BORDER_COLOR_PRESSED, edEgaBlack)
	set(raygui.DEFAULT, raygui.BASE_COLOR_PRESSED, edEgaBlue)
	set(raygui.DEFAULT, raygui.TEXT_COLOR_PRESSED, edEgaWhite)
	set(raygui.DEFAULT, raygui.BORDER_COLOR_DISABLED, edEgaDarkGray)
	set(raygui.DEFAULT, raygui.BASE_COLOR_DISABLED, edEgaLightGray)
	set(raygui.DEFAULT, raygui.TEXT_COLOR_DISABLED, edEgaDarkGray)
	set(raygui.DEFAULT, raygui.LINE_COLOR, edEgaDarkGray)
	set(raygui.DEFAULT, raygui.BACKGROUND_COLOR, edEgaBlue)
	set(raygui.TEXTBOX, raygui.BASE_COLOR_NORMAL, edEgaBlue)
	set(raygui.TEXTBOX, raygui.TEXT_COLOR_NORMAL, edEgaYellow)
	set(raygui.TEXTBOX, raygui.BASE_COLOR_FOCUSED, edEgaBlue)
	set(raygui.TEXTBOX, raygui.TEXT_COLOR_FOCUSED, edEgaWhite)
	set(raygui.TEXTBOX, raygui.BASE_COLOR_PRESSED, edEgaBlue)
	set(raygui.TEXTBOX, raygui.TEXT_COLOR_PRESSED, edEgaWhite)
	// The scrollbar trough comes from BORDER_COLOR_DISABLED, its thumb from the
	// slider border colours and its arrows from the scrollbar text colours.
	set(raygui.SCROLLBAR, raygui.TEXT_COLOR_NORMAL, edEgaWhite)
	set(raygui.SCROLLBAR, raygui.TEXT_COLOR_FOCUSED, edEgaWhite)
	set(raygui.SCROLLBAR, raygui.TEXT_COLOR_PRESSED, edEgaWhite)
	set(raygui.SLIDER, raygui.BORDER_COLOR_NORMAL, edEgaBlue)
	set(raygui.SLIDER, raygui.BORDER_COLOR_FOCUSED, edEgaCyan)
	set(raygui.SLIDER, raygui.BORDER_COLOR_PRESSED, edEgaCyan)

	raygui.SetFont(edFont)
	raygui.SetStyle(raygui.DEFAULT, raygui.TEXT_SIZE, raygui.PropertyValue(edFontSize))
	raygui.SetStyle(raygui.DEFAULT, raygui.BORDER_WIDTH, raygui.PropertyValue(1))
	// unscii's glyph advance already fills the 8px cell, so no extra tracking:
	// that keeps raygui's text measurement on the same grid as the editor's,
	// and stops it from truncating list rows that actually fit.
	spacing := raygui.PropertyValue(1)
	if edFontLoaded {
		spacing = 0
	}
	raygui.SetStyle(raygui.DEFAULT, raygui.TEXT_SPACING, spacing)
	raygui.SetStyle(raygui.SCROLLBAR, raygui.ARROWS_VISIBLE, raygui.PropertyValue(1))
}

// ── actions ───────────────────────────────────────────────────────────────────

// edGuardDirty defers act behind a confirmation when dirty is set. It returns
// true when the action may proceed - either because nothing would be lost, or
// because this is the re-run after the user confirmed.
func edGuardDirty(s *edState, act edAction, dirty bool, msg string) bool {
	if s.confirmed || !dirty {
		return true
	}
	s.pendingAct = act
	s.confirm.Show(msg, "Discard", "Cancel")
	return false
}

func edApply(s *edState, act edAction) {
	switch act {
	case edActNew:
		edOpenBuffer(s, edUntitledName(s), []string{""})
		s.toast.Notify("New buffer: " + s.path)

	case edActOpen:
		edShowOpenDialog(s)

	case edActSave:
		if err := edSaveFile(s, s.path); err != nil {
			s.toast.Notify("Save failed: " + err.Error())
		} else {
			s.toast.Notify("Saved " + s.path)
		}

	case edActSaveAs:
		s.dlgInput = filepath.ToSlash(s.path)
		s.openDialog(edDlgSaveAs)

	case edActRun:
		edLaunchGame(s)

	case edActQuit:
		if !edGuardDirty(s, act, s.anyDirty(), "Unsaved changes - quit anyway?") {
			return
		}
		s.wantQuit = true

	case edActNextBuf:
		edCycleBuffer(s, 1)

	case edActPrevBuf:
		edCycleBuffer(s, -1)

	case edActCloseBuf:
		if !edGuardDirty(s, act, s.dirty, "Buffer not saved - close anyway?") {
			return
		}
		edCloseBuffer(s)

	case edActBufList:
		edShowBufferDialog(s)

	case edActUndo:
		if !s.restore(&s.undo, &s.redo) {
			s.toast.Notify("Nothing to undo")
		}

	case edActRedo:
		if !s.restore(&s.redo, &s.undo) {
			s.toast.Notify("Nothing to redo")
		}

	case edActCut:
		if s.hasSel {
			rl.SetClipboardText(s.selText())
			s.beginEdit(false)
			s.deleteSelection()
			s.ensureVisible()
		}

	case edActCopy:
		if s.hasSel {
			rl.SetClipboardText(s.selText())
			s.toast.Notify("Copied")
		}

	case edActPaste:
		if txt := rl.GetClipboardText(); txt != "" {
			s.beginEdit(false)
			s.insert(txt)
			s.ensureVisible()
		}

	case edActSelectAll:
		s.selX, s.selY = 0, s.viewTop()
		s.cy = s.viewBottom()
		s.cx = s.lineLen(s.cy)
		s.hasSel = true
		s.ensureVisible()

	case edActIndent:
		s.beginEdit(false)
		s.indentSelection(1)

	case edActUnindent:
		s.beginEdit(false)
		s.indentSelection(-1)

	case edActToggleComment:
		s.toggleComment()
		s.ensureVisible()

	case edActDupLine:
		s.duplicateLines()

	case edActMoveUp:
		s.moveLines(-1)

	case edActMoveDown:
		s.moveLines(1)

	case edActIndentWidth:
		s.indentWidth = edIndentSteps[(edIndexOf(edIndentSteps, s.indentWidth)+1)%len(edIndentSteps)]
		s.toast.Notify(fmt.Sprintf("Indent: %d spaces", s.indentWidth))

	case edActAutoClose:
		edAutoClose = !edAutoClose
		if edAutoClose {
			s.toast.Notify("Auto-closing brackets and quotes")
		} else {
			s.toast.Notify("Auto-close off")
		}

	case edActBuild:
		edStartBuild(s)

	case edActGfx:
		edSpawnTool(s, "sprite editor", "gfx")

	case edActMap:
		edSpawnTool(s, "map editor", "map")

	case edActFind:
		s.dlgInput = s.findTerm
		s.openDialog(edDlgFind)

	case edActReplace:
		edShowReplaceDialog(s)

	case edActFindNext:
		edFind(s, true)

	case edActGoto:
		s.dlgInput = strconv.Itoa(s.cy + 1)
		s.openDialog(edDlgGoto)

	case edActOutline:
		edShowOutlineDialog(s)

	case edActLineNums:
		s.showLineNums = !s.showLineNums

	case edActSyntax:
		s.showSyntax = !s.showSyntax

	case edActFocus:
		edToggleFocus(s)

	case edActUseLSP:
		edToggleLSP(s)

	case edActForceStylua:
		edToggleForceStylua(s)

	case edActNextProblem:
		edNextProblem(s)

	case edActFontUnscii:
		edSwitchFont(s, edFontUnscii)

	case edActFontVGA:
		edSwitchFont(s, edFontVGA)

	case edActFontBigger:
		edApplyFontZoom(s, 1)

	case edActFontSmaller:
		edApplyFontZoom(s, -1)

	case edActComplete:
		edStartComplete(s)

	case edActInlineHelp:
		edStartHover(s)

	case edActFormat:
		edStartFormat(s)

	case edActHelp:
		s.showHelp = true

	case edActMenuBar:
		s.menuOpen, s.menuHover = 0, 0
	}
}

// ── actions that need more than a line ───────────────────────────────────────

// edShowOpenDialog lists the project's Lua files, starting on the current one.
func edShowOpenDialog(s *edState) {
	s.dlgList = edListLuaFiles()
	s.dlgActive, s.dlgScroll = 0, 0
	for i, f := range s.dlgList {
		if f == filepath.ToSlash(s.path) {
			s.dlgActive = int32(i)
		}
	}
	edListFollow(s)
	s.openDialog(edDlgOpen)
}

// edShowBufferDialog lists the open buffers, starting on the current one.
func edShowBufferDialog(s *edState) {
	s.syncBuffer()
	s.dlgList = make([]string, 0, len(s.buffers))
	for i, b := range s.buffers {
		s.dlgList = append(s.dlgList, edBufferRow(i, b))
	}
	s.dlgActive, s.dlgScroll = int32(s.bufIndex), 0
	edListFollow(s)
	s.openDialog(edDlgBuffers)
}

// edShowOutlineDialog lists the file's functions, starting on the one the caret
// is in. A top-of-file entry always leads, so the picker is never empty and
// there is always a way back to the start of the document.
func edShowOutlineDialog(s *edState) {
	s.rehighlight() // the outline skips commented-out code, so tokens must be fresh
	s.dlgFuncs = append([]edFunc{{name: edTopOfFile}}, edParseFunctions(s.lines, s.hl)...)
	s.dlgList = make([]string, 0, len(s.dlgFuncs))
	for _, fn := range s.dlgFuncs {
		s.dlgList = append(s.dlgList, edOutlineRow(fn))
	}

	// Of the declarations starting at or before the caret, the latest one.
	s.dlgActive, s.dlgScroll = 0, 0
	best := -1
	for i, fn := range s.dlgFuncs {
		if fn.line <= s.cy && (best < 0 || fn.line > s.dlgFuncs[best].line) {
			best = i
		}
	}
	if best >= 0 {
		s.dlgActive = int32(best)
	}
	edListFollow(s)
	s.openDialog(edDlgOutline)
}

// edToggleFocus turns function focus on or off and says what happened, since
// the answer depends on whether the caret was inside a function at the time.
func edToggleFocus(s *edState) {
	s.focusMode = !s.focusMode
	s.refocus()

	if !s.focusMode {
		s.toast.Notify("Function focus off - whole file")
		return
	}
	edRequestSymbols(s.path, strings.Join(s.lines, "\n"))
	if s.focus.active {
		s.toast.Notify("Function focus: " + s.focus.name)
	} else {
		s.toast.Notify("Function focus on - put the caret in a function, or use Alt+F2")
	}
}

// edToggleLSP turns language server support on or off, dropping what it had
// told us when it goes.
func edToggleLSP(s *edState) {
	edSetUseLSP(!edUseLSP)
	if edUseLSP {
		edWarmLSP(s.path, strings.Join(s.lines, "\n"))
		s.toast.Notify("Language server on")
		return
	}
	s.diags, s.assist = nil, edAssist{}
	s.toast.Notify("Language server off - completion falls back to the buffer")
}

// edToggleForceStylua flips the override that keeps formatting away from the
// language server.
func edToggleForceStylua(s *edState) {
	if edStyluaPath == "" {
		s.toast.Notify("stylua is not installed")
		return
	}
	edForceStylua = !edForceStylua
	s.toast.Notify("Formatting with " + edPickFormatter().name)
}

// edSwitchFont changes the typeface, keeping each face's own default size.
func edSwitchFont(s *edState, kind edFontKind) {
	if edFontKindID == kind {
		return
	}
	if !edSetFont(kind, edFontDefs[kind].def) {
		s.toast.Notify("Could not load that font")
		return
	}
	s.ensureVisible() // the row count changed with the cell size
	s.toast.Notify("Font: " + edFontDefs[kind].label)
}

// edStartFormat hands the buffer to whichever formatter is currently chosen, on
// a goroutine; the result is picked up by the render loop so a slow tool never
// stalls a frame.
func edStartFormat(s *edState) {
	if s.formatting {
		return
	}
	if !strings.EqualFold(filepath.Ext(s.path), ".lua") {
		s.toast.Notify("Not a Lua file")
		return
	}
	formatter := edPickFormatter()
	if formatter.kind == edFmtNone {
		s.toast.Notify("No formatter found - install stylua or lua-language-server")
		return
	}

	src := strings.Join(s.lines, "\n")
	path, done := s.path, s.fmtDone
	s.formatting = true
	s.toast.Notify("Formatting with " + formatter.name + "...")

	go func() {
		text, err := formatter.format(path, src)
		done <- edFmtResult{text: text, name: formatter.name, err: err}
	}()
}

// edApplyFormat swaps in formatted source as one undoable edit, holding the
// cursor on the same line and column as far as they still exist.
func edApplyFormat(s *edState, res edFmtResult) {
	s.formatting = false
	if res.err != nil {
		s.toast.Notify("Format failed: " + res.err.Error())
		return
	}

	text := strings.ReplaceAll(res.text, "\r\n", "\n")
	if text == strings.Join(s.lines, "\n") {
		s.toast.Notify("Already formatted")
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
	s.toast.Notify("Formatted with " + res.name)
}

// edFind moves the cursor to the next occurrence of the search term, wrapping
// around the end of the buffer. With advance set the search starts one column
// past the cursor, so repeated Find Next steps through matches.
func edFind(s *edState, advance bool) {
	if s.findTerm == "" {
		s.toast.Notify("No search term")
		return
	}
	needle := []rune(strings.ToLower(s.findTerm))
	top, n := s.viewTop(), s.viewLen()
	for i := 0; i <= n; i++ {
		ln := top + (s.cy-top+i)%n
		hay := []rune(strings.ToLower(s.lines[ln]))
		from := 0
		if i == 0 {
			from = s.cx
			if advance {
				from++
			}
		}
		if from > len(hay) {
			continue
		}
		idx := edIndexRunes(hay[from:], needle)
		if idx < 0 {
			continue
		}
		pos := from + idx
		s.selY, s.selX = ln, pos
		s.cy, s.cx = ln, pos+len(needle)
		s.hasSel = true
		s.goalX = s.cx
		s.clampCursor()
		s.ensureVisible()
		return
	}
	s.toast.Notify("Not found: " + s.findTerm)
}

// edEllipsis shortens s to at most n cells, marking the cut.
func edEllipsis(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "\u2026"
	}
	return s
}

// edOutlineRow renders one outline entry: the bare function name, the top-level
// function it is declared inside when it is a nested one, and its line number,
// each in a fixed column so they stay aligned. Whether a function is local
// shows in the row's colour rather than in the text.
func edOutlineRow(fn edFunc) string {
	const nameCols, ownerCols = 24, 19
	owner := ""
	if fn.nested() {
		owner = "in " + edEllipsis(fn.parent, ownerCols-3)
	}
	return fmt.Sprintf("%-*s %-*s %4d",
		nameCols, edEllipsis(fn.name, nameCols), ownerCols, owner, fn.line+1)
}

// centerOnCursor scrolls the cursor line to mid-view, which reads better after
// a jump than nudging it just far enough to be visible.
func (s *edState) centerOnCursor() {
	s.scrollY = max(0, min(s.cy-s.viewTop()-edRows/2, max(0, s.viewLen()-edRows)))
	s.scrollX = 0
	s.ensureVisible()
}

func edIndexRunes(hay, needle []rune) int {
	if len(needle) == 0 || len(needle) > len(hay) {
		return -1
	}
	for i := 0; i+len(needle) <= len(hay); i++ {
		match := true
		for j := range needle {
			if hay[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// ── input ─────────────────────────────────────────────────────────────────────

// repeatKey reports whether a held key should act this frame. raylib's
// IsKeyPressedRepeat relays the operating system's keyboard repeat, which
// starts after a long delay and runs at whatever rate the user's system
// preferences say - fine for typing, sluggish for moving a caret. Driving it
// from the editor's own clock instead makes movement feel the same everywhere.
func (s *edState) repeatKey(key int32) bool {
	return s.repeatTick(key, float64(rl.GetTime()),
		rl.IsKeyPressed(key), rl.IsKeyDown(key),
		s.repeatID != 0 && rl.IsKeyDown(s.repeatID))
}

// repeatTick is repeatKey's logic with the clock and the key state passed in.
// Only one key repeats at a time - the most recently pressed one - so two keys
// held together cannot take turns stealing the slot and leave neither firing.
func (s *edState) repeatTick(key int32, now float64, pressed, down, ownerDown bool) bool {
	if pressed {
		s.repeatID, s.repeatAt = key, now+edRepeatDelay
		return true
	}
	if !down {
		if s.repeatID == key {
			s.repeatID = 0
		}
		return false
	}
	if s.repeatID != key {
		if ownerDown {
			return false // another key is holding the repeat
		}
		// Held, but nothing owns the repeat: either we never saw it go down,
		// because a dialog had the keyboard, or the previous owner was let go.
		// Start its delay rather than firing at once.
		s.repeatID, s.repeatAt = key, now+edRepeatDelay
		return false
	}
	if now >= s.repeatAt {
		s.repeatAt = now + edRepeatRate
		return true
	}
	return false
}

// edMove runs a cursor movement, maintaining the selection anchor when shift is
// held and dropping the selection otherwise.
func edMove(s *edState, shift bool, move func()) {
	if shift && !s.hasSel {
		s.selX, s.selY = s.cx, s.cy
		s.hasSel = true
	}
	move()
	s.assist.dismiss()
	s.clampCursor()
	if !shift {
		s.hasSel = false
	} else if s.selX == s.cx && s.selY == s.cy {
		s.hasSel = false
	}
	s.typing = false
	s.ensureVisible()
}

// selectWordAt selects the identifier under a position, or the single
// character there when it is not part of one.
func (s *edState) selectWordAt(col, line int) {
	r := s.runes(line)
	from, to := min(col, len(r)), min(col, len(r))
	if to < len(r) && edIsIdentChar(r[to]) {
		for from > 0 && edIsIdentChar(r[from-1]) {
			from--
		}
		for to < len(r) && edIsIdentChar(r[to]) {
			to++
		}
	} else if to < len(r) {
		to++ // not a word: take the one character, so the click still shows
	}
	if from == to {
		return
	}

	s.selY, s.selX = line, from
	s.cy, s.cx = line, to
	s.goalX = s.cx
	s.hasSel = true
	s.selecting = false
	s.ensureVisible()
}

func edWordLeft(s *edState) {
	r := s.runes(s.cy)
	if s.cx == 0 {
		if s.cy > s.viewTop() {
			s.cy--
			s.cx = s.lineLen(s.cy)
		}
		return
	}
	i := s.cx - 1
	for i > 0 && !edIsIdentChar(r[i]) {
		i--
	}
	for i > 0 && edIsIdentChar(r[i-1]) {
		i--
	}
	s.cx = i
}

func edWordRight(s *edState) {
	r := s.runes(s.cy)
	if s.cx >= len(r) {
		if s.cy < s.viewBottom() {
			s.cy++
			s.cx = 0
		}
		return
	}
	i := s.cx
	for i < len(r) && edIsIdentChar(r[i]) {
		i++
	}
	for i < len(r) && !edIsIdentChar(r[i]) {
		i++
	}
	s.cx = i
}

// ── keyboard map ──────────────────────────────────────────────────────────────

// edKeyBinding is one key that runs one action. Everything that is a plain
// key-to-action mapping lives in the tables below; the chords whose meaning
// depends on Shift, and the ones that move the caret and so must repeat, are
// handled explicitly alongside them.
type edKeyBinding struct {
	key int32
	act edAction
}

var edFunctionKeyMap = []edKeyBinding{
	{rl.KeyF1, edActHelp},
	{rl.KeyF2, edActSave},
	{rl.KeyF3, edActOpen},
	{rl.KeyF4, edActFindNext},
	{rl.KeyF5, edActRun},
	{rl.KeyF6, edActNextBuf},
	{rl.KeyF7, edActFormat},
	{rl.KeyF8, edActFocus},
	{rl.KeyF9, edActBuild},
	{rl.KeyF10, edActMenuBar},
	{rl.KeyF12, edActSaveAs},
}

var edAltKeyMap = []edKeyBinding{
	{rl.KeyF2, edActOutline},
	{rl.KeyF6, edActPrevBuf},
	{rl.KeyZero, edActBufList},
	{rl.KeyKp0, edActBufList},
}

var edCtrlKeyMap = []edKeyBinding{
	{rl.KeyN, edActNew},
	{rl.KeyO, edActOpen},
	{rl.KeyQ, edActQuit},
	{rl.KeyW, edActCloseBuf},
	{rl.KeyY, edActRedo},
	{rl.KeyX, edActCut},
	{rl.KeyC, edActCopy},
	{rl.KeyV, edActPaste},
	{rl.KeyA, edActSelectAll},
	{rl.KeyF, edActFind},
	{rl.KeyH, edActReplace},
	{rl.KeyD, edActDupLine},
	{rl.KeyG, edActGoto},
	{rl.KeyE, edActNextProblem},
	{rl.KeySpace, edActComplete},
	{rl.KeyI, edActInlineHelp},

	// Which physical key yields "/" or "-" depends on the keyboard layout, so
	// comment toggling answers to all of them.
	{rl.KeySlash, edActToggleComment},
	{rl.KeyMinus, edActToggleComment},
	{rl.KeyKpSubtract, edActToggleComment},
	{rl.KeyB, edActToggleComment},
}

// edBoundKey runs the first binding in the table whose key was pressed.
func edBoundKey(s *edState, table []edKeyBinding) bool {
	for _, b := range table {
		if rl.IsKeyPressed(b.key) {
			edApply(s, b.act)
			return true
		}
	}
	return false
}

// ── input ─────────────────────────────────────────────────────────────────────

func edHandleInput(s *edState) {
	if edModalInput(s) {
		return
	}

	ctrl := rl.IsKeyDown(rl.KeyLeftControl) || rl.IsKeyDown(rl.KeyRightControl) ||
		rl.IsKeyDown(rl.KeyLeftSuper) || rl.IsKeyDown(rl.KeyRightSuper)
	shift := rl.IsKeyDown(rl.KeyLeftShift) || rl.IsKeyDown(rl.KeyRightShift)
	alt := rl.IsKeyDown(rl.KeyLeftAlt) || rl.IsKeyDown(rl.KeyRightAlt)

	switch {
	case alt && edAltKeys(s):
		// An Alt chord that matched; one that did not falls through to the
		// function keys, so Alt+F1 still opens the help.
	case edBoundKey(s, edFunctionKeyMap):
	case ctrl:
		// Ctrl swallows the keystroke whether or not it is bound, so an unknown
		// chord cannot leak through and type a character.
		edCtrlKeys(s, shift)
	default:
		edTextKeys(s, shift)
		edMouse(s)
	}
}

// edModalInput gives the overlays first claim on the keyboard, in the order
// they sit on top of one another. It reports whether one of them took it.
func edModalInput(s *edState) bool {
	switch {
	case s.showHelp:
		if rl.IsKeyPressed(rl.KeyF1) || rl.IsKeyPressed(rl.KeyEscape) ||
			rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
			s.showHelp = false
		}
		return true
	case s.confirm.Active:
		return true
	case s.dlg != edDlgNone:
		edDialogInput(s)
		return true
	case s.menuOpen >= 0:
		edMenuInput(s)
		return true
	}
	return edAssistInput(s)
}

func edAltKeys(s *edState) bool {
	switch {
	case s.repeatKey(rl.KeyEqual) || s.repeatKey(rl.KeyKpAdd):
		edApply(s, edActFontBigger)
		return true
	case s.repeatKey(rl.KeyMinus) || s.repeatKey(rl.KeyKpSubtract):
		edApply(s, edActFontSmaller)
		return true
	}
	switch {
	case s.repeatKey(rl.KeyUp):
		edApply(s, edActMoveUp)
		return true
	case s.repeatKey(rl.KeyDown):
		edApply(s, edActMoveDown)
		return true
	}
	if edBoundKey(s, edAltKeyMap) {
		return true
	}

	// Alt plus a menu's first letter drops that menu, DOS style.
	for i, m := range edMenus {
		if rl.IsKeyPressed(int32(strings.ToUpper(m.title)[0])) {
			s.menuOpen, s.menuHover = i, -1
			return true
		}
	}
	return false
}

func edCtrlKeys(s *edState, shift bool) bool {
	// Chords that mean something different with Shift held.
	switch {
	case rl.IsKeyPressed(rl.KeyS):
		if shift {
			edApply(s, edActSaveAs)
		} else {
			edApply(s, edActSave)
		}
		return true
	case rl.IsKeyPressed(rl.KeyZ):
		if shift {
			edApply(s, edActRedo)
		} else {
			edApply(s, edActUndo)
		}
		return true
	}

	// Caret movement, which repeats while held.
	switch {
	case s.repeatKey(rl.KeyHome):
		edMove(s, shift, func() { s.cx, s.cy = 0, s.viewTop() })
	case s.repeatKey(rl.KeyEnd):
		edMove(s, shift, func() { s.cy = s.viewBottom(); s.cx = s.lineLen(s.cy) })
	case s.repeatKey(rl.KeyLeft):
		edMove(s, shift, func() { edWordLeft(s) })
	case s.repeatKey(rl.KeyRight):
		edMove(s, shift, func() { edWordRight(s) })
	default:
		return edBoundKey(s, edCtrlKeyMap)
	}
	return true
}

func edTextKeys(s *edState, shift bool) {
	cols := edCols(s)

	switch {
	case s.repeatKey(rl.KeyLeft):
		edMove(s, shift, func() {
			if s.cx > 0 {
				s.cx--
			} else if s.cy > s.viewTop() {
				s.cy--
				s.cx = s.lineLen(s.cy)
			}
			s.goalX = s.cx
		})
	case s.repeatKey(rl.KeyRight):
		edMove(s, shift, func() {
			if s.cx < s.lineLen(s.cy) {
				s.cx++
			} else if s.cy < s.viewBottom() {
				s.cy++
				s.cx = 0
			}
			s.goalX = s.cx
		})
	case s.repeatKey(rl.KeyUp):
		edMove(s, shift, func() {
			if s.cy > s.viewTop() {
				s.cy--
				s.cx = min(s.goalX, s.lineLen(s.cy))
			}
		})
	case s.repeatKey(rl.KeyDown):
		edMove(s, shift, func() {
			if s.cy < s.viewBottom() {
				s.cy++
				s.cx = min(s.goalX, s.lineLen(s.cy))
			}
		})
	case s.repeatKey(rl.KeyPageUp):
		edMove(s, shift, func() {
			s.cy = max(s.viewTop(), s.cy-edRows)
			s.scrollY = max(0, s.scrollY-edRows)
			s.cx = min(s.goalX, s.lineLen(s.cy))
		})
	case s.repeatKey(rl.KeyPageDown):
		edMove(s, shift, func() {
			s.cy = min(s.viewBottom(), s.cy+edRows)
			s.scrollY = min(max(0, s.viewLen()-1), s.scrollY+edRows)
			s.cx = min(s.goalX, s.lineLen(s.cy))
		})
	case s.repeatKey(rl.KeyHome):
		edMove(s, shift, func() {
			// First press jumps to the first non-blank column, second to zero.
			indent := len([]rune(edLeadingWS(s.lines[s.cy])))
			if s.cx == indent {
				s.cx = 0
			} else {
				s.cx = indent
			}
			s.goalX = s.cx
		})
	case s.repeatKey(rl.KeyEnd):
		edMove(s, shift, func() { s.cx = s.lineLen(s.cy); s.goalX = s.cx })
	case rl.IsKeyPressed(rl.KeyEscape):
		s.hasSel = false

	case rl.IsKeyPressed(rl.KeyTab) || rl.IsKeyPressedRepeat(rl.KeyTab):
		switch {
		case shift:
			edApply(s, edActUnindent)
		case s.hasSel:
			edApply(s, edActIndent)
		default:
			s.beginEdit(true)
			s.insert(s.indentPad())
			s.ensureVisible()
		}
	case s.repeatKey(rl.KeyEnter) || s.repeatKey(rl.KeyKpEnter):
		s.beginEdit(false)
		s.newline()
		s.ensureVisible()
	case s.repeatKey(rl.KeyBackspace):
		s.beginEdit(true)
		s.backspace()
		s.ensureVisible()
	case s.repeatKey(rl.KeyDelete):
		s.beginEdit(true)
		s.deleteForward()
		s.ensureVisible()
	}

	// Typed characters.
	typed := ""
	for c := rl.GetCharPressed(); c != 0; c = rl.GetCharPressed() {
		if c < 32 {
			continue
		}
		s.beginEdit(true)
		typed += edTypeRune(s, c)
	}
	edAssistAfterEdit(s)
	edAssistAfterTyping(s, typed)
	if s.cx >= s.scrollX+cols {
		s.ensureVisible()
	}
}

func edMouse(s *edState) {
	mp := rl.GetMousePosition()

	if rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
		if mp.Y < float32(edMenuH) {
			for i := range edMenus {
				if rl.CheckCollisionPointRec(mp, edMenuTitleRect(i)) {
					s.menuOpen, s.menuHover = i, -1
					return
				}
			}
		}
		if rl.CheckCollisionPointRec(mp, edCloseBoxRect()) {
			edApply(s, edActQuit)
			return
		}
		if rl.CheckCollisionPointRec(mp, edTextRect(s)) {
			s.assist.dismiss()
			x, y := edPosFromMouse(s, mp)

			// A second click on the same cell in quick succession takes the
			// whole word instead of placing the caret again.
			now := float64(rl.GetTime())
			if now-s.clickAt < edDoubleClick && x == s.clickX && y == s.clickY {
				s.clickAt = 0
				s.selectWordAt(x, y)
				return
			}
			s.clickAt, s.clickX, s.clickY = now, x, y

			s.selX, s.selY = x, y
			s.cx, s.cy = x, y
			s.goalX = x
			s.hasSel = false
			s.selecting = true
			s.typing = false
		}
	}

	if s.selecting {
		if rl.IsMouseButtonDown(rl.MouseButtonLeft) {
			x, y := edPosFromMouse(s, mp)
			s.cx, s.cy = x, y
			s.goalX = x
			s.hasSel = x != s.selX || y != s.selY
			s.ensureVisible()
		} else {
			s.selecting = false
		}
	}

	if w := rl.GetMouseWheelMove(); w != 0 {
		s.scrollY = max(0, min(s.scrollY-int(w*3), max(0, len(s.lines)-1)))
	}
}

func edCloseBoxRect() rl.Rectangle {
	return rl.NewRectangle(float32(edFrameX+1+edCharW), float32(edFrameY+1),
		float32(3*edCharW), float32(edLineH))
}

func edTextRect(s *edState) rl.Rectangle {
	x := edTextX0(s)
	return rl.NewRectangle(float32(x), float32(edTextY0), float32(edVScrX-x), float32(edTextH))
}

// edPosFromMouse maps a canvas position to a (column, line) caret position.
func edPosFromMouse(s *edState, p rl.Vector2) (int, int) {
	row := int((p.Y - float32(edTextY0)) / float32(edTxtLineH))
	if p.Y < float32(edTextY0) {
		row = 0
	}
	y := max(s.viewTop(), min(s.viewTop()+s.scrollY+row, s.viewBottom()))

	col := 0
	if dx := p.X - float32(edTextX0(s)); dx > 0 {
		col = int((dx + float32(edTxtCharW)/2) / float32(edTxtCharW))
	}
	x := max(0, min(s.scrollX+col, s.lineLen(y)))
	return x, y
}

func edMenuInput(s *edState) {
	if rl.IsKeyPressed(rl.KeyEscape) || rl.IsKeyPressed(rl.KeyF10) {
		s.menuOpen = -1
		return
	}
	switch {
	case rl.IsKeyPressed(rl.KeyDown):
		edMenuStep(s, 1)
	case rl.IsKeyPressed(rl.KeyUp):
		edMenuStep(s, -1)
	case rl.IsKeyPressed(rl.KeyRight):
		s.menuOpen = (s.menuOpen + 1) % len(edMenus)
		s.menuHover = -1
	case rl.IsKeyPressed(rl.KeyLeft):
		s.menuOpen = (s.menuOpen + len(edMenus) - 1) % len(edMenus)
		s.menuHover = -1
	case rl.IsKeyPressed(rl.KeyEnter) || rl.IsKeyPressed(rl.KeyKpEnter):
		if s.menuHover >= 0 {
			act := edMenus[s.menuOpen].items[s.menuHover].act
			s.menuOpen = -1
			edApply(s, act)
		}
		return
	}

	mp := rl.GetMousePosition()
	for i := range edMenus {
		if !rl.CheckCollisionPointRec(mp, edMenuTitleRect(i)) {
			continue
		}
		if rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
			s.menuOpen = -1 // clicking the open title closes the menu
		} else if s.menuOpen != i {
			s.menuOpen, s.menuHover = i, -1
		}
		return
	}

	if idx := edMenuItemAt(s.menuOpen, mp); idx >= 0 {
		s.menuHover = idx
	}
	if rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
		idx := edMenuItemAt(s.menuOpen, mp)
		if idx >= 0 {
			act := edMenus[s.menuOpen].items[idx].act
			s.menuOpen = -1
			edApply(s, act)
		} else if !rl.CheckCollisionPointRec(mp, edMenuDropRect(s.menuOpen)) {
			s.menuOpen = -1
		}
	}
}

// openDialog switches to a modal dialog and marks it fresh. A dialog is drawn
// in the same frame as the input that opened it, and raygui reads the live key
// and mouse state rather than a queue - so without this the ENTER that picked
// "Find..." off the menu would also be seen by the dialog's own text box, which
// treats it as "accepted", and the dialog would vanish the instant it appeared.
// A click does the same, since a text box in edit mode takes a click outside
// its bounds as confirmation.
func (s *edState) openDialog(kind edDialogKind) {
	s.dlg = kind
	s.dlgFresh = true
}

// edListDialog reports whether a dialog kind is one of the list pickers.
func edListDialog(d edDialogKind) bool {
	return d == edDlgOpen || d == edDlgOutline || d == edDlgBuffers
}

func edDialogInput(s *edState) {
	if rl.IsKeyPressed(rl.KeyEscape) {
		s.dlg = edDlgNone
		return
	}
	if s.dlg == edDlgReplace && rl.IsKeyPressed(rl.KeyTab) {
		s.dlgField = 1 - s.dlgField
		return
	}
	if edListDialog(s.dlg) {
		switch {
		case s.repeatKey(rl.KeyDown):
			edListMove(s, 1)
		case s.repeatKey(rl.KeyUp):
			edListMove(s, -1)
		case s.repeatKey(rl.KeyPageDown):
			edListMove(s, edListVisibleRows())
		case s.repeatKey(rl.KeyPageUp):
			edListMove(s, -edListVisibleRows())
		case rl.IsKeyPressed(rl.KeyHome):
			edListMove(s, -len(s.dlgList))
		case rl.IsKeyPressed(rl.KeyEnd):
			edListMove(s, len(s.dlgList))
		case rl.IsKeyPressed(rl.KeyEnter) || rl.IsKeyPressed(rl.KeyKpEnter):
			edDialogConfirm(s)
		}
	}
}

// edDialogConfirm applies whichever dialog is currently open and closes it.
func edDialogConfirm(s *edState) {
	switch s.dlg {
	case edDlgOpen:
		if s.dlgActive < 0 || int(s.dlgActive) >= len(s.dlgList) {
			s.toast.Notify("No file selected")
			return
		}
		path := s.dlgList[s.dlgActive]
		if i := edFindBuffer(s, path); i >= 0 {
			edSwitchBuffer(s, i)
			s.toast.Notify("Already open: " + path)
			break
		}
		lines, err := edReadFile(path)
		if err != nil {
			s.toast.Notify("Open failed: " + err.Error())
			return
		}
		edOpenBuffer(s, path, lines)
		s.toast.Notify("Opened " + path)

	case edDlgBuffers:
		if s.dlgActive < 0 || int(s.dlgActive) >= len(s.buffers) {
			return
		}
		edSwitchBuffer(s, int(s.dlgActive))

	case edDlgOutline:
		if s.dlgActive < 0 || int(s.dlgActive) >= len(s.dlgFuncs) {
			s.toast.Notify("No function selected")
			return
		}
		fn := s.dlgFuncs[s.dlgActive]
		s.focus = edFocusWhole() // step out before jumping, then re-derive
		s.cy = max(0, min(fn.line, len(s.lines)-1))
		s.cx, s.goalX = 0, 0
		s.hasSel = false
		s.refocus()
		s.centerOnCursor()

	case edDlgSaveAs:
		name := strings.TrimSpace(s.dlgInput)
		if name == "" {
			s.toast.Notify("Enter a file name")
			return
		}
		if filepath.Ext(name) == "" {
			name += ".lua"
		}
		if err := edSaveFile(s, name); err != nil {
			s.toast.Notify("Save failed: " + err.Error())
		} else {
			s.toast.Notify("Saved " + name)
		}

	case edDlgFind:
		s.findTerm = s.dlgInput
		s.dlg = edDlgNone
		edFind(s, false)
		return

	case edDlgReplace:
		// Enter replaces the current match and steps on, leaving the dialog up
		// so the next one can be dealt with too.
		edTakeReplaceTerms(s)
		edReplaceOnce(s)
		return

	case edDlgGoto:
		n, err := strconv.Atoi(strings.TrimSpace(s.dlgInput))
		if err != nil {
			s.toast.Notify("Not a line number")
			return
		}
		s.focus = edFocusWhole()
		s.cy = max(0, min(n-1, len(s.lines)-1))
		s.cx, s.goalX = 0, 0
		s.hasSel = false
		s.refocus()
		s.ensureVisible()
	}
	s.dlg = edDlgNone
}

func edDrawHelp(s *edState) {
	if !s.showHelp {
		return
	}
	rl.DrawRectangle(0, 0, virtualW, virtualH, rl.NewColor(0, 0, 0, 180))

	type row struct{ key, desc string }
	left := []row{
		{"F2 / ^S", "Save"},
		{"F12 / ^S+Sh", "Save As"},
		{"F3 / ^O", "Open"},
		{"^N", "New buffer"},
		{"F5", "Run the game"},
		{"^Q", "Quit"},
		{},
		{"^Z / ^Y", "Undo / Redo"},
		{"^X ^C ^V", "Cut / Copy / Paste"},
		{"^A", "Select all"},
		{"^Space", "Complete"},
		{"^I", "Inline help"},
		{"F7", "Format document"},
		{"^/ ^- ^B", "Toggle comment"},
		{"^D", "Duplicate line"},
		{"Alt+Up / Dn", "Move line"},
		{"^H", "Replace"},
		{},
		{"F8", "Function focus"},
		{"F9", "Build the project"},
		{"Alt+ / Alt-", "Text size"},
	}
	right := []row{
		{"^F", "Find"},
		{"F4", "Find next"},
		{"^G", "Go to line"},
		{"^E", "Next problem"},
		{"Alt+F2", "Function outline"},
		{},
		{"Tab / Sh+Tab", "Indent / unindent"},
		{"Shift+arrows", "Extend selection"},
		{"^Left / ^Right", "Word jump"},
		{"^Home / ^End", "Doc start / end"},
		{},
		{"F6 / Alt+F6", "Next / previous buffer"},
		{"Alt+0", "Buffer list"},
		{"^W", "Close buffer"},
		{},
		{"F10 / Alt+key", "Open the menu bar"},
	}

	// The panel grows with the character cell, so it fits either font.
	const w = int32(608) // widest row: right column + "Next / previous buffer"
	h := 2*edLineH + int32(max(len(left), len(right)))*edLineH + 28
	x, y := edDialogFrame("Keyboard - F1 or Esc to close", w, h)

	const keyCols = 16 // widest shortcut ("^Left / ^Right") plus a gap
	draw := func(rows []row, cx int32) {
		ry := y + edLineH + 12
		for _, r := range rows {
			if r.key != "" {
				edDrawStr(cx, ry, r.key, edEgaWhite)
				edDrawStr(cx+keyCols*edCharW, ry, r.desc, edEgaBlack)
			}
			ry += edLineH
		}
	}
	draw(left, x+edCharW)
	draw(right, x+edCharW+36*edCharW)

	footer := fmt.Sprintf("fz edit v%s - Lua source editor - formatter: %s",
		version, edPickFormatter().name)
	edDrawStr(x+edCharW, y+h-edLineH-8, footer, edEgaBlack)
}
