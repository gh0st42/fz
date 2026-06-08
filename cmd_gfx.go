package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/gen2brain/raylib-go/raygui"
)

// ── virtual canvas ────────────────────────────────────────────────────────────

const (
	virtualW = int32(640)
	virtualH = int32(504) // extra 24px below toolbox for the tile-flags row

	toolbarH   = int32(28)
	statusBarH = int32(20)

	drawAreaX  = int32(4)
	drawAreaY  = toolbarH + 4 // 32
	drawAreaSz = int32(384) // screen pixels for the single-tile editing area
)

// ── right panel layout ────────────────────────────────────────────────────────

const (
	panelX = drawAreaX + drawAreaSz + 4 // 392
	panelW = virtualW - panelX - 4      // 244

	// Palette
	palCols   = int32(8)
	swatchSz  = int32(24)
	swatchGap = int32(2)
	palStartX = panelX + 4
	palStartY = drawAreaY + 14 // below "PALETTE" label

	// Sheet preview (below palette, ~y=202)
	// previewSz is the fixed on-screen budget; quadrant size and scale are dynamic.
	previewSz = int32(192)
	previewX  = panelX + (panelW-previewSz)/2 // centred in panel: 418
	sheetTabY    = int32(202)
	sheetTabH    = int32(18)
	previewGridY = sheetTabY + sheetTabH + 2 // where tiles start: 222

	// Toolbox row below the drawing area
	toolboxY     = drawAreaY + drawAreaSz + 4 // 420
	toolboxBtnSz = int32(24)

	// Tile-flags row below the toolbox
	flagsRowY = toolboxY + toolboxBtnSz + 6 // 450
)

// ── palette ───────────────────────────────────────────────────────────────────

// picotronPalette is the Picotron 020 WIP v6 palette (32 colours).
// Source: https://lospec.com/palette-list/picotron-020-wip-v6
var picotronPalette = [32]rl.Color{
	rl.NewColor(0, 0, 0, 255),
	rl.NewColor(29, 43, 83, 255),
	rl.NewColor(126, 37, 83, 255),
	rl.NewColor(0, 135, 81, 255),
	rl.NewColor(171, 82, 54, 255),
	rl.NewColor(95, 87, 79, 255),
	rl.NewColor(194, 195, 199, 255),
	rl.NewColor(255, 241, 232, 255),
	rl.NewColor(255, 0, 77, 255),
	rl.NewColor(255, 163, 0, 255),
	rl.NewColor(255, 236, 39, 255),
	rl.NewColor(0, 228, 54, 255),
	rl.NewColor(41, 173, 255, 255),
	rl.NewColor(131, 118, 156, 255),
	rl.NewColor(255, 119, 168, 255),
	rl.NewColor(255, 204, 170, 255),
	rl.NewColor(36, 99, 176, 255),
	rl.NewColor(0, 165, 161, 255),
	rl.NewColor(101, 70, 136, 255),
	rl.NewColor(18, 83, 89, 255),
	rl.NewColor(116, 47, 41, 255),
	rl.NewColor(69, 45, 50, 255),
	rl.NewColor(162, 136, 121, 255),
	rl.NewColor(255, 172, 197, 255),
	rl.NewColor(185, 0, 62, 255),
	rl.NewColor(226, 107, 2, 255),
	rl.NewColor(149, 240, 66, 255),
	rl.NewColor(0, 178, 81, 255),
	rl.NewColor(100, 223, 246, 255),
	rl.NewColor(189, 154, 223, 255),
	rl.NewColor(228, 13, 171, 255),
	rl.NewColor(255, 133, 87, 255),
}

// ── tools ─────────────────────────────────────────────────────────────────────

type drawTool int

const (
	toolPencil drawTool = iota
	toolBucket
	toolEraser
)

// ── dialog mode ───────────────────────────────────────────────────────────────

type dialogMode int

const (
	modeSaveAs dialogMode = iota
	modeNew
)

// ── state ─────────────────────────────────────────────────────────────────────

var tileSizeCycle = []int{8, 16, 32, 64}

type saveDialog struct {
	active   bool
	mode     dialogMode
	filename string // text being typed (no directory, no extension required)
}

type gfxState struct {
	imgPath        string
	img            *rl.Image
	tex            rl.Texture2D
	tileSize       int // current tile size: 8, 16, 32, or 64
	tileX          int // selected tile column in the sheet
	tileY          int // selected tile row in the sheet
	showGrid       bool
	showSymmetry   bool
	selectedColor  int
	activeQuadrant int // 0–3 maps to sheet quadrants 1–4
	sheetSz        int // actual image width/height in pixels
	activeTool     drawTool
	clipboard      []rl.Color // nil = empty
	iconCCW        rl.RenderTexture2D // ROTATE_FILL icon pre-rendered for H-flip
	dialog         saveDialog
	texDirty       bool // image was modified; texture needs uploading before next draw
	tileFlags      map[int]uint8 // tile index → 8-bit flag mask; zero entries omitted
	showHelp       bool

	// file switcher dropdown
	fileList   []string // basenames of .png files found in assets/gfx
	fileActive int32    // index of currently displayed item in dropdown
	fileEdit   bool     // true while the dropdown is open
	fileText   string   // semicolon-joined names passed to raygui
}

// tileZoom returns the screen pixels per tile pixel for the drawing area.
func (s *gfxState) tileZoom() int32 { return drawAreaSz / int32(s.tileSize) }

// tileIndex returns the linear tile index within the full sheet.
func (s *gfxState) tileIndex() int {
	return s.tileY*(s.sheetSz/s.tileSize) + s.tileX
}

// tileCount returns the total number of tiles on the sheet.
func (s *gfxState) tileCount() int {
	n := s.sheetSz / s.tileSize
	return n * n
}

// quadrantSz returns the pixel size of one sheet quadrant (half the sheet).
func (s *gfxState) quadrantSz() int { return s.sheetSz / 2 }

// previewScaleF returns the float scale factor from quadrant pixels to screen pixels.
func (s *gfxState) previewScaleF() float32 {
	return float32(previewSz) / float32(s.quadrantSz())
}

// ── entry point ───────────────────────────────────────────────────────────────

func runGfx(args []string) error {
	var filename string
	if len(args) > 0 {
		filename = args[0]
	}

	imgPath, img, err := resolveGfxImage(filename)
	if err != nil {
		return err
	}

	rl.SetConfigFlags(rl.FlagWindowResizable | rl.FlagWindowHighdpi)
	rl.InitWindow(virtualW*2, virtualH*2, "fz gfx — "+filepath.Base(imgPath))
	rl.SetTargetFPS(60)
	defer rl.CloseWindow()

	// Normalise to RGBA8 so img.Data is a plain []byte-compatible pixel buffer
	// that UpdateTexture can upload without conversion.
	rl.ImageFormat(img, rl.UncompressedR8g8b8a8)

	// Pre-render the CCW rotation icon (ROTATE_FILL flipped horizontally).
	iconCCW := rl.LoadRenderTexture(16, 16)
	rl.BeginTextureMode(iconCCW)
	rl.ClearBackground(rl.NewColor(0, 0, 0, 0))
	raygui.DrawIcon(raygui.ICON_ROTATE_FILL, 0, 0, 1, rl.White)
	rl.EndTextureMode()
	defer rl.UnloadRenderTexture(iconCCW)

	state := &gfxState{
		imgPath:   imgPath,
		img:       img,
		tex:       rl.LoadTextureFromImage(img),
		tileSize:  16,
		showGrid:  true,
		sheetSz:   int(img.Width),
		iconCCW:   iconCCW,
		tileFlags: make(map[int]uint8),
	}
	rl.SetTextureFilter(state.tex, rl.FilterPoint)
	defer func() { rl.UnloadTexture(state.tex) }()

	refreshFileList(state)
	loadTileMeta(state)

	canvas := rl.LoadRenderTexture(virtualW, virtualH)
	defer rl.UnloadRenderTexture(canvas)

	for !rl.WindowShouldClose() {
		scale, offsetX, offsetY := virtualScale()
		// BeginDrawing() with FLAG_WINDOW_HIGHDPI draws in screen (logical) coords,
		// but virtualScale() uses render (physical) coords. Convert for draw calls.
		dpi := float32(rl.GetRenderWidth()) / float32(rl.GetScreenWidth())
		screenScale := scale / dpi
		screenOffX := offsetX / dpi
		screenOffY := offsetY / dpi

		rl.SetMouseOffset(int(-screenOffX), int(-screenOffY))
		rl.SetMouseScale(1/screenScale, 1/screenScale)

		handleGfxInput(state)

		if state.texDirty {
			rl.UnloadTexture(state.tex)
			state.tex = rl.LoadTextureFromImage(state.img)
			rl.SetTextureFilter(state.tex, rl.FilterPoint)
			state.texDirty = false
		}

		rl.BeginTextureMode(canvas)
		drawGfxScene(state)
		rl.EndTextureMode()

		rl.SetMouseOffset(0, 0)
		rl.SetMouseScale(1, 1)

		rl.BeginDrawing()
		rl.ClearBackground(rl.Black)
		src := rl.NewRectangle(0, 0, float32(virtualW), -float32(virtualH))
		dst := rl.NewRectangle(screenOffX, screenOffY, float32(virtualW)*screenScale, float32(virtualH)*screenScale)
		rl.DrawTexturePro(canvas.Texture, src, dst, rl.NewVector2(0, 0), 0, rl.White)
		rl.EndDrawing()
	}

	return nil
}

// ── input ─────────────────────────────────────────────────────────────────────

func handleGfxInput(s *gfxState) {
	if s.showHelp {
		if rl.IsKeyPressed(rl.KeyF1) || rl.IsKeyPressed(rl.KeyEscape) {
			s.showHelp = false
		}
		return
	}
	if s.dialog.active {
		handleDialogInput(s)
		return
	}
	if s.fileEdit {
		return // dropdown is open; don't forward clicks to canvas or palette
	}

	mod := rl.IsKeyDown(rl.KeyLeftControl) || rl.IsKeyDown(rl.KeyRightControl) ||
		rl.IsKeyDown(rl.KeyLeftSuper) || rl.IsKeyDown(rl.KeyRightSuper)
	alt := rl.IsKeyDown(rl.KeyLeftAlt) || rl.IsKeyDown(rl.KeyRightAlt)

	// Ctrl/Cmd shortcuts – always handled first, always return early.
	if mod {
		switch {
		case rl.IsKeyPressed(rl.KeyS):
			triggerSave(s)
		case rl.IsKeyPressed(rl.KeyC):
			s.tileCopy()
		case rl.IsKeyPressed(rl.KeyX):
			s.tileCut()
		case rl.IsKeyPressed(rl.KeyV):
			s.tilePaste()
		case rl.IsKeyPressed(rl.KeyD):
			s.tileClear()
		}
		return
	}

	// Plain key shortcuts.
	switch {
	case rl.IsKeyPressed(rl.KeyF1):
		s.showHelp = true
	case rl.IsKeyPressed(rl.KeyS):
		for i, sz := range tileSizeCycle {
			if s.tileSize == sz {
				s.tileSize = tileSizeCycle[(i+1)%len(tileSizeCycle)]
				maxTile := s.sheetSz/s.tileSize - 1
				if s.tileX > maxTile {
					s.tileX = maxTile
				}
				if s.tileY > maxTile {
					s.tileY = maxTile
				}
				break
			}
		}
	case rl.IsKeyPressed(rl.KeyG):
		s.showGrid = !s.showGrid
	case rl.IsKeyPressed(rl.KeyEqual) || rl.IsKeyPressed(rl.KeyKpAdd):
		s.showSymmetry = !s.showSymmetry
	case rl.IsKeyPressed(rl.KeyP):
		s.activeTool = toolPencil
	case rl.IsKeyPressed(rl.KeyF):
		s.activeTool = toolBucket
	case rl.IsKeyPressed(rl.KeyE):
		s.activeTool = toolEraser
	case rl.IsKeyPressed(rl.KeyH):
		s.tileFlipH()
	case rl.IsKeyPressed(rl.KeyV):
		s.tileFlipV()
	case rl.IsKeyPressed(rl.KeyR):
		if alt {
			s.tileRotateCCW()
		} else {
			s.tileRotateCW()
		}
	// Alt+1..4 → switch sheet quadrant; plain 1..4 fall through to flag-bit toggle.
	case rl.IsKeyPressed(rl.KeyOne):
		if alt {
			s.activeQuadrant = 0
		} else {
			s.toggleTileFlag(1)
		}
	case rl.IsKeyPressed(rl.KeyTwo):
		if alt {
			s.activeQuadrant = 1
		} else {
			s.toggleTileFlag(2)
		}
	case rl.IsKeyPressed(rl.KeyThree):
		if alt {
			s.activeQuadrant = 2
		} else {
			s.toggleTileFlag(3)
		}
	case rl.IsKeyPressed(rl.KeyFour):
		if alt {
			s.activeQuadrant = 3
		} else {
			s.toggleTileFlag(4)
		}
	case rl.IsKeyPressed(rl.KeyZero):
		s.toggleTileFlag(0)
	case rl.IsKeyPressed(rl.KeyFive):
		s.toggleTileFlag(5)
	case rl.IsKeyPressed(rl.KeySix):
		s.toggleTileFlag(6)
	case rl.IsKeyPressed(rl.KeySeven):
		s.toggleTileFlag(7)
	}

	// Palette swatch click.
	if rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
		mouse := rl.GetMousePosition()
		for i := range picotronPalette {
			col := int32(i) % palCols
			row := int32(i) / palCols
			x := float32(palStartX + col*(swatchSz+swatchGap))
			y := float32(palStartY + row*(swatchSz+swatchGap))
			if rl.CheckCollisionPointRec(mouse, rl.NewRectangle(x, y, float32(swatchSz), float32(swatchSz))) {
				s.selectedColor = i
			}
		}
	}

	// Draw area mouse input.
	mouse := rl.GetMousePosition()
	mx := int32(mouse.X)
	my := int32(mouse.Y)
	if mx >= drawAreaX && mx < drawAreaX+drawAreaSz && my >= drawAreaY && my < drawAreaY+drawAreaSz {
		zoom := s.tileZoom()
		pixX := (mx - drawAreaX) / zoom
		pixY := (my - drawAreaY) / zoom
		imgX := int32(s.tileX*s.tileSize) + pixX
		imgY := int32(s.tileY*s.tileSize) + pixY

		if rl.IsMouseButtonDown(rl.MouseButtonLeft) {
			switch s.activeTool {
			case toolPencil:
				rl.ImageDrawPixel(s.img, imgX, imgY, picotronPalette[s.selectedColor])
				s.texDirty = true
			case toolEraser:
				rl.ImageDrawPixel(s.img, imgX, imgY, rl.NewColor(0, 0, 0, 0))
				s.texDirty = true
			case toolBucket:
				if rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
					s.tileFill(imgX, imgY)
				}
			}
		}
		if rl.IsMouseButtonPressed(rl.MouseButtonRight) {
			c := rl.GetImageColor(*s.img, imgX, imgY)
			s.selectedColor = closestPaletteColor(c)
		}
	}
}

func handleDialogInput(s *gfxState) {
	// Accumulate typed characters.
	for {
		c := rl.GetCharPressed()
		if c == 0 {
			break
		}
		if c >= 32 && c < 127 {
			s.dialog.filename += string(c)
		}
	}
	if rl.IsKeyPressed(rl.KeyBackspace) && len(s.dialog.filename) > 0 {
		r := []rune(s.dialog.filename)
		s.dialog.filename = string(r[:len(r)-1])
	}
	if rl.IsKeyPressed(rl.KeyEnter) || rl.IsKeyPressed(rl.KeyKpEnter) {
		if s.dialog.mode == modeNew {
			confirmNew(s)
		} else {
			confirmSave(s)
		}
	}
	if rl.IsKeyPressed(rl.KeyEscape) {
		s.dialog.active = false
	}
}

// ── drawing ───────────────────────────────────────────────────────────────────

func drawGfxScene(s *gfxState) {
	rl.ClearBackground(rl.NewColor(45, 45, 48, 255))
	drawToolbar(s)
	drawTileEditor(s)
	drawToolbox(s)
	drawTileFlags(s)
	drawPalettePanel(s)
	drawSheetPreview(s)
	drawStatusBar(s)
	drawSaveDialog(s)      // modal overlay — drawn last so it sits on top
	drawToolbarDropdown(s) // expanded dropdown must render above all other content
	drawHelpPage(s)        // help overlay is topmost
}

func drawToolbar(s *gfxState) {
	rl.DrawRectangle(0, 0, virtualW, toolbarH, rl.NewColor(30, 30, 30, 255))
	rl.DrawLine(0, toolbarH, virtualW, toolbarH, rl.NewColor(60, 60, 60, 255))
	if !s.dialog.active {
		if raygui.Button(rl.NewRectangle(4, 4, 44, 20), "Save") {
			triggerSave(s)
		}
		if raygui.Button(rl.NewRectangle(52, 4, 36, 20), "New") {
			s.dialog.mode = modeNew
			s.dialog.filename = ""
			s.dialog.active = true
		}
	}
}

// drawToolbarDropdown is called at the end of drawGfxScene so the expanded
// list renders on top of the canvas, tile editor, and other elements.
func drawToolbarDropdown(s *gfxState) {
	if s.dialog.active {
		return
	}
	prevEdit := s.fileEdit
	if raygui.DropdownBox(rl.NewRectangle(92, 4, 170, 20), s.fileText, &s.fileActive, s.fileEdit) {
		s.fileEdit = !s.fileEdit
	}
	switch {
	case !prevEdit && s.fileEdit:
		// Dropdown just opened: refresh from disk so the list is always current.
		refreshFileList(s)
	case prevEdit && !s.fileEdit && len(s.fileList) > 0:
		// Dropdown just closed: load the selected file.
		if int(s.fileActive) < len(s.fileList) {
			loadGfxFile(s, s.fileList[s.fileActive])
		}
	}
}

// drawTileEditor renders the large editing area showing one tile zoomed in.
// Pixels are read directly from s.img so the view is always current without
// needing a GPU texture upload.
func drawTileEditor(s *gfxState) {
	zoom := s.tileZoom()
	tsz := int32(s.tileSize)

	// Checkerboard background: one cell per tile pixel.
	for row := range tsz {
		for col := range tsz {
			x := drawAreaX + col*zoom
			y := drawAreaY + row*zoom
			var c rl.Color
			if (row+col)%2 == 0 {
				c = rl.NewColor(160, 160, 160, 255)
			} else {
				c = rl.NewColor(100, 100, 100, 255)
			}
			rl.DrawRectangle(x, y, zoom, zoom, c)
		}
	}

	// Draw each tile pixel as a rectangle read straight from the image.
	baseX := int32(s.tileX * s.tileSize)
	baseY := int32(s.tileY * s.tileSize)
	for row := range tsz {
		for col := range tsz {
			c := rl.GetImageColor(*s.img, baseX+col, baseY+row)
			if c.A == 0 {
				continue
			}
			rl.DrawRectangle(drawAreaX+col*zoom, drawAreaY+row*zoom, zoom, zoom, c)
		}
	}

	// Pixel grid: one line per pixel in the tile.
	if s.showGrid {
		gc := rl.NewColor(0, 0, 0, 70)
		for i := int32(0); i <= tsz; i++ {
			x := drawAreaX + i*zoom
			rl.DrawLine(x, drawAreaY, x, drawAreaY+drawAreaSz, gc)
			yy := drawAreaY + i*zoom
			rl.DrawLine(drawAreaX, yy, drawAreaX+drawAreaSz, yy, gc)
		}
	}

	// Symmetry cross through the centre.
	if s.showSymmetry {
		cx := drawAreaX + drawAreaSz/2
		cy := drawAreaY + drawAreaSz/2
		rl.DrawLine(cx, drawAreaY, cx, drawAreaY+drawAreaSz, rl.Yellow)
		rl.DrawLine(drawAreaX, cy, drawAreaX+drawAreaSz, cy, rl.Yellow)
	}

	rl.DrawRectangleLines(drawAreaX, drawAreaY, drawAreaSz, drawAreaSz, rl.NewColor(80, 80, 80, 255))
}

func drawPalettePanel(s *gfxState) {
	rl.DrawText("PALETTE", palStartX, drawAreaY, 10, rl.NewColor(180, 180, 180, 255))

	for i, c := range picotronPalette {
		col := int32(i) % palCols
		row := int32(i) / palCols
		x := palStartX + col*(swatchSz+swatchGap)
		y := palStartY + row*(swatchSz+swatchGap)
		rl.DrawRectangle(x, y, swatchSz, swatchSz, c)
		if i == s.selectedColor {
			rl.DrawRectangleLines(x-2, y-2, swatchSz+4, swatchSz+4, rl.White)
			rl.DrawRectangleLines(x-1, y-1, swatchSz+2, swatchSz+2, rl.NewColor(0, 0, 0, 180))
		}
	}

	// Divider + selected colour preview.
	palGridH := (swatchSz+swatchGap)*int32(len(picotronPalette))/palCols - swatchGap
	divY := palStartY + palGridH + 6
	rl.DrawLine(palStartX, divY, palStartX+palCols*(swatchSz+swatchGap)-swatchGap, divY,
		rl.NewColor(60, 60, 60, 255))

	previewY := divY + 4
	previewSwatch := int32(36)
	selC := picotronPalette[s.selectedColor]
	rl.DrawRectangle(palStartX, previewY, previewSwatch, previewSwatch, selC)
	rl.DrawRectangleLines(palStartX-1, previewY-1, previewSwatch+2, previewSwatch+2,
		rl.NewColor(80, 80, 80, 255))
	lx := palStartX + previewSwatch + 6
	rl.DrawText(fmt.Sprintf("#%02x%02x%02x", selC.R, selC.G, selC.B), lx, previewY+2, 10,
		rl.NewColor(200, 200, 200, 255))
	rl.DrawText(fmt.Sprintf("idx %d", s.selectedColor), lx, previewY+16, 10,
		rl.NewColor(140, 140, 140, 255))
}

// drawSheetPreview renders the tabbed quadrant preview in the lower-right panel.
func drawSheetPreview(s *gfxState) {
	// ── tabs 1–4 ──────────────────────────────────────────────────────────────
	tabW := previewSz / 4 // 48
	for i := range 4 {
		tx := previewX + int32(i)*tabW
		active := s.activeQuadrant == i
		var bg rl.Color
		if active {
			bg = rl.NewColor(70, 100, 140, 255)
		} else {
			bg = rl.NewColor(45, 45, 50, 255)
		}
		rl.DrawRectangle(tx, sheetTabY, tabW, sheetTabH, bg)
		rl.DrawRectangleLines(tx, sheetTabY, tabW, sheetTabH, rl.NewColor(60, 60, 60, 255))
		label := fmt.Sprintf("%d", i+1)
		rl.DrawText(label, tx+tabW/2-3, sheetTabY+4, 10, rl.White)

		mouse := rl.GetMousePosition()
		r := rl.NewRectangle(float32(tx), float32(sheetTabY), float32(tabW), float32(sheetTabH))
		if rl.CheckCollisionPointRec(mouse, r) && rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
			s.activeQuadrant = i
		}
	}

	// ── dynamic quadrant geometry ─────────────────────────────────────────────
	qSz := s.quadrantSz()   // sheet pixels per quadrant side (e.g. 128 for a 256px sheet)
	scaleF := s.previewScaleF() // screen pixels per sheet pixel (e.g. 1.5)

	// Q0=top-left, Q1=top-right, Q2=bottom-left, Q3=bottom-right
	quadOffPxX := 0
	quadOffPxY := 0
	switch s.activeQuadrant {
	case 1:
		quadOffPxX = qSz
	case 2:
		quadOffPxY = qSz
	case 3:
		quadOffPxX = qSz
		quadOffPxY = qSz
	}

	// ── draw the quadrant scaled to previewSz×previewSz ──────────────────────
	src := rl.NewRectangle(float32(quadOffPxX), float32(quadOffPxY), float32(qSz), float32(qSz))
	dst := rl.NewRectangle(float32(previewX), float32(previewGridY), float32(previewSz), float32(previewSz))
	rl.DrawTexturePro(s.tex, src, dst, rl.NewVector2(0, 0), 0, rl.White)

	// ── tile grid overlay ─────────────────────────────────────────────────────
	tilesInQuad := qSz / s.tileSize
	gridCol := rl.NewColor(80, 130, 200, 160)
	for i := 0; i <= tilesInQuad; i++ {
		x := previewX + int32(float32(i*s.tileSize)*scaleF)
		rl.DrawLine(x, previewGridY, x, previewGridY+previewSz, gridCol)
		yy := previewGridY + int32(float32(i*s.tileSize)*scaleF)
		rl.DrawLine(previewX, yy, previewX+previewSz, yy, gridCol)
	}

	// helper: screen-space tile box at (col, row) within the quadrant
	tileBox := func(col, row int) (x, y, w, h int32) {
		x = previewX + int32(float32(col*s.tileSize)*scaleF)
		y = previewGridY + int32(float32(row*s.tileSize)*scaleF)
		w = int32(float32((col+1)*s.tileSize)*scaleF) - (x - previewX)
		h = int32(float32((row+1)*s.tileSize)*scaleF) - (y - previewGridY)
		return
	}

	// ── hover highlight ───────────────────────────────────────────────────────
	mouse := rl.GetMousePosition()
	mx := int32(mouse.X)
	my := int32(mouse.Y)
	if mx >= previewX && mx < previewX+previewSz && my >= previewGridY && my < previewGridY+previewSz {
		colInQuad := int(float32(mx-previewX)/scaleF) / s.tileSize
		rowInQuad := int(float32(my-previewGridY)/scaleF) / s.tileSize

		hx, hy, hw, hh := tileBox(colInQuad, rowInQuad)
		rl.DrawRectangleLines(hx, hy, hw, hh, rl.NewColor(255, 255, 100, 200))

		if rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
			s.tileX = quadOffPxX/s.tileSize + colInQuad
			s.tileY = quadOffPxY/s.tileSize + rowInQuad
		}
	}

	// ── selected tile highlight ───────────────────────────────────────────────
	quadTileOffX := quadOffPxX / s.tileSize
	quadTileOffY := quadOffPxY / s.tileSize
	localSelX := s.tileX - quadTileOffX
	localSelY := s.tileY - quadTileOffY
	if localSelX >= 0 && localSelX < tilesInQuad && localSelY >= 0 && localSelY < tilesInQuad {
		hlX, hlY, hlW, hlH := tileBox(localSelX, localSelY)
		rl.DrawRectangleLines(hlX-1, hlY-1, hlW+2, hlH+2, rl.White)
		rl.DrawRectangleLines(hlX, hlY, hlW, hlH, rl.NewColor(0, 0, 0, 180))
	}

	// ── border around the whole preview ──────────────────────────────────────
	rl.DrawRectangleLines(previewX-1, previewGridY-1, previewSz+2, previewSz+2,
		rl.NewColor(60, 60, 60, 255))
}

func drawStatusBar(s *gfxState) {
	y := virtualH - statusBarH
	rl.DrawRectangle(0, y, virtualW, statusBarH, rl.NewColor(30, 30, 30, 255))
	rl.DrawLine(0, y, virtualW, y, rl.NewColor(60, 60, 60, 255))

	gridStr := "off"
	if s.showGrid {
		gridStr = "on"
	}
	symStr := "off"
	if s.showSymmetry {
		symStr = "on"
	}
	status := fmt.Sprintf(
		"Sheet: %dpx  Tile: %dpx  #%d (%d,%d)/%d  |  Grid: %s [G]  |  Symmetry: %s [+]  |  Size: [S]",
		s.sheetSz, s.tileSize, s.tileIndex(), s.tileX, s.tileY, s.tileCount()-1,
		gridStr, symStr,
	)
	rl.DrawText(status, 4, y+5, 10, rl.LightGray)
}

// ── save ──────────────────────────────────────────────────────────────────────

// triggerSave saves immediately when a real path is known, otherwise opens
// the save-as dialog (only for the default "new.png" placeholder).
func triggerSave(s *gfxState) {
	if filepath.Base(s.imgPath) == "new.png" {
		// No explicit filename was given – ask for one.
		s.dialog.filename = ""
		s.dialog.mode = modeSaveAs
		s.dialog.active = true
		return
	}
	// Named path (existing or newly created) – save directly.
	if err := doSaveImage(s, s.imgPath); err != nil {
		fmt.Fprintln(os.Stderr, "save:", err)
	}
}

// confirmSave validates the typed name and writes the file.
func confirmSave(s *gfxState) {
	name := strings.TrimSpace(s.dialog.filename)
	if name == "" {
		return
	}
	if !strings.HasSuffix(strings.ToLower(name), ".png") {
		name += ".png"
	}
	if err := doSaveImage(s, filepath.Join("assets", "gfx", name)); err != nil {
		fmt.Fprintln(os.Stderr, "save:", err)
		return
	}
	s.dialog.active = false
	s.dialog.filename = ""
}

// doSaveImage exports the in-memory image to path and updates editor state.
func doSaveImage(s *gfxState, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if !rl.ExportImage(*s.img, path) {
		return fmt.Errorf("could not export %s", path)
	}
	s.imgPath = path
	rl.SetWindowTitle("fz gfx — " + filepath.Base(path))
	saveTileMeta(s)
	refreshFileList(s)
	return nil
}

// confirmNew creates a blank 256×256 sheet with the typed name and switches to it.
func confirmNew(s *gfxState) {
	name := strings.TrimSpace(s.dialog.filename)
	if name == "" {
		return
	}
	if !strings.HasSuffix(strings.ToLower(name), ".png") {
		name += ".png"
	}
	path := filepath.Join("assets", "gfx", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "new:", err)
		return
	}
	img := rl.GenImageColor(256, 256, rl.Blank)
	rl.ImageFormat(img, rl.UncompressedR8g8b8a8)
	if !rl.ExportImage(*img, path) {
		fmt.Fprintln(os.Stderr, "new: could not export", path)
		return
	}
	s.switchToImage(path, img)
	s.dialog.active = false
	s.dialog.filename = ""
	refreshFileList(s)
}

// switchToImage unloads the current image and texture and loads the given one.
func (s *gfxState) switchToImage(path string, img *rl.Image) {
	rl.UnloadImage(s.img)
	rl.UnloadTexture(s.tex)
	rl.ImageFormat(img, rl.UncompressedR8g8b8a8)
	s.img = img
	s.tex = rl.LoadTextureFromImage(img)
	rl.SetTextureFilter(s.tex, rl.FilterPoint)
	s.imgPath = path
	s.sheetSz = int(img.Width)
	s.tileX = 0
	s.tileY = 0
	s.clipboard = nil
	s.texDirty = false
	s.tileFlags = make(map[int]uint8)
	rl.SetWindowTitle("fz gfx — " + filepath.Base(path))
	loadTileMeta(s)
}

// loadGfxFile loads a PNG from assets/gfx by basename and switches to it.
func loadGfxFile(s *gfxState, name string) {
	path := filepath.Join("assets", "gfx", name)
	img := rl.LoadImage(path)
	if img == nil {
		return
	}
	s.switchToImage(path, img)
	refreshFileList(s) // re-sync fileActive to the newly current file
}

// refreshFileList scans assets/gfx for PNG files and rebuilds the dropdown text.
// It also updates fileActive to point at the currently open file.
func refreshFileList(s *gfxState) {
	dir := filepath.Join("assets", "gfx")
	entries, err := os.ReadDir(dir)
	if err != nil {
		s.fileList = nil
		s.fileText = "(empty)"
		s.fileActive = 0
		return
	}
	s.fileList = nil
	for _, e := range entries {
		if !e.IsDir() && strings.ToLower(filepath.Ext(e.Name())) == ".png" {
			s.fileList = append(s.fileList, e.Name())
		}
	}
	if len(s.fileList) == 0 {
		s.fileText = "(empty)"
		s.fileActive = 0
		return
	}
	s.fileText = strings.Join(s.fileList, ";")
	base := filepath.Base(s.imgPath)
	s.fileActive = 0
	for i, f := range s.fileList {
		if f == base {
			s.fileActive = int32(i)
			break
		}
	}
}

// drawSaveDialog renders a modal filename-input dialog when a save-as is needed.
func drawSaveDialog(s *gfxState) {
	if !s.dialog.active {
		return
	}

	// Dimmed backdrop.
	rl.DrawRectangle(0, 0, virtualW, virtualH, rl.NewColor(0, 0, 0, 160))

	const dw, dh = int32(300), int32(118)
	dx := (virtualW - dw) / 2
	dy := (virtualH - dh) / 2

	rl.DrawRectangle(dx, dy, dw, dh, rl.NewColor(40, 40, 46, 255))
	rl.DrawRectangleLines(dx, dy, dw, dh, rl.NewColor(90, 110, 170, 255))
	rl.DrawLine(dx, dy+22, dx+dw, dy+22, rl.NewColor(65, 75, 120, 255))
	rl.DrawText("Save As", dx+8, dy+6, 12, rl.NewColor(210, 210, 220, 255))

	// Text input area.
	ix, iy, iw := dx+8, dy+32, dw-16
	rl.DrawRectangle(ix, iy, iw, 24, rl.NewColor(22, 22, 28, 255))
	rl.DrawRectangleLines(ix, iy, iw, 24, rl.NewColor(80, 110, 200, 255))

	display := s.dialog.filename
	if int(rl.GetTime()*2)%2 == 0 {
		display += "_"
	}
	rl.DrawText(display, ix+5, iy+7, 10, rl.White)

	// Path preview below the input.
	hint := "assets/gfx/" + s.dialog.filename
	if s.dialog.filename != "" && !strings.HasSuffix(strings.ToLower(s.dialog.filename), ".png") {
		hint += ".png"
	}
	rl.DrawText(hint, ix, iy+28, 9, rl.NewColor(110, 110, 140, 255))

	// Buttons.
	bY := float32(dy+dh) - 28
	if raygui.Button(rl.NewRectangle(float32(dx+dw-136), bY, 62, 20), "Save") {
		confirmSave(s)
	}
	if raygui.Button(rl.NewRectangle(float32(dx+dw-68), bY, 62, 20), "Cancel") {
		s.dialog.active = false
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// closestPaletteColor returns the index of the palette entry nearest to c by
// squared Euclidean distance in RGB space.
func closestPaletteColor(c rl.Color) int {
	best, bestDist := 0, int(^uint(0)>>1)
	for i, p := range picotronPalette {
		dr := int(c.R) - int(p.R)
		dg := int(c.G) - int(p.G)
		db := int(c.B) - int(p.B)
		if d := dr*dr + dg*dg + db*db; d < bestDist {
			bestDist = d
			best = i
		}
	}
	return best
}

// ── toolbox ───────────────────────────────────────────────────────────────────

func drawToolbox(s *gfxState) {
	if s.dialog.active {
		return
	}
	const bsz = float32(toolboxBtnSz) // 24
	const step = bsz + 2              // 26
	y := float32(toolboxY)
	x := float32(drawAreaX)
	mouse := rl.GetMousePosition()

	// hoveredTip accumulates the tooltip for whatever button the mouse is over.
	var hoveredTip string
	tip := func(text string, bx float32) {
		if rl.CheckCollisionPointRec(mouse, rl.NewRectangle(bx, y, bsz, bsz)) {
			hoveredTip = text
		}
	}

	// ── Tool toggles ────────────────────────────────────────────────────────────
	pencilOn := s.activeTool == toolPencil
	bucketOn := s.activeTool == toolBucket
	eraserOn := s.activeTool == toolEraser

	raygui.Toggle(rl.NewRectangle(x, y, bsz, bsz), raygui.IconText(raygui.ICON_PENCIL, ""), &pencilOn)
	tip("Pencil (P)", x)
	x += step
	raygui.Toggle(rl.NewRectangle(x, y, bsz, bsz), raygui.IconText(raygui.ICON_COLOR_BUCKET, ""), &bucketOn)
	tip("Bucket (F)", x)
	x += step
	raygui.Toggle(rl.NewRectangle(x, y, bsz, bsz), raygui.IconText(raygui.ICON_RUBBER, ""), &eraserOn)
	tip("Eraser (E)", x)

	switch {
	case pencilOn && s.activeTool != toolPencil:
		s.activeTool = toolPencil
	case bucketOn && s.activeTool != toolBucket:
		s.activeTool = toolBucket
	case eraserOn && s.activeTool != toolEraser:
		s.activeTool = toolEraser
	}
	x += step + 8

	// ── Flip / Rotate ──────────────────────────────────────────────────────────
	if raygui.Button(rl.NewRectangle(x, y, bsz, bsz), raygui.IconText(raygui.ICON_SYMMETRY_HORIZONTAL, "")) {
		s.tileFlipH()
	}
	tip("Flip Horizontal (H)", x)
	x += step
	if raygui.Button(rl.NewRectangle(x, y, bsz, bsz), raygui.IconText(raygui.ICON_SYMMETRY_VERTICAL, "")) {
		s.tileFlipV()
	}
	tip("Flip Vertical (V)", x)
	x += step
	if raygui.Button(rl.NewRectangle(x, y, bsz, bsz), raygui.IconText(raygui.ICON_ROTATE_FILL, "")) {
		s.tileRotateCW()
	}
	tip("Rotate CW (R)", x)
	x += step
	// CCW button: plain button for interaction + manually drawn flipped icon.
	if raygui.Button(rl.NewRectangle(x, y, bsz, bsz), "") {
		s.tileRotateCCW()
	}
	tip("Rotate CCW (Alt+R)", x)
	// Overlay the pre-rendered ROTATE_FILL icon flipped horizontally.
	// Negative source width mirrors X; negative height corrects OpenGL's
	// render-texture Y inversion.
	tw := float32(s.iconCCW.Texture.Width)
	th := float32(s.iconCCW.Texture.Height)
	rl.DrawTexturePro(
		s.iconCCW.Texture,
		rl.NewRectangle(tw, th, -tw, -th),
		rl.NewRectangle(x+(bsz-tw)/2, y+(bsz-th)/2, tw, th),
		rl.Vector2Zero(), 0, rl.White,
	)
	x += step + 8

	// ── Clipboard ──────────────────────────────────────────────────────────────
	if raygui.Button(rl.NewRectangle(x, y, bsz, bsz), raygui.IconText(raygui.ICON_FILE_COPY, "")) {
		s.tileCopy()
	}
	tip("Copy (Ctrl+C)", x)
	x += step
	if raygui.Button(rl.NewRectangle(x, y, bsz, bsz), raygui.IconText(raygui.ICON_FILE_CUT, "")) {
		s.tileCut()
	}
	tip("Cut (Ctrl+X)", x)
	x += step
	if raygui.Button(rl.NewRectangle(x, y, bsz, bsz), raygui.IconText(raygui.ICON_FILE_PASTE, "")) {
		s.tilePaste()
	}
	tip("Paste (Ctrl+V)", x)
	x += step
	if raygui.Button(rl.NewRectangle(x, y, bsz, bsz), raygui.IconText(raygui.ICON_FILE_DELETE, "")) {
		s.tileClear()
	}
	tip("Delete (Ctrl+D)", x)

	// ── Draw tooltip ────────────────────────────────────────────────────────────
	if hoveredTip != "" {
		drawToolTip(hoveredTip, mouse.X, mouse.Y)
	}
}

func drawToolTip(text string, mx, my float32) {
	const pad = float32(4)
	const fontSize = int32(10)
	tw := float32(rl.MeasureText(text, fontSize)) + pad*2
	th := float32(fontSize) + pad*2
	tx := mx + 6
	ty := my - th - 4
	if tx+tw > float32(virtualW) {
		tx = float32(virtualW) - tw - 2
	}
	if ty < 0 {
		ty = my + 14
	}
	rl.DrawRectangle(int32(tx), int32(ty), int32(tw), int32(th), rl.NewColor(25, 25, 28, 230))
	rl.DrawRectangleLines(int32(tx), int32(ty), int32(tw), int32(th), rl.NewColor(90, 90, 100, 255))
	rl.DrawText(text, int32(tx+pad), int32(ty+pad), fontSize, rl.NewColor(220, 220, 220, 255))
}

// ── tile operations ───────────────────────────────────────────────────────────

// tileTransform applies an in-place pixel permutation to the current tile.
// fn(dstCol, dstRow, sz) returns the (srcCol, srcRow) that maps to that destination.
func (s *gfxState) tileTransform(fn func(col, row, sz int) (int, int)) {
	sz := s.tileSize
	ox, oy := s.tileX*sz, s.tileY*sz
	buf := make([]rl.Color, sz*sz)
	for row := range sz {
		for col := range sz {
			buf[row*sz+col] = rl.GetImageColor(*s.img, int32(ox+col), int32(oy+row))
		}
	}
	for row := range sz {
		for col := range sz {
			sc, sr := fn(col, row, sz)
			rl.ImageDrawPixel(s.img, int32(ox+col), int32(oy+row), buf[sr*sz+sc])
		}
	}
	s.texDirty = true
}

func (s *gfxState) tileFlipH() {
	s.tileTransform(func(col, row, sz int) (int, int) { return sz - 1 - col, row })
}

func (s *gfxState) tileFlipV() {
	s.tileTransform(func(col, row, sz int) (int, int) { return col, sz - 1 - row })
}

func (s *gfxState) tileRotateCW() {
	// dst(col,row) <- src(row, sz-1-col)
	s.tileTransform(func(col, row, sz int) (int, int) { return row, sz - 1 - col })
}

func (s *gfxState) tileRotateCCW() {
	// dst(col,row) <- src(sz-1-row, col)
	s.tileTransform(func(col, row, sz int) (int, int) { return sz - 1 - row, col })
}

// tileFill flood-fills the current tile from (startX, startY) with the selected color.
func (s *gfxState) tileFill(startX, startY int32) {
	target := rl.GetImageColor(*s.img, startX, startY)
	fill := picotronPalette[s.selectedColor]
	if target == fill {
		return
	}
	minX := int32(s.tileX * s.tileSize)
	minY := int32(s.tileY * s.tileSize)
	sz := int32(s.tileSize)
	type pt struct{ x, y int32 }
	queue := []pt{{startX, startY}}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if p.x < minX || p.x >= minX+sz || p.y < minY || p.y >= minY+sz {
			continue
		}
		if rl.GetImageColor(*s.img, p.x, p.y) != target {
			continue
		}
		rl.ImageDrawPixel(s.img, p.x, p.y, fill)
		queue = append(queue, pt{p.x - 1, p.y}, pt{p.x + 1, p.y}, pt{p.x, p.y - 1}, pt{p.x, p.y + 1})
	}
	s.texDirty = true
}

func (s *gfxState) tileCopy() {
	sz := s.tileSize
	ox, oy := s.tileX*sz, s.tileY*sz
	s.clipboard = make([]rl.Color, sz*sz)
	for row := range sz {
		for col := range sz {
			s.clipboard[row*sz+col] = rl.GetImageColor(*s.img, int32(ox+col), int32(oy+row))
		}
	}
}

func (s *gfxState) tileCut() {
	s.tileCopy()
	s.tileClear()
}

func (s *gfxState) tilePaste() {
	if len(s.clipboard) != s.tileSize*s.tileSize {
		return
	}
	sz := s.tileSize
	ox, oy := s.tileX*sz, s.tileY*sz
	for row := range sz {
		for col := range sz {
			rl.ImageDrawPixel(s.img, int32(ox+col), int32(oy+row), s.clipboard[row*sz+col])
		}
	}
	s.texDirty = true
}

func (s *gfxState) tileClear() {
	sz := s.tileSize
	ox, oy := s.tileX*sz, s.tileY*sz
	blank := rl.NewColor(0, 0, 0, 0)
	for row := range sz {
		for col := range sz {
			rl.ImageDrawPixel(s.img, int32(ox+col), int32(oy+row), blank)
		}
	}
	s.texDirty = true
}

func (s *gfxState) toggleTileFlag(bit int) {
	id := s.tileIndex()
	s.tileFlags[id] ^= 1 << uint(bit)
	if s.tileFlags[id] == 0 {
		delete(s.tileFlags, id)
	}
}

// virtualScale returns scale and offsets in physical/render-pixel space.
// Callers must convert to screen/logical space before passing to draw calls.
func virtualScale() (scale, offsetX, offsetY float32) {
	rw := float32(rl.GetRenderWidth())
	rh := float32(rl.GetRenderHeight())
	sx := rw / float32(virtualW)
	sy := rh / float32(virtualH)
	if sx < sy {
		scale = sx
	} else {
		scale = sy
	}
	offsetX = (rw - float32(virtualW)*scale) / 2
	offsetY = (rh - float32(virtualH)*scale) / 2
	return
}

// ── tile metadata (PNG tEXt chunk) ────────────────────────────────────────────

// tileMeta is serialised as JSON into a PNG tEXt chunk with key "fz_meta".
type tileMeta struct {
	TileSize int              `json:"tile_size"`
	Flags    map[string]uint8 `json:"flags,omitempty"` // tile-id string → 8-bit mask
}

const pngMetaKey = "fz_meta"

// pngChunk holds one parsed PNG chunk.
type pngChunk struct {
	typ string
	raw []byte // full encoded bytes: length(4)+type(4)+data(N)+crc(4)
	dat []byte // data slice into raw
}

func makePNGChunk(typ string, data []byte) []byte {
	out := make([]byte, 12+len(data))
	binary.BigEndian.PutUint32(out[0:], uint32(len(data)))
	copy(out[4:], typ)
	copy(out[8:], data)
	binary.BigEndian.PutUint32(out[8+len(data):], crc32.Checksum(out[4:8+len(data)], crc32.IEEETable))
	return out
}

func parsePNGChunks(buf []byte) ([]pngChunk, error) {
	if len(buf) < 8 || string(buf[:8]) != "\x89PNG\r\n\x1a\n" {
		return nil, fmt.Errorf("not a PNG file")
	}
	var chunks []pngChunk
	pos := 8
	for pos+12 <= len(buf) {
		n := int(binary.BigEndian.Uint32(buf[pos:]))
		if pos+12+n > len(buf) {
			break
		}
		c := pngChunk{
			typ: string(buf[pos+4 : pos+8]),
			raw: buf[pos : pos+12+n],
			dat: buf[pos+8 : pos+8+n],
		}
		chunks = append(chunks, c)
		pos += 12 + n
		if c.typ == "IEND" {
			break
		}
	}
	return chunks, nil
}

// isOurTextChunk returns true when c is the tEXt chunk with our metadata key.
func isOurTextChunk(c pngChunk) bool {
	if c.typ != "tEXt" {
		return false
	}
	for i, b := range c.dat {
		if b == 0 {
			return string(c.dat[:i]) == pngMetaKey
		}
	}
	return false
}

func readPNGMeta(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	chunks, err := parsePNGChunks(raw)
	if err != nil {
		return "", err
	}
	for _, c := range chunks {
		if !isOurTextChunk(c) {
			continue
		}
		for i, b := range c.dat {
			if b == 0 {
				return string(c.dat[i+1:]), nil
			}
		}
	}
	return "", nil
}

func writePNGMeta(path, jsonStr string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	chunks, err := parsePNGChunks(raw)
	if err != nil {
		return err
	}
	out := []byte("\x89PNG\r\n\x1a\n")
	for _, c := range chunks {
		if isOurTextChunk(c) {
			continue // drop old metadata chunk
		}
		if c.typ == "IEND" {
			// Insert fresh metadata chunk before IEND.
			cd := append([]byte(pngMetaKey+"\x00"), []byte(jsonStr)...)
			out = append(out, makePNGChunk("tEXt", cd)...)
		}
		out = append(out, c.raw...)
	}
	return os.WriteFile(path, out, 0o644)
}

func loadTileMeta(s *gfxState) {
	jsonStr, err := readPNGMeta(s.imgPath)
	if err != nil || jsonStr == "" {
		return
	}
	var meta tileMeta
	if err := json.Unmarshal([]byte(jsonStr), &meta); err != nil {
		return
	}
	for _, sz := range tileSizeCycle {
		if meta.TileSize == sz {
			s.tileSize = sz
			break
		}
	}
	for k, v := range meta.Flags {
		if id, err := strconv.Atoi(k); err == nil && v != 0 {
			s.tileFlags[id] = v
		}
	}
}

func saveTileMeta(s *gfxState) {
	flags := make(map[string]uint8)
	for id, v := range s.tileFlags {
		if v != 0 {
			flags[strconv.Itoa(id)] = v
		}
	}
	var flagField map[string]uint8
	if len(flags) > 0 {
		flagField = flags
	}
	j, err := json.Marshal(tileMeta{TileSize: s.tileSize, Flags: flagField})
	if err != nil {
		fmt.Fprintln(os.Stderr, "meta:", err)
		return
	}
	if err := writePNGMeta(s.imgPath, string(j)); err != nil {
		fmt.Fprintln(os.Stderr, "meta:", err)
	}
}

// ── tile flags UI ─────────────────────────────────────────────────────────────

func drawTileFlags(s *gfxState) {
	if s.dialog.active {
		return
	}
	const cbSz = float32(14)
	const step = float32(34)
	y := float32(flagsRowY)
	x := float32(drawAreaX)

	rl.DrawText("FLAGS:", int32(x), int32(y)+2, 10, rl.NewColor(160, 160, 160, 255))
	x += 50

	tileID := s.tileIndex()
	flags := s.tileFlags[tileID]

	for bit := 0; bit < 8; bit++ {
		checked := (flags>>uint(bit))&1 == 1
		prev := checked
		raygui.CheckBox(rl.NewRectangle(x, y, cbSz, cbSz), strconv.Itoa(bit), &checked)
		if checked != prev {
			if checked {
				s.tileFlags[tileID] |= 1 << uint(bit)
			} else {
				s.tileFlags[tileID] &^= 1 << uint(bit)
				if s.tileFlags[tileID] == 0 {
					delete(s.tileFlags, tileID)
				}
			}
		}
		x += step
	}

	current := s.tileFlags[tileID]
	rl.DrawText(fmt.Sprintf("= 0x%02X", current),
		int32(x)+4, int32(y)+2, 10, rl.NewColor(130, 180, 130, 255))
}

// ── resolve image path ────────────────────────────────────────────────────────

func resolveGfxImage(filename string) (path string, img *rl.Image, err error) {
	if filename == "" {
		return filepath.Join("assets", "gfx", "new.png"),
			rl.GenImageColor(256, 256, rl.Blank),
			nil
	}
	// Auto-add .png when no extension is given.
	if filepath.Ext(filename) == "" {
		filename += ".png"
	}
	path = filepath.Join("assets", "gfx", filename)
	statErr := func() error { _, e := os.Stat(path); return e }()
	if os.IsNotExist(statErr) {
		// Named file doesn't exist yet – start with a blank canvas.
		return path, rl.GenImageColor(256, 256, rl.Blank), nil
	}
	if statErr != nil {
		return path, nil, statErr
	}
	img = rl.LoadImage(path)
	if img == nil {
		return path, nil, fmt.Errorf("could not load image: %s", path)
	}
	return path, img, nil
}

// ── help overlay ──────────────────────────────────────────────────────────────

func drawHelpPage(s *gfxState) {
	if !s.showHelp {
		return
	}
	rl.DrawRectangle(0, 0, virtualW, virtualH, rl.NewColor(0, 0, 0, 195))

	const pw, ph = int32(560), int32(268)
	px := (virtualW - pw) / 2
	py := (virtualH - ph) / 2

	rl.DrawRectangle(px, py, pw, ph, rl.NewColor(22, 22, 30, 255))
	rl.DrawRectangleLines(px, py, pw, ph, rl.NewColor(80, 100, 165, 255))
	rl.DrawLine(px, py+22, px+pw, py+22, rl.NewColor(55, 65, 110, 255))
	rl.DrawLine(px+pw/2, py+28, px+pw/2, py+ph-6, rl.NewColor(50, 60, 100, 255))

	title := "Keyboard Shortcuts  —  F1 or Esc to close"
	tw := rl.MeasureText(title, 11)
	rl.DrawText(title, px+(pw-tw)/2, py+5, 11, rl.NewColor(190, 205, 240, 255))

	type row struct{ key, desc string }
	left := []row{
		{"Ctrl+S", "Save"},
		{"F1", "Toggle this help"},
		{},
		{"P", "Pencil tool"},
		{"F", "Bucket fill"},
		{"E", "Eraser"},
		{},
		{"H", "Flip horizontal"},
		{"V", "Flip vertical"},
		{"R", "Rotate CW"},
		{"Alt+R", "Rotate CCW"},
	}
	right := []row{
		{"Ctrl+C", "Copy tile"},
		{"Ctrl+X", "Cut tile"},
		{"Ctrl+V", "Paste tile"},
		{"Ctrl+D", "Clear tile"},
		{},
		{"G", "Toggle grid"},
		{"+", "Toggle symmetry"},
		{"S", "Cycle tile size"},
		{},
		{"Alt+1..4", "Switch sheet page"},
		{"0..7", "Toggle flag bit"},
	}

	kCol := rl.NewColor(215, 225, 100, 255)
	dCol := rl.NewColor(175, 175, 188, 255)

	drawRows := func(rows []row, startX, startY int32) {
		cy := startY
		for _, r := range rows {
			if r.key == "" {
				cy += 7
				continue
			}
			rl.DrawText(r.key, startX, cy, 10, kCol)
			rl.DrawText(r.desc, startX+86, cy, 10, dCol)
			cy += 15
		}
	}

	drawRows(left, px+12, py+30)
	drawRows(right, px+pw/2+12, py+30)
}
