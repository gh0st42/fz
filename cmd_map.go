package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gen2brain/raylib-go/raygui"
	rl "github.com/gen2brain/raylib-go/raylib"
)

// ── layout constants ──────────────────────────────────────────────────────────

const (
	// Right panel — tools
	mapRToolsLabelY  = drawAreaY            // 32
	mapRTools1Y      = drawAreaY + 14       // 46  pencil/eraser/fill
	mapRTools2Y      = drawAreaY + 42       // 74  flip/rotate + grid
	mapRToolsSepY    = drawAreaY + 74       // 106

	// Right panel — tileset (no "TILES" label; tabs sit right after the dropdown)
	mapRTilesetLblY  = mapRToolsSepY + 4    // 110
	mapRTilesetDropY = mapRToolsSepY + 16   // 122  (drawn last for z-order)
	mapSheetTabY     = mapRTilesetDropY + 26 // 148
	mapSheetTabH     = int32(18)
	mapSheetGridY    = mapSheetTabY + mapSheetTabH + 2 // 168  → 168+256=424, gap=36 to statusBar

	// Below drawing area — layers
	mapLayerRowH    = int32(20)
	mapBelowLayersY = drawAreaY + drawAreaSz + 4       // 356
	mapBelowListH   = int32(76)
	mapBelowBtnsY   = mapBelowLayersY + mapBelowListH + 2 // 434
	mapBelowBtnH    = int32(20)                            // 434+20=454 → 6px gap to statusBar
)

// ── minimap constants ─────────────────────────────────────────────────────────

const (
	mmSize   = int32(128) // minimap render area in virtual pixels
	mmBorder = int32(4)   // draggable border around the render area
)

// ── undo / redo ───────────────────────────────────────────────────────────────

// mapSnapshot captures tile-layer data for all layers at a point in time.
// Object layers and layer structure (names, count) are not included.
type mapSnapshot struct {
	mapW, mapH int
	data       [][]uint32 // one entry per layer; nil for object layers
}

// ── layer kind ────────────────────────────────────────────────────────────────

type layerKind int

const (
	layerKindTile   layerKind = iota
	layerKindObject           // name starts with "_"
)

func kindFromName(name string) layerKind {
	if strings.HasPrefix(name, "_") {
		return layerKindObject
	}
	return layerKindTile
}

// ── Tiled GID flip flags ──────────────────────────────────────────────────────

const (
	gidFlipH    = uint32(0x80000000)
	gidFlipV    = uint32(0x40000000)
	gidFlipD    = uint32(0x20000000) // diagonal / transpose
	gidFlagMask = gidFlipH | gidFlipV | gidFlipD
)

// gidFlagTable[rotation][flipHBit][flipVBit] → Tiled flip bits.
// Encodes the composed transform: apply flipH/flipV first, then rotate by rotation×90°CW.
var gidFlagTable = [4][2][2]uint32{
	{{0x00000000, 0x40000000}, {0x80000000, 0xC0000000}}, // rot=0
	{{0xA0000000, 0x20000000}, {0xE0000000, 0x60000000}}, // rot=1  90°CW
	{{0xC0000000, 0x80000000}, {0x40000000, 0x00000000}}, // rot=2 180°
	{{0x60000000, 0xE0000000}, {0x20000000, 0xA0000000}}, // rot=3 270°CW
}

func tileGIDFlags(flipH, flipV bool, rotation int32) uint32 {
	fh, fv := 0, 0
	if flipH {
		fh = 1
	}
	if flipV {
		fv = 1
	}
	return gidFlagTable[rotation&3][fh][fv]
}

type tileRenderParams struct{ rotation float32; flipH, flipV bool }

// tileRenderTable maps the 3-bit (D,H,V) Tiled flag index to DrawTexturePro params.
// Index: (D?4:0)|(H?2:0)|(V?1:0)
var tileRenderTable = [8]tileRenderParams{
	{0, false, false},   // D=0 H=0 V=0 – identity
	{0, false, true},    // D=0 H=0 V=1 – flipV
	{0, true, false},    // D=0 H=1 V=0 – flipH
	{180, false, false}, // D=0 H=1 V=1 – 180°
	{90, false, true},   // D=1 H=0 V=0 – transpose (90°CW + flipV in src)
	{270, false, false}, // D=1 H=0 V=1 – 270°CW
	{90, false, false},  // D=1 H=1 V=0 – 90°CW
	{90, true, false},   // D=1 H=1 V=1 – 90°CW + flipH in src
}

func gidToRenderParams(gidFlags uint32) tileRenderParams {
	idx := 0
	if gidFlags&gidFlipD != 0 {
		idx |= 4
	}
	if gidFlags&gidFlipH != 0 {
		idx |= 2
	}
	if gidFlags&gidFlipV != 0 {
		idx |= 1
	}
	return tileRenderTable[idx]
}

// drawTileTransformed draws tile index ti from tex into dst, with optional
// horizontal/vertical source flip and a CW rotation (in degrees: 0/90/180/270).
// Origin is set to the center of dst so the tile stays within its cell on rotation.
func drawTileTransformed(tex rl.Texture2D, ti, columns, tileSize int, dst rl.Rectangle, flipH, flipV bool, rotation float32, tint rl.Color) {
	if tex.ID == 0 || columns <= 0 || tileSize <= 0 || ti < 0 {
		return
	}
	col := ti % columns
	row := ti / columns
	srcX := float32(col * tileSize)
	srcY := float32(row * tileSize)
	srcW := float32(tileSize)
	srcH := float32(tileSize)
	if flipH {
		srcX += srcW
		srcW = -srcW
	}
	if flipV {
		srcY += srcH
		srcH = -srcH
	}
	// Move dest to its own centre so the origin offset cancels for rotation=0
	// and all rotations stay within the original dst bounds.
	centred := rl.NewRectangle(dst.X+dst.Width/2, dst.Y+dst.Height/2, dst.Width, dst.Height)
	origin := rl.NewVector2(dst.Width/2, dst.Height/2)
	rl.DrawTexturePro(tex, rl.NewRectangle(srcX, srcY, srcW, srcH), centred, origin, rotation, tint)
}

// ── state ─────────────────────────────────────────────────────────────────────

type mapLayer struct {
	name    string
	visible bool
	class   string
	kind    layerKind
	data    []uint32    // tile GIDs (len = mapW*mapH); nil for object layers
	objects []mapObject // rect objects; nil for tile layers
}

type mapResizeDialog struct {
	active bool
	wText  string
	hText  string
	focusW bool
}

type mapState struct {
	mapW, mapH int
	tileSize   int
	zoom       int
	mapPath    string

	scrollX, scrollY int
	showGrid         bool
	activeTool       drawTool

	layers      []mapLayer
	activeLayer int
	renaming     bool
	renameText   string
	classEditing bool
	classEditText string

	// Pre-rendered CCW rotation icon (ROTATE_FILL flipped horizontally), same as gfx editor.
	iconCCW rl.RenderTexture2D

	// Active tileset
	sheetImg        *rl.Image
	sheetTex        rl.Texture2D
	sheetSz         int    // image width in pixels
	sheetColumns    int    // columns from TSJ (= imageWidth/tileWidth when spacing=0)
	tilesetFirstGID int    // firstgid of the loaded tileset (usually 1)
	tilesetTSJPath  string // path to .tsj (used in TMJ source field)
	tilesetImgPath  string // path to the PNG image
	tilesetName     string
	selectedTile    int
	activeQuadrant  int
	tileFlipH       bool
	tileFlipV       bool
	tileRotation    int32 // 0=0°, 1=90°CW, 2=180°, 3=270°CW

	// Tileset dropdown (assets/gfx)
	sheetList     []string
	sheetText     string
	sheetActive   int32
	sheetDropEdit bool
	prevDropEdit  bool

	// Map file dropdown (assets/maps)
	mapList         []string
	mapText         string
	mapActive       int32
	mapDropEdit     bool
	prevMapDropEdit bool
	pendingMapPath  string // set by dropdown, consumed in handleMapInput (before BeginTextureMode)
	pendingSheetName string // set by tileset dropdown, consumed in handleMapInput

	// Save-as dialog
	saveActive   bool
	saveFilename string

	// Viewport hover
	hoverX, hoverY int
	hoverValid     bool

	// Layer list scroll offset
	layerScroll int

	// Scroll-key repeat timer (shared for all directions)
	scrollKeyTimer float64

	resize   mapResizeDialog
	showHelp bool

	// Object layer editing
	objListScroll int
	selectedObj   int  // index in active layer's objects, -1 = none
	objRenaming   bool
	objRenameText string
	objIDEditing  bool
	objIDText     string
	lastClickTime float64
	lastClickType int // 1=name area 2=id area (for double-click detection)

	// Object drag-to-draw state
	objDragActive     bool
	objDragX0, objDragY0 int
	objDragX1, objDragY1 int

	// Scrollbar drag state
	layerSbDrag    bool
	layerSbDragOff float32
	objSbDrag      bool
	objSbDragOff   float32
	vSbDrag        bool
	vSbDragOff     float32
	hSbDrag        bool
	hSbDragOff     float32
	// Layer double-click rename
	layerLastClickTime float64

	dirty       bool
	wantQuit    bool
	focusMode   bool // Tab: hide all UI except status bar, use full viewport
	toast       Toast
	exitConfirm ConfirmDialog
	undoStack   []mapSnapshot
	redoStack   []mapSnapshot

	// Raw top-level TMJ fields loaded from disk; nil for new (unsaved) maps.
	// Used by saveMapTMJ to preserve unknown fields (properties, backgroundcolor, etc.).
	tmjBase map[string]json.RawMessage

	// Minimap
	showMinimap    bool
	minimapX       int32
	minimapY       int32
	minimapTex     rl.RenderTexture2D
	minimapDirty   bool
	minimapDrag    bool
	minimapDragOX  float32
	minimapDragOY  float32
}

// vpRect returns the map viewport's (x, y, width, height) in virtual pixels.
// In focus mode the viewport fills the screen above the status bar; otherwise
// it is the fixed drawing area.
func (s *mapState) vpRect() (x, y, w, h int32) {
	if s.focusMode {
		return 0, 0, virtualW, virtualH - statusBarH
	}
	return drawAreaX, drawAreaY, drawAreaSz, drawAreaSz
}

func (s *mapState) quadrantSz() int { return s.sheetSz / 2 }

func (s *mapState) isSbDragging() bool {
	return s.layerSbDrag || s.objSbDrag || s.vSbDrag || s.hSbDrag
}

func (s *mapState) mapWindowTitle() string {
	if s.mapPath != "" {
		return "fz map — " + filepath.Base(s.mapPath)
	}
	return "fz map"
}

func (s *mapState) markDirty() {
	s.minimapDirty = true
	if !s.dirty {
		s.dirty = true
		rl.SetWindowTitle(s.mapWindowTitle() + " *")
	}
}

func (s *mapState) markClean() {
	s.dirty = false
	rl.SetWindowTitle(s.mapWindowTitle())
}

func (s *mapState) takeMapSnapshot() mapSnapshot {
	snap := mapSnapshot{mapW: s.mapW, mapH: s.mapH, data: make([][]uint32, len(s.layers))}
	for i, l := range s.layers {
		if l.kind == layerKindTile && len(l.data) > 0 {
			d := make([]uint32, len(l.data))
			copy(d, l.data)
			snap.data[i] = d
		}
	}
	return snap
}

func (s *mapState) applyMapSnapshot(snap mapSnapshot) {
	s.mapW = snap.mapW
	s.mapH = snap.mapH
	for i, d := range snap.data {
		if i >= len(s.layers) || d == nil {
			continue
		}
		if s.layers[i].kind == layerKindTile {
			s.layers[i].data = make([]uint32, len(d))
			copy(s.layers[i].data, d)
		}
	}
	s.clampScroll()
}

func (s *mapState) pushMapUndo() {
	s.undoStack = append(s.undoStack, s.takeMapSnapshot())
	if len(s.undoStack) > maxUndoSteps {
		s.undoStack = s.undoStack[1:]
	}
	s.redoStack = s.redoStack[:0]
}

func (s *mapState) mapUndo() {
	if len(s.undoStack) == 0 {
		return
	}
	s.redoStack = append(s.redoStack, s.takeMapSnapshot())
	if len(s.redoStack) > maxUndoSteps {
		s.redoStack = s.redoStack[1:]
	}
	snap := s.undoStack[len(s.undoStack)-1]
	s.undoStack = s.undoStack[:len(s.undoStack)-1]
	s.applyMapSnapshot(snap)
	s.markDirty()
}

func (s *mapState) mapRedo() {
	if len(s.redoStack) == 0 {
		return
	}
	s.undoStack = append(s.undoStack, s.takeMapSnapshot())
	if len(s.undoStack) > maxUndoSteps {
		s.undoStack = s.undoStack[1:]
	}
	snap := s.redoStack[len(s.redoStack)-1]
	s.redoStack = s.redoStack[:len(s.redoStack)-1]
	s.applyMapSnapshot(snap)
	s.markDirty()
}

func (s *mapState) tileSheetLayout() (px, sz, scale int32) {
	qSz := int32(s.quadrantSz())
	if qSz <= 0 {
		return panelX, panelW - 4, 1
	}
	maxW := panelW - 4
	scale = maxW / qSz
	if scale < 1 {
		scale = 1
	}
	sz = qSz * scale
	if sz > maxW {
		sz = maxW
	}
	px = panelX + (panelW-sz)/2
	return
}

func (s *mapState) clampScroll() {
	if s.tileSize <= 0 || s.zoom <= 0 {
		return
	}
	cellSz := s.tileSize * s.zoom
	_, _, vpW, vpH := s.vpRect()
	visW := int(vpW) / cellSz
	visH := int(vpH) / cellSz
	maxX := s.mapW - visW
	maxY := s.mapH - visH
	if maxX < 0 {
		maxX = 0
	}
	if maxY < 0 {
		maxY = 0
	}
	if s.scrollX < 0 {
		s.scrollX = 0
	}
	if s.scrollX > maxX {
		s.scrollX = maxX
	}
	if s.scrollY < 0 {
		s.scrollY = 0
	}
	if s.scrollY > maxY {
		s.scrollY = maxY
	}
}

func (s *mapState) clampLayerScroll() {
	visRows := int(mapBelowListH) / int(mapLayerRowH)
	max := len(s.layers) - visRows
	if max < 0 {
		max = 0
	}
	if s.layerScroll < 0 {
		s.layerScroll = 0
	}
	if s.layerScroll > max {
		s.layerScroll = max
	}
}

func (s *mapState) ensureLayerVisible() {
	visRows := int(mapBelowListH) / int(mapLayerRowH)
	if s.activeLayer < s.layerScroll {
		s.layerScroll = s.activeLayer
	}
	if s.activeLayer >= s.layerScroll+visRows {
		s.layerScroll = s.activeLayer - visRows + 1
	}
}

// ── tileset I/O ───────────────────────────────────────────────────────────────

func refreshMapSheetList(s *mapState) {
	dir := filepath.Join("assets", "gfx")
	entries, err := os.ReadDir(dir)
	if err != nil {
		s.sheetList = nil
		s.sheetText = "(empty)"
		return
	}

	// First pass: collect base names that have a .tsj so we can suppress the
	// paired .png from the list (they represent the same tileset; .tsj is canonical).
	hasTSJ := make(map[string]bool)
	for _, e := range entries {
		if !e.IsDir() && strings.ToLower(filepath.Ext(e.Name())) == ".tsj" {
			base := strings.ToLower(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
			hasTSJ[base] = true
		}
	}

	s.sheetList = nil
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext == ".tsj" {
			s.sheetList = append(s.sheetList, e.Name())
		} else if ext == ".png" {
			base := strings.ToLower(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
			if !hasTSJ[base] {
				s.sheetList = append(s.sheetList, e.Name())
			}
		}
	}
	if len(s.sheetList) == 0 {
		s.sheetText = "(empty)"
		return
	}
	s.sheetText = strings.Join(s.sheetList, ";")
}

func loadMapSheetFromEntry(s *mapState, name string) {
	ext := strings.ToLower(filepath.Ext(name))
	var imgPath string

	var tsjColumns int
	if ext == ".tsj" {
		tsjPath := filepath.Join("assets", "gfx", name)
		ts, err := loadTSJFile(tsjPath)
		if err != nil {
			return
		}
		imgPath = filepath.Join(filepath.Dir(tsjPath), filepath.FromSlash(ts.Image))
		s.tilesetTSJPath = tsjPath
		s.tilesetImgPath = imgPath
		s.tilesetName = ts.Name
		tsjColumns = ts.Columns
		for _, sz := range tileSizeCycle {
			if ts.TileWidth == sz {
				s.tileSize = sz
				break
			}
		}
	} else {
		imgPath = filepath.Join("assets", "gfx", name)
		base := strings.TrimSuffix(name, filepath.Ext(name))
		s.tilesetTSJPath = filepath.Join("assets", "gfx", base+".tsj")
		s.tilesetImgPath = imgPath
		s.tilesetName = base
		if jsonStr, err := readPNGMeta(imgPath); err == nil && jsonStr != "" {
			var meta tileMeta
			if json.Unmarshal([]byte(jsonStr), &meta) == nil {
				for _, sz := range tileSizeCycle {
					if meta.TileSize == sz {
						s.tileSize = sz
						break
					}
				}
			}
		}
	}

	img := rl.LoadImage(imgPath)
	if img == nil || img.Width == 0 {
		return
	}
	rl.ImageFormat(img, rl.UncompressedR8g8b8a8)
	if s.sheetTex.ID > 0 {
		rl.UnloadTexture(s.sheetTex)
	}
	if s.sheetImg != nil {
		rl.UnloadImage(s.sheetImg)
	}
	s.sheetImg = img
	s.sheetTex = rl.LoadTextureFromImage(img)
	rl.SetTextureFilter(s.sheetTex, rl.FilterPoint)
	s.sheetSz = int(img.Width)
	if tsjColumns > 0 {
		s.sheetColumns = tsjColumns
	} else if s.tileSize > 0 {
		s.sheetColumns = int(img.Width) / s.tileSize
	}
	s.tilesetFirstGID = 1
	s.selectedTile = 0
	s.activeQuadrant = 0
	s.minimapDirty = true
}

// ── map I/O ───────────────────────────────────────────────────────────────────

func defaultLayers(w, h int) []mapLayer {
	sz := w * h
	// Index 0 = topmost visual layer (drawn last); last index = bottommost (drawn first).
	// Draw order in drawMapTileLayers iterates slice in reverse.
	return []mapLayer{
		{name: "Above", visible: true, class: "top", kind: layerKindTile, data: make([]uint32, sz)},
		{name: "Foreground", visible: true, class: "bottom", kind: layerKindTile, data: make([]uint32, sz)},
		{name: "Background", visible: true, class: "bottom", kind: layerKindTile, data: make([]uint32, sz)},
		{name: "_Events", visible: true, kind: layerKindObject, objects: []mapObject{}},
		{name: "_Encounters", visible: true, kind: layerKindObject, objects: []mapObject{}},
	}
}

func refreshMapFileList(s *mapState) {
	dir := filepath.Join("assets", "maps")
	entries, err := os.ReadDir(dir)
	if err != nil {
		s.mapList = nil
		s.mapText = "(empty)"
		return
	}
	s.mapList = nil
	for _, e := range entries {
		if !e.IsDir() && strings.ToLower(filepath.Ext(e.Name())) == ".tmj" {
			s.mapList = append(s.mapList, e.Name())
		}
	}
	if len(s.mapList) == 0 {
		s.mapText = "(empty)"
		return
	}
	s.mapText = strings.Join(s.mapList, ";")
	base := filepath.Base(s.mapPath)
	for i, f := range s.mapList {
		if f == base {
			s.mapActive = int32(i)
			break
		}
	}
}

func loadMapTMJ(s *mapState, path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var tm tiledMapJSON
	if err := json.Unmarshal(raw, &tm); err != nil {
		return err
	}
	// Capture all raw top-level fields so saveMapTMJ can preserve unknown ones.
	var tmjBase map[string]json.RawMessage
	json.Unmarshal(raw, &tmjBase)
	s.tmjBase = tmjBase

	s.mapW = tm.Width
	s.mapH = tm.Height
	s.tileSize = tm.TileWidth
	if s.tileSize <= 0 {
		s.tileSize = 16
	}
	s.mapPath = path

	// TODO: only the first tileset is loaded. Maps that reference multiple tilesets
	// will have GIDs from the second+ tilesets resolved against the first sheet,
	// producing wrong tiles. Fix requires iterating tm.Tilesets in GID order and
	// picking the correct sheet per-GID at draw time.
	s.tilesetFirstGID = 1
	if len(tm.Tilesets) > 0 {
		tsRef := tm.Tilesets[0]
		s.tilesetFirstGID = tsRef.FirstGID
		if s.tilesetFirstGID <= 0 {
			s.tilesetFirstGID = 1
		}
		relSrc := filepath.FromSlash(tsRef.Source)
		tsjPath := filepath.Join(filepath.Dir(path), relSrc)
		if ts, err := loadTSJFile(tsjPath); err == nil {
			imgPath := filepath.Join(filepath.Dir(tsjPath), filepath.FromSlash(ts.Image))
			if img := rl.LoadImage(imgPath); img != nil && img.Width > 0 {
				rl.ImageFormat(img, rl.UncompressedR8g8b8a8)
				if s.sheetTex.ID > 0 {
					rl.UnloadTexture(s.sheetTex)
				}
				if s.sheetImg != nil {
					rl.UnloadImage(s.sheetImg)
				}
				s.sheetImg = img
				s.sheetTex = rl.LoadTextureFromImage(img)
				rl.SetTextureFilter(s.sheetTex, rl.FilterPoint)
				s.sheetSz = int(img.Width)
				if ts.Columns > 0 {
					s.sheetColumns = ts.Columns
				} else if s.tileSize > 0 {
					s.sheetColumns = int(img.Width) / s.tileSize
				}
				// Prefer TSJ tilewidth if TMJ didn't set it
				if s.tileSize <= 0 && ts.TileWidth > 0 {
					s.tileSize = ts.TileWidth
				}
				s.tilesetTSJPath = tsjPath
				s.tilesetImgPath = imgPath
				s.tilesetName = ts.Name
			}
		}
	}

	sz := s.mapW * s.mapH
	s.layers = nil
	for _, jl := range tm.Layers {
		layer := mapLayer{name: jl.Name, visible: jl.Visible, class: jl.Class}
		switch jl.Type {
		case "tilelayer":
			layer.kind = layerKindTile
			if len(jl.Data) == sz {
				layer.data = jl.Data
			} else {
				layer.data = make([]uint32, sz)
			}
		case "objectgroup":
			layer.kind = layerKindObject
			if jl.Objects != nil {
				layer.objects = *jl.Objects
			} else {
				layer.objects = []mapObject{}
			}
		default:
			layer.kind = layerKindTile
			layer.data = make([]uint32, sz)
		}
		s.layers = append(s.layers, layer)
	}
	if len(s.layers) == 0 {
		s.layers = defaultLayers(s.mapW, s.mapH)
	} else {
		// TMJ stores layers bottom-up (Background first); reverse to our top-down convention.
		for i, j := 0, len(s.layers)-1; i < j; i, j = i+1, j-1 {
			s.layers[i], s.layers[j] = s.layers[j], s.layers[i]
		}
	}

	s.activeLayer = 0
	s.layerScroll = 0
	s.selectedTile = 0
	s.activeQuadrant = 0
	s.scrollX = 0
	s.scrollY = 0
	s.clampScroll()
	refreshMapFileList(s)
	syncSheetActive(s)
	s.dirty = false
	s.minimapDirty = true
	s.undoStack = s.undoStack[:0]
	s.redoStack = s.redoStack[:0]
	rl.SetWindowTitle("fz map — " + filepath.Base(path))
	return nil
}

// syncSheetActive updates sheetActive to point to the currently loaded tileset
// in the sheet dropdown list, matching by TSJ filename then image filename.
func syncSheetActive(s *mapState) {
	if s.tilesetTSJPath != "" {
		tsjBase := filepath.Base(s.tilesetTSJPath)
		for i, name := range s.sheetList {
			if name == tsjBase {
				s.sheetActive = int32(i)
				return
			}
		}
	}
	if s.tilesetImgPath != "" {
		imgBase := filepath.Base(s.tilesetImgPath)
		for i, name := range s.sheetList {
			if name == imgBase {
				s.sheetActive = int32(i)
				return
			}
		}
	}
}

// saveMapTMJ serialises the current map to disk, preserving all fields in the
// original TMJ and TSJ that the internal editors do not manage.
func saveMapTMJ(s *mapState, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	// ── TSJ: merge flag properties into existing tileset file ─────────────────
	if s.tilesetTSJPath != "" && s.sheetSz > 0 && s.tileSize > 0 {
		_ = saveTSJMerged(s)
	}

	// ── TMJ: build a raw-map starting from the original so unknown fields survive ──

	outMap := make(map[string]json.RawMessage, len(s.tmjBase)+16)
	for k, v := range s.tmjBase {
		outMap[k] = v
	}

	set := func(key string, val any) {
		raw, _ := json.Marshal(val)
		outMap[key] = json.RawMessage(raw)
	}

	set("compressionlevel", -1)
	set("height", s.mapH)
	set("width", s.mapW)
	set("infinite", false)
	set("orientation", "orthogonal")
	set("renderorder", "right-down")
	set("tiledversion", "1.10.2")
	set("tileheight", s.tileSize)
	set("tilewidth", s.tileSize)
	set("type", "map")
	set("version", "1.10")

	maxObjID := 0
	for _, l := range s.layers {
		for _, obj := range l.objects {
			if obj.ID > maxObjID {
				maxObjID = obj.ID
			}
		}
	}
	set("nextlayerid", len(s.layers)+1)
	set("nextobjectid", maxObjID+1)

	if s.tilesetTSJPath != "" {
		relSrc, err := filepath.Rel(filepath.Dir(path), s.tilesetTSJPath)
		if err != nil {
			relSrc = filepath.Base(s.tilesetTSJPath)
		}
		set("tilesets", []tiledTilesetRefJSON{{FirstGID: 1, Source: filepath.ToSlash(relSrc)}})
	} else if _, ok := outMap["tilesets"]; !ok {
		set("tilesets", []tiledTilesetRefJSON{})
	}

	// Index original layer raw maps by name for field preservation.
	origLayerByName := make(map[string]map[string]json.RawMessage)
	if lb, ok := outMap["layers"]; ok {
		var arr []json.RawMessage
		if json.Unmarshal(lb, &arr) == nil {
			for _, item := range arr {
				var lm map[string]json.RawMessage
				if json.Unmarshal(item, &lm) == nil {
					var name string
					if nb, ok2 := lm["name"]; ok2 {
						json.Unmarshal(nb, &name)
					}
					if name != "" {
						origLayerByName[name] = lm
					}
				}
			}
		}
	}

	// Emit layers reversed (bottom-up) to match Tiled's file convention.
	var layerMaps []map[string]json.RawMessage
	for ri := len(s.layers) - 1; ri >= 0; ri-- {
		layer := s.layers[ri]

		lm := make(map[string]json.RawMessage)
		for k, v := range origLayerByName[layer.name] {
			lm[k] = v
		}

		lset := func(key string, val any) {
			raw, _ := json.Marshal(val)
			lm[key] = json.RawMessage(raw)
		}

		lset("id", len(s.layers)-ri)
		lset("name", layer.name)
		lset("visible", layer.visible)
		if layer.class != "" {
			lset("class", layer.class)
		} else {
			delete(lm, "class")
		}
		lset("x", 0)
		lset("y", 0)
		if _, exists := lm["opacity"]; !exists {
			lset("opacity", 1.0)
		}

		switch layer.kind {
		case layerKindTile:
			lset("type", "tilelayer")
			lset("width", s.mapW)
			lset("height", s.mapH)
			data := layer.data
			if len(data) != s.mapW*s.mapH {
				data = make([]uint32, s.mapW*s.mapH)
			}
			lset("data", data)
			delete(lm, "objects")
			delete(lm, "draworder")
		case layerKindObject:
			lset("type", "objectgroup")
			if _, exists := lm["draworder"]; !exists {
				lset("draworder", "topdown")
			}
			objs := layer.objects
			if objs == nil {
				objs = []mapObject{}
			}
			lset("objects", objs)
			delete(lm, "data")
			delete(lm, "width")
			delete(lm, "height")
		}

		layerMaps = append(layerMaps, lm)
	}

	layersBytes, _ := json.Marshal(layerMaps)
	outMap["layers"] = json.RawMessage(layersBytes)

	out, err := json.MarshalIndent(outMap, "", "  ")
	if err != nil {
		return err
	}
	out = reformatTMJTileData(out, s.mapW)
	return os.WriteFile(path, out, 0o644)
}

// saveTSJMerged writes the TSJ, merging computed flag properties from the PNG
// meta into the existing file so that animations, custom properties, and other
// fields Tiled may have added are preserved.
func saveTSJMerged(s *mapState) error {
	cols := s.sheetSz / s.tileSize

	// Load existing TSJ as a raw map so all unknown fields are round-tripped.
	tsjMap := make(map[string]json.RawMessage)
	if raw, err := os.ReadFile(s.tilesetTSJPath); err == nil {
		json.Unmarshal(raw, &tsjMap)
	}

	set := func(key string, val any) {
		raw, _ := json.Marshal(val)
		tsjMap[key] = json.RawMessage(raw)
	}

	set("columns", cols)
	imgRef := filepath.Base(s.tilesetImgPath)
	if rel, err := filepath.Rel(filepath.Dir(s.tilesetTSJPath), s.tilesetImgPath); err == nil {
		imgRef = filepath.ToSlash(rel)
	}
	set("image", imgRef)
	set("imagewidth", s.sheetSz)
	set("imageheight", s.sheetSz)
	set("tilecount", cols*cols)
	set("tileheight", s.tileSize)
	set("tilewidth", s.tileSize)
	set("type", "tileset")
	set("version", "1.10")
	set("tiledversion", "1.10.2")
	if _, ok := tsjMap["name"]; !ok {
		set("name", s.tilesetName)
	}

	// Decode existing tile entries keyed by id.
	existingTiles := make(map[int]map[string]json.RawMessage)
	if tilesBytes, ok := tsjMap["tiles"]; ok {
		var arr []json.RawMessage
		if json.Unmarshal(tilesBytes, &arr) == nil {
			for _, item := range arr {
				var tm map[string]json.RawMessage
				if json.Unmarshal(item, &tm) == nil {
					var id int
					if ib, ok2 := tm["id"]; ok2 {
						json.Unmarshal(ib, &id)
					}
					existingTiles[id] = tm
				}
			}
		}
	}

	// Compute flag_N properties from the PNG fz_meta chunk.
	newFlagProps := make(map[int][]tiledProperty)
	if s.tilesetImgPath != "" {
		if metaJSON, err := readPNGMeta(s.tilesetImgPath); err == nil && metaJSON != "" {
			var meta tileMeta
			if json.Unmarshal([]byte(metaJSON), &meta) == nil {
				for idStr, flags := range meta.Flags {
					if flags == 0 {
						continue
					}
					tileID, err := strconv.Atoi(idStr)
					if err != nil {
						continue
					}
					var props []tiledProperty
					for bit := 0; bit < 8; bit++ {
						if flags&(1<<uint(bit)) != 0 {
							boolVal, _ := json.Marshal(true)
							props = append(props, tiledProperty{
								Name:  fmt.Sprintf("flag_%d", bit),
								Type:  "bool",
								Value: json.RawMessage(boolVal),
							})
						}
					}
					newFlagProps[tileID] = props
				}
			}
		}
	}

	// Collect IDs that need an entry: tiles with flags OR tiles from the existing
	// TSJ that have non-flag properties or extra fields (animations, images, etc.).
	tileIDSet := make(map[int]bool)
	for id := range newFlagProps {
		tileIDSet[id] = true
	}
	for id, tm := range existingTiles {
		for k := range tm {
			if k != "id" && k != "properties" {
				tileIDSet[id] = true
				break
			}
		}
		if pb, ok := tm["properties"]; ok {
			var props []tiledProperty
			if json.Unmarshal(pb, &props) == nil {
				for _, p := range props {
					if !strings.HasPrefix(p.Name, "flag_") {
						tileIDSet[id] = true
						break
					}
				}
			}
		}
	}

	var tileIDs []int
	for id := range tileIDSet {
		tileIDs = append(tileIDs, id)
	}
	sort.Ints(tileIDs)

	var tilesOut []map[string]json.RawMessage
	for _, id := range tileIDs {
		tm := make(map[string]json.RawMessage)
		for k, v := range existingTiles[id] {
			tm[k] = v
		}
		idRaw, _ := json.Marshal(id)
		tm["id"] = json.RawMessage(idRaw)

		// Keep non-flag_N properties; replace all flag_N with fresh values.
		var keepProps []tiledProperty
		if pb, ok := tm["properties"]; ok {
			var existing []tiledProperty
			if json.Unmarshal(pb, &existing) == nil {
				for _, p := range existing {
					if !strings.HasPrefix(p.Name, "flag_") {
						keepProps = append(keepProps, p)
					}
				}
			}
		}
		merged := append(keepProps, newFlagProps[id]...)
		if len(merged) > 0 {
			pb, _ := json.Marshal(merged)
			tm["properties"] = json.RawMessage(pb)
		} else {
			delete(tm, "properties")
		}

		tilesOut = append(tilesOut, tm)
	}

	if len(tilesOut) > 0 {
		tilesBytes, _ := json.Marshal(tilesOut)
		tsjMap["tiles"] = json.RawMessage(tilesBytes)
	} else {
		delete(tsjMap, "tiles")
	}

	raw, err := json.MarshalIndent(tsjMap, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.tilesetTSJPath, raw, 0o644)
}

// ── layer helpers ─────────────────────────────────────────────────────────────

func (s *mapState) layerAdd() {
	name := fmt.Sprintf("layer %d", len(s.layers)+1)
	kind := kindFromName(name)
	l := mapLayer{name: name, visible: true, kind: kind}
	if kind == layerKindTile {
		l.data = make([]uint32, s.mapW*s.mapH)
	} else {
		l.objects = []mapObject{}
	}
	s.layers = append(s.layers, l)
	s.activeLayer = len(s.layers) - 1
	s.ensureLayerVisible()
	s.markDirty()
}

func (s *mapState) layerDelete() {
	if len(s.layers) <= 1 {
		return
	}
	s.layers = append(s.layers[:s.activeLayer], s.layers[s.activeLayer+1:]...)
	if s.activeLayer >= len(s.layers) {
		s.activeLayer = len(s.layers) - 1
	}
	s.clampLayerScroll()
	s.ensureLayerVisible()
	s.markDirty()
}

func (s *mapState) layerMoveUp() {
	if s.activeLayer <= 0 {
		return
	}
	s.layers[s.activeLayer], s.layers[s.activeLayer-1] = s.layers[s.activeLayer-1], s.layers[s.activeLayer]
	s.activeLayer--
	s.ensureLayerVisible()
	s.markDirty()
}

func (s *mapState) layerMoveDown() {
	if s.activeLayer >= len(s.layers)-1 {
		return
	}
	s.layers[s.activeLayer], s.layers[s.activeLayer+1] = s.layers[s.activeLayer+1], s.layers[s.activeLayer]
	s.activeLayer++
	s.ensureLayerVisible()
	s.markDirty()
}

func (s *mapState) layerRenameStart() {
	if s.activeLayer < len(s.layers) {
		s.renameText = s.layers[s.activeLayer].name
		s.renaming = true
		s.classEditing = false
	}
}

func (s *mapState) layerClassEditStart() {
	if s.activeLayer < len(s.layers) {
		s.classEditText = s.layers[s.activeLayer].class
		s.classEditing = true
		s.renaming = false
	}
}

func (s *mapState) layerClassEditConfirm() {
	if s.activeLayer < len(s.layers) {
		s.layers[s.activeLayer].class = strings.TrimSpace(s.classEditText)
		s.markDirty()
	}
	s.classEditing = false
}

func (s *mapState) layerRenameConfirm() {
	name := strings.TrimSpace(s.renameText)
	if name == "" {
		name = fmt.Sprintf("layer %d", s.activeLayer+1)
	}
	l := &s.layers[s.activeLayer]
	l.name = name
	newKind := kindFromName(name)
	if l.kind != newKind {
		l.kind = newKind
		if newKind == layerKindTile {
			l.data = make([]uint32, s.mapW*s.mapH)
			l.objects = nil
		} else {
			l.data = nil
			l.objects = []mapObject{}
		}
	}
	s.renaming = false
	s.markDirty()
}

// ── minimap ───────────────────────────────────────────────────────────────────

// renderMinimap redraws s.minimapTex from the current tile layers.
// Must be called OUTSIDE BeginTextureMode(canvas) since it uses its own texture target.
func renderMinimap(s *mapState) {
	if s.minimapTex.ID == 0 {
		return
	}
	rl.BeginTextureMode(s.minimapTex)
	rl.ClearBackground(rl.NewColor(18, 18, 24, 255))

	if s.mapW > 0 && s.mapH > 0 && s.sheetTex.ID > 0 && s.sheetColumns > 0 && s.tileSize > 0 {
		tileW := float32(mmSize) / float32(s.mapW)
		tileH := float32(mmSize) / float32(s.mapH)

		// Draw layers bottom-up (index len-1 first) to match viewport order
		for li := len(s.layers) - 1; li >= 0; li-- {
			layer := &s.layers[li]
			if !layer.visible || layer.kind != layerKindTile || len(layer.data) == 0 {
				continue
			}
			for row := 0; row < s.mapH; row++ {
				for col := 0; col < s.mapW; col++ {
					gid := layer.data[row*s.mapW+col]
					rawID := gid &^ gidFlagMask
					if rawID == 0 {
						continue
					}
					ti := int(rawID) - s.tilesetFirstGID
					if ti < 0 {
						continue
					}
					p := gidToRenderParams(gid & gidFlagMask)
					dst := rl.NewRectangle(
						float32(col)*tileW,
						float32(row)*tileH,
						tileW,
						tileH,
					)
					drawTileTransformed(s.sheetTex, ti, s.sheetColumns, s.tileSize, dst,
						p.flipH, p.flipV, p.rotation, rl.White)
				}
			}
		}
	}
	rl.EndTextureMode()
	s.minimapDirty = false
}

// mouseOverMinimap reports whether the current mouse position is anywhere inside
// the minimap window (border + content). Used to suppress input in draw functions
// that check rl.IsMouseButtonPressed directly (bypassing raygui.Lock).
func (s *mapState) mouseOverMinimap() bool {
	if !s.showMinimap {
		return false
	}
	m := rl.GetMousePosition()
	return m.X >= float32(s.minimapX) && m.X < float32(s.minimapX+mmSize+2*mmBorder) &&
		m.Y >= float32(s.minimapY) && m.Y < float32(s.minimapY+mmSize+2*mmBorder)
}

// minimapRect returns the bounding rectangle of the full minimap window
// (border + content) in virtual pixels.
func (s *mapState) minimapRect() rl.Rectangle {
	return rl.NewRectangle(
		float32(s.minimapX),
		float32(s.minimapY),
		float32(mmSize+2*mmBorder),
		float32(mmSize+2*mmBorder),
	)
}

// handleMinimapInput processes minimap drag. Returns true if the minimap
// consumed the mouse event so other hit-tests should be skipped.
func handleMinimapInput(s *mapState) bool {
	if !s.showMinimap {
		return false
	}
	mouse := rl.GetMousePosition()
	mmW := float32(mmSize + 2*mmBorder)
	mmH := float32(mmSize + 2*mmBorder)
	mx, my := mouse.X, mouse.Y

	inOuter := mx >= float32(s.minimapX) && mx < float32(s.minimapX)+mmW &&
		my >= float32(s.minimapY) && my < float32(s.minimapY)+mmH
	inInner := mx >= float32(s.minimapX+mmBorder) && mx < float32(s.minimapX+mmBorder+mmSize) &&
		my >= float32(s.minimapY+mmBorder) && my < float32(s.minimapY+mmBorder+mmSize)
	onBorder := inOuter && !inInner

	if s.minimapDrag {
		if rl.IsMouseButtonDown(rl.MouseButtonLeft) {
			newX := int32(mx - s.minimapDragOX)
			newY := int32(my - s.minimapDragOY)
			// Clamp inside virtual screen
			maxX := virtualW - mmSize - 2*mmBorder
			maxY := virtualH - mmSize - 2*mmBorder
			if newX < 0 {
				newX = 0
			}
			if newX > maxX {
				newX = maxX
			}
			if newY < 0 {
				newY = 0
			}
			if newY > maxY {
				newY = maxY
			}
			s.minimapX = newX
			s.minimapY = newY
			return true
		}
		s.minimapDrag = false
	}

	if onBorder && rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
		s.minimapDrag = true
		s.minimapDragOX = mx - float32(s.minimapX)
		s.minimapDragOY = my - float32(s.minimapY)
		return true
	}

	// Click or drag on the content area: center viewport on the clicked map position
	if inInner && rl.IsMouseButtonDown(rl.MouseButtonLeft) && s.mapW > 0 && s.mapH > 0 && s.tileSize > 0 && s.zoom > 0 {
		relX := mx - float32(s.minimapX+mmBorder)
		relY := my - float32(s.minimapY+mmBorder)
		tileCol := int(relX / float32(mmSize) * float32(s.mapW))
		tileRow := int(relY / float32(mmSize) * float32(s.mapH))
		_, _, vpW, vpH := s.vpRect()
		cellSz := s.tileSize * s.zoom
		visW := int(vpW) / cellSz
		visH := int(vpH) / cellSz
		s.scrollX = tileCol - visW/2
		s.scrollY = tileRow - visH/2
		s.clampScroll()
		return true
	}

	return inOuter // block tile interaction when cursor is anywhere over the minimap
}

// drawMinimap renders the cached minimap texture plus the viewport frame overlay.
// Called inside BeginTextureMode(canvas) after all other UI, always (not gated by focusMode).
func drawMinimap(s *mapState) {
	if !s.showMinimap || s.minimapTex.ID == 0 {
		return
	}
	mx := s.minimapX
	my := s.minimapY
	sz := mmSize
	b := mmBorder

	borderCol := rl.NewColor(90, 110, 160, 255)
	if s.minimapDrag {
		borderCol = rl.NewColor(130, 160, 220, 255)
	}

	// Background border
	rl.DrawRectangle(mx, my, sz+2*b, sz+2*b, rl.NewColor(20, 22, 32, 255))
	rl.DrawRectangleLines(mx, my, sz+2*b, sz+2*b, borderCol)

	// Minimap texture (RenderTexture Y is flipped)
	src := rl.NewRectangle(0, 0, float32(sz), -float32(sz))
	dst := rl.NewRectangle(float32(mx+b), float32(my+b), float32(sz), float32(sz))
	rl.DrawTexturePro(s.minimapTex.Texture, src, dst, rl.NewVector2(0, 0), 0, rl.White)

	// Viewport frame
	if s.mapW > 0 && s.mapH > 0 && s.tileSize > 0 && s.zoom > 0 {
		_, _, vpW, vpH := s.vpRect()
		cellSz := s.tileSize * s.zoom
		visW := float32(int(vpW)/cellSz) / float32(s.mapW) * float32(sz)
		visH := float32(int(vpH)/cellSz) / float32(s.mapH) * float32(sz)
		frameX := float32(mx+b) + float32(s.scrollX)/float32(s.mapW)*float32(sz)
		frameY := float32(my+b) + float32(s.scrollY)/float32(s.mapH)*float32(sz)
		if visW < 1 {
			visW = 1
		}
		if visH < 1 {
			visH = 1
		}
		// Clamp frame within minimap area
		contentRight := float32(mx + b + sz)
		contentBottom := float32(my + b + sz)
		if frameX+visW > contentRight {
			visW = contentRight - frameX
		}
		if frameY+visH > contentBottom {
			visH = contentBottom - frameY
		}
		rl.DrawRectangleLinesEx(
			rl.NewRectangle(frameX, frameY, visW, visH),
			1,
			rl.NewColor(255, 220, 50, 220),
		)
	}
}

// ── entry point ───────────────────────────────────────────────────────────────

func runMap(args []string) error {
	rl.SetConfigFlags(rl.FlagWindowResizable | rl.FlagWindowHighdpi)
	rl.InitWindow(virtualW*2, virtualH*2, "fz map")
	rl.SetTargetFPS(60)
	defer rl.CloseWindow()

	iconCCW := rl.LoadRenderTexture(16, 16)
	rl.BeginTextureMode(iconCCW)
	rl.ClearBackground(rl.NewColor(0, 0, 0, 0))
	raygui.DrawIcon(raygui.ICON_ROTATE_FILL, 0, 0, 1, rl.White)
	rl.EndTextureMode()
	defer rl.UnloadRenderTexture(iconCCW)

	state := &mapState{
		mapW: 32, mapH: 32, tileSize: 16, zoom: 2,
		showGrid: true, activeTool: toolPencil,
		selectedObj: -1,
		minimapX: virtualW - mmSize - 2*mmBorder - 4,
		minimapY: toolbarH + 4,
		iconCCW:  iconCCW,
	}
	state.layers = defaultLayers(state.mapW, state.mapH)
	refreshMapSheetList(state)
	refreshMapFileList(state)

	if len(args) > 0 {
		p := args[0]
		// Bare filename → look in assets/maps/
		if !filepath.IsAbs(p) && filepath.Dir(p) == "." {
			p = filepath.Join("assets", "maps", p)
		}
		_ = loadMapTMJ(state, p)
	} else {
		if len(state.mapList) > 0 {
			_ = loadMapTMJ(state, filepath.Join("assets", "maps", state.mapList[0]))
		}
		if state.sheetTex.ID == 0 && len(state.sheetList) > 0 {
			loadMapSheetFromEntry(state, state.sheetList[0])
		}
	}

	defer func() {
		if state.sheetTex.ID > 0 {
			rl.UnloadTexture(state.sheetTex)
		}
		if state.sheetImg != nil {
			rl.UnloadImage(state.sheetImg)
		}
	}()

	canvas := rl.LoadRenderTexture(virtualW, virtualH)
	defer rl.UnloadRenderTexture(canvas)

	state.minimapTex = rl.LoadRenderTexture(mmSize, mmSize)
	defer rl.UnloadRenderTexture(state.minimapTex)
	state.minimapDirty = true

	rl.SetExitKey(0)
	running := true
	osClosed := false

	for running {
		if rl.WindowShouldClose() && !osClosed {
			osClosed = true
			if state.dirty {
				state.exitConfirm.Show("Unsaved changes — quit anyway?", "Quit", "Cancel")
			} else {
				running = false
			}
		}

		scale, offsetX, offsetY := virtualScale()
		dpi := float32(rl.GetRenderWidth()) / float32(rl.GetScreenWidth())
		ss := scale / dpi
		sox := offsetX / dpi
		soy := offsetY / dpi
		rl.SetMouseOffset(int(-sox), int(-soy))
		rl.SetMouseScale(1/ss, 1/ss)

		handleMapInput(state, float64(rl.GetFrameTime()))

		if state.wantQuit {
			running = false
		}

		if state.pendingMapPath != "" {
			if err := loadMapTMJ(state, state.pendingMapPath); err == nil {
				state.toast.Notify("Loaded " + filepath.Base(state.pendingMapPath))
			}
			state.pendingMapPath = ""
		}
		if state.pendingSheetName != "" {
			loadMapSheetFromEntry(state, state.pendingSheetName)
			state.toast.Notify("Tileset: " + state.pendingSheetName)
			state.pendingSheetName = ""
		}

		if state.showMinimap && state.minimapDirty {
			renderMinimap(state)
		}

		rl.BeginTextureMode(canvas)
		drawMapScene(state)
		rl.EndTextureMode()

		if state.wantQuit {
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

// ── scene ─────────────────────────────────────────────────────────────────────

func drawMapScene(s *mapState) {
	rl.ClearBackground(rl.NewColor(45, 45, 48, 255))

	// Lock raygui controls so clicks on underlying UI don't register while the
	// minimap is visible and the cursor is over it.
	if s.showMinimap {
		m := rl.GetMousePosition()
		if m.X >= float32(s.minimapX) && m.X < float32(s.minimapX+mmSize+2*mmBorder) &&
			m.Y >= float32(s.minimapY) && m.Y < float32(s.minimapY+mmSize+2*mmBorder) {
			raygui.Lock()
			defer raygui.Unlock()
		}
	}

	if !s.focusMode {
		drawMapToolbar(s)
	}
	drawMapViewport(s)
	if !s.focusMode {
		drawMapBelowLayers(s)
		drawMapRightPanel(s)
	}
	drawMapStatusBar(s)
	drawMapResizeDialog(s)
	drawMapSaveDialog(s)
	if !s.focusMode {
		drawMapFileDropdown(s)    // last — z-order above everything
		drawMapTilesetDropdown(s) // last — z-order above everything
	}
	drawMinimap(s)
	s.toast.Draw()
	if s.showHelp {
		drawMapHelpOverlay()
	}
	if s.exitConfirm.Draw() {
		s.wantQuit = true
	}
}

func drawMapHelpOverlay() {
	const (
		dw = int32(460)
		dh = int32(352)
	)
	dx := (virtualW - dw) / 2
	dy := (virtualH - dh) / 2

	rl.DrawRectangle(0, 0, virtualW, virtualH, rl.NewColor(0, 0, 0, 170))
	rl.DrawRectangle(dx, dy, dw, dh, rl.NewColor(30, 32, 42, 255))
	rl.DrawRectangleLines(dx, dy, dw, dh, rl.NewColor(80, 110, 170, 255))

	titleCol := rl.NewColor(180, 200, 230, 255)
	headCol := rl.NewColor(130, 160, 210, 255)
	keyCol := rl.NewColor(220, 220, 120, 255)
	valCol := rl.NewColor(190, 190, 200, 255)
	dimCol := rl.NewColor(110, 110, 130, 255)

	tw := rl.MeasureText("fz map — keyboard shortcuts", 11)
	rl.DrawText("fz map — keyboard shortcuts", dx+(dw-tw)/2, dy+8, 11, titleCol)
	rl.DrawLine(dx+8, dy+24, dx+dw-8, dy+24, rl.NewColor(60, 70, 100, 255))

	type row struct{ key, val string }
	type section struct {
		title string
		rows  []row
	}

	left := []section{
		{"Navigation", []row{
			{"WASD / Arrows", "Scroll map"},
			{"Mouse wheel", "Scroll vertical"},
			{"Shift+Wheel", "Scroll horizontal"},
			{"+  /  -", "Zoom in / out"},
			{"Ctrl+Wheel", "Zoom in / out"},
		}},
		{"Tools (tile layer)", []row{
			{"P", "Pencil"},
			{"F", "Fill / bucket"},
			{"E", "Eraser"},
			{"Left drag", "Draw object rect"},
		}},
		{"Tile transforms", []row{
			{"H", "Flip horizontal"},
			{"V", "Flip vertical"},
			{"R / Shift+R", "Rotate CW / CCW"},
			{"Right-click", "Pick tile from layer"},
		}},
	}
	right := []section{
		{"Map", []row{
			{"Ctrl+S", "Save"},
			{"Ctrl+Z", "Undo"},
			{"Ctrl+Y / Ctrl+Shift+Z", "Redo"},
			{"G", "Toggle grid"},
			{"M", "Toggle minimap"},
			{"Tab", "Toggle focus mode"},
			{"F1 / Esc", "Close this help"},
		}},
		{"Layers", []row{
			{"Page Up / Down", "Cycle active layer"},
			{"Space", "Toggle active layer visibility"},
			{"Dbl-click name", "Rename layer"},
		}},
		{"Objects (object layer)", []row{
			{"Drag on map", "Draw rect object"},
			{"Dbl-click name", "Rename object"},
			{"Dbl-click #id", "Edit object ID"},
		}},
		{"Tileset panel", []row{
			{"Click tile", "Select tile"},
			{"Scroll wheel", "Scroll tileset"},
		}},
	}

	drawCol := func(sections []section, cx, cy int32) {
		x := cx
		y := cy
		for _, sec := range sections {
			rl.DrawText(sec.title, x, y, 9, headCol)
			y += 13
			for _, r := range sec.rows {
				kw := rl.MeasureText(r.key, 8)
				rl.DrawText(r.key, x+80-kw, y, 8, keyCol)
				rl.DrawText(r.val, x+86, y, 8, valCol)
				y += 12
			}
			y += 5
		}
	}

	colY := dy + 32
	drawCol(left, dx+10, colY)
	drawCol(right, dx+dw/2+10, colY)

	rl.DrawText("F1 or Esc to close", dx+(dw-rl.MeasureText("F1 or Esc to close", 9))/2, dy+dh-14, 9, dimCol)
}

// ── toolbar ───────────────────────────────────────────────────────────────────

func drawMapToolbar(s *mapState) {
	rl.DrawRectangle(0, 0, virtualW, toolbarH, rl.NewColor(30, 30, 30, 255))
	rl.DrawLine(0, toolbarH, virtualW, toolbarH, rl.NewColor(60, 60, 60, 255))

	blocked := s.resize.active || s.saveActive || s.sheetDropEdit || s.mapDropEdit
	if blocked {
		raygui.SetState(raygui.STATE_DISABLED)
	}

	if raygui.Button(rl.NewRectangle(4, 4, 44, 20), "Save") && !blocked {
		if s.mapPath != "" {
			if err := saveMapTMJ(s, s.mapPath); err == nil {
				s.toast.Notify("Saved " + filepath.Base(s.mapPath))
				s.markClean()
			}
			refreshMapFileList(s)
		} else {
			s.saveActive = true
			s.saveFilename = ""
		}
	}
	if raygui.Button(rl.NewRectangle(52, 4, 36, 20), "New") && !blocked {
		s.mapPath = ""
		s.mapW, s.mapH = 32, 32
		s.layers = defaultLayers(s.mapW, s.mapH)
		s.activeLayer = 0
		s.scrollX, s.scrollY = 0, 0
		s.dirty = false
		s.undoStack = s.undoStack[:0]
		s.redoStack = s.redoStack[:0]
		s.tmjBase = nil
		s.minimapDirty = true
		rl.SetWindowTitle("fz map")
	}
	if raygui.Button(rl.NewRectangle(256, 4, 50, 20), "Resize") && !blocked {
		s.resize.active = true
		s.resize.wText = strconv.Itoa(s.mapW)
		s.resize.hText = strconv.Itoa(s.mapH)
		s.resize.focusW = true
	}

	// Grid toggle — right-aligned in toolbar
	raygui.CheckBox(rl.NewRectangle(float32(virtualW-52), 7, 14, 14), "Grid", &s.showGrid)

	if blocked {
		raygui.SetState(raygui.STATE_NORMAL)
	}
}

// ── map viewport ──────────────────────────────────────────────────────────────

func drawMapViewport(s *mapState) {
	vx, vy, vw, vh := s.vpRect()
	rl.DrawRectangle(vx, vy, vw, vh, rl.NewColor(18, 18, 24, 255))
	drawMapTileLayers(s)
	drawMapObjectLayers(s)

	cellSz := int32(0)
	if s.tileSize > 0 && s.zoom > 0 {
		cellSz = int32(s.tileSize * s.zoom)
	}

	if s.showGrid && cellSz > 0 {
		gc := rl.NewColor(50, 60, 80, 180)
		for x := int32(0); x <= vw; x += cellSz {
			rl.DrawLine(vx+x, vy, vx+x, vy+vh, gc)
		}
		for y := int32(0); y <= vh; y += cellSz {
			rl.DrawLine(vx, vy+y, vx+vw, vy+y, gc)
		}
	}

	// Hover tile highlight + coordinate tracking
	s.hoverValid = false
	if cellSz > 0 {
		mouse := rl.GetMousePosition()
		mx, my := mouse.X, mouse.Y
		overMinimap := s.showMinimap &&
			mx >= float32(s.minimapX) && mx < float32(s.minimapX+mmSize+2*mmBorder) &&
			my >= float32(s.minimapY) && my < float32(s.minimapY+mmSize+2*mmBorder)
		if !overMinimap && mx >= float32(vx) && mx < float32(vx+vw) &&
			my >= float32(vy) && my < float32(vy+vh) {
			col := int((mx - float32(vx)) / float32(cellSz))
			row := int((my - float32(vy)) / float32(cellSz))
			s.hoverX = s.scrollX + col
			s.hoverY = s.scrollY + row
			s.hoverValid = true
			hx := vx + int32(col)*cellSz
			hy := vy + int32(row)*cellSz
			// Ghost tile preview for pencil and bucket tools
			if (s.activeTool == toolPencil || s.activeTool == toolBucket) && s.sheetTex.ID > 0 && s.sheetColumns > 0 {
				if s.activeLayer < len(s.layers) && s.layers[s.activeLayer].kind == layerKindTile {
					dst := rl.NewRectangle(float32(hx), float32(hy), float32(cellSz), float32(cellSz))
					drawTileTransformed(s.sheetTex, s.selectedTile, s.sheetColumns, s.tileSize, dst,
						s.tileFlipH, s.tileFlipV, float32(s.tileRotation)*90, rl.NewColor(255, 255, 255, 160))
				}
			}
			rl.DrawRectangleLines(hx, hy, cellSz, cellSz, rl.NewColor(255, 255, 100, 180))
		}
	}

	if !s.focusMode {
		rl.DrawRectangleLines(vx, vy, vw, vh, rl.NewColor(80, 80, 80, 255))
	}
	drawMapViewportScrollbars(s)
}

func drawMapViewportScrollbars(s *mapState) {
	if s.tileSize <= 0 || s.zoom <= 0 {
		return
	}
	cellSz := s.tileSize * s.zoom
	vx, vy, vw, vh := s.vpRect()
	visW := int(vw) / cellSz
	visH := int(vh) / cellSz

	trackCol := rl.NewColor(35, 35, 48, 255)
	thumbCol := rl.NewColor(85, 110, 160, 255)
	mouse := rl.GetMousePosition()

	// Vertical scrollbar: right of viewport gap in normal mode, inside right edge in focus mode.
	if s.mapH > visH {
		var tx, ty, th, sbW int32
		if s.focusMode {
			sbW = 4
			tx = vx + vw - sbW
			ty = vy
			th = vh
		} else {
			sbW = 4
			tx = vx + vw + 1
			ty = vy
			th = vh
		}
		maxSc := int32(s.mapH - visH)
		thumbH := int32(th) * int32(visH) / int32(s.mapH)
		if thumbH < 8 {
			thumbH = 8
		}
		if s.vSbDrag {
			if rl.IsMouseButtonDown(rl.MouseButtonLeft) {
				newY := mouse.Y - s.vSbDragOff
				if newY < float32(ty) {
					newY = float32(ty)
				}
				if maxY := float32(ty + th - thumbH); newY > maxY {
					newY = maxY
				}
				if th-thumbH > 0 {
					s.scrollY = int(float32(maxSc) * (newY - float32(ty)) / float32(th-thumbH))
				}
				s.clampScroll()
			} else {
				s.vSbDrag = false
			}
		}
		thumbY := ty + int32(s.scrollY)*(int32(th)-thumbH)/maxSc
		hitR := rl.NewRectangle(float32(tx)-2, float32(thumbY), float32(sbW)+4, float32(thumbH))
		if !s.vSbDrag && !s.mouseOverMinimap() && rl.IsMouseButtonPressed(rl.MouseButtonLeft) && rl.CheckCollisionPointRec(mouse, hitR) {
			s.vSbDrag = true
			s.vSbDragOff = mouse.Y - float32(thumbY)
		}
		rl.DrawRectangle(tx, ty, sbW, th, trackCol)
		rl.DrawRectangle(tx, thumbY, sbW, thumbH, thumbCol)
	}

	// Horizontal scrollbar: below viewport gap in normal mode, inside bottom edge in focus mode.
	if s.mapW > visW {
		var tx, ty, tw, sbH int32
		if s.focusMode {
			sbH = 3
			tx = vx
			ty = vy + vh - sbH
			tw = vw
		} else {
			sbH = 3
			ty = vy + vh + 1
			tx = vx
			tw = vw
		}
		maxSc := int32(s.mapW - visW)
		thumbW := int32(tw) * int32(visW) / int32(s.mapW)
		if thumbW < 8 {
			thumbW = 8
		}
		if s.hSbDrag {
			if rl.IsMouseButtonDown(rl.MouseButtonLeft) {
				newX := mouse.X - s.hSbDragOff
				if newX < float32(tx) {
					newX = float32(tx)
				}
				if maxX := float32(tx + tw - thumbW); newX > maxX {
					newX = maxX
				}
				if tw-thumbW > 0 {
					s.scrollX = int(float32(maxSc) * (newX - float32(tx)) / float32(tw-thumbW))
				}
				s.clampScroll()
			} else {
				s.hSbDrag = false
			}
		}
		thumbX := tx + int32(s.scrollX)*(int32(tw)-thumbW)/maxSc
		hitR := rl.NewRectangle(float32(thumbX), float32(ty)-2, float32(thumbW), float32(sbH)+4)
		if !s.hSbDrag && !s.mouseOverMinimap() && rl.IsMouseButtonPressed(rl.MouseButtonLeft) && rl.CheckCollisionPointRec(mouse, hitR) {
			s.hSbDrag = true
			s.hSbDragOff = mouse.X - float32(thumbX)
		}
		rl.DrawRectangle(tx, ty, tw, sbH, trackCol)
		rl.DrawRectangle(thumbX, ty, thumbW, sbH, thumbCol)
	}
}

func drawMapTileLayers(s *mapState) {
	if s.sheetTex.ID == 0 || s.tileSize <= 0 || s.sheetSz <= 0 {
		return
	}
	columns := s.sheetColumns
	if columns <= 0 {
		columns = s.sheetSz / s.tileSize
	}
	if columns <= 0 {
		return
	}
	firstGID := s.tilesetFirstGID
	if firstGID <= 0 {
		firstGID = 1
	}
	cellSz := s.tileSize * s.zoom
	if cellSz <= 0 {
		return
	}
	vx, vy, vw, vh := s.vpRect()
	visW := int(vw)/cellSz + 1
	visH := int(vh)/cellSz + 1

	viewRight := int(vx) + int(vw)
	viewBottom := int(vy) + int(vh)

	for li := len(s.layers) - 1; li >= 0; li-- {
		layer := s.layers[li]
		if !layer.visible || layer.kind != layerKindTile || len(layer.data) == 0 {
			continue
		}
		for ty := 0; ty < visH; ty++ {
			screenY := int(vy) + ty*cellSz
			if screenY >= viewBottom {
				break
			}
			mapY := s.scrollY + ty
			if mapY < 0 || mapY >= s.mapH {
				continue
			}
			for tx := 0; tx < visW; tx++ {
				screenX := int(vx) + tx*cellSz
				if screenX >= viewRight {
					break
				}
				mapX := s.scrollX + tx
				if mapX < 0 || mapX >= s.mapW {
					continue
				}
				gid := layer.data[mapY*s.mapW+mapX]
				rawID := gid &^ gidFlagMask
				if rawID < uint32(firstGID) {
					continue
				}
				ti := int(rawID) - firstGID
				p := gidToRenderParams(gid & gidFlagMask)
				dst := rl.NewRectangle(float32(screenX), float32(screenY), float32(cellSz), float32(cellSz))
				drawTileTransformed(s.sheetTex, ti, columns, s.tileSize, dst, p.flipH, p.flipV, p.rotation, rl.White)
			}
		}
	}
}

// ── object layer rendering ────────────────────────────────────────────────────

var objLayerPalette = []rl.Color{
	rl.NewColor(255, 100, 100, 255),
	rl.NewColor(100, 220, 100, 255),
	rl.NewColor(100, 160, 255, 255),
	rl.NewColor(255, 210, 50, 255),
	rl.NewColor(210, 100, 255, 255),
	rl.NewColor(50, 220, 220, 255),
	rl.NewColor(255, 150, 50, 255),
	rl.NewColor(180, 180, 180, 255),
}

func objLayerColor(layerIdx int) rl.Color {
	return objLayerPalette[layerIdx%len(objLayerPalette)]
}

func drawMapObjectLayers(s *mapState) {
	if s.tileSize <= 0 || s.zoom <= 0 {
		return
	}
	cellSz := float32(s.tileSize * s.zoom)
	vx, vy, vw, vh := s.vpRect()

	// Draw bottom-up (last layer first) so upper layers appear on top.
	for li := len(s.layers) - 1; li >= 0; li-- {
		layer := s.layers[li]
		if !layer.visible || layer.kind != layerKindObject {
			continue
		}
		c := objLayerColor(li)
		fill := rl.NewColor(c.R, c.G, c.B, 45)
		border := rl.NewColor(c.R, c.G, c.B, 200)
		label := rl.NewColor(c.R, c.G, c.B, 230)
		for _, obj := range layer.objects {
			if obj.Width == 0 && obj.Height == 0 {
				continue
			}
			sx := int32(float32(vx) + (float32(obj.X)/float32(s.tileSize)-float32(s.scrollX))*cellSz)
			sy := int32(float32(vy) + (float32(obj.Y)/float32(s.tileSize)-float32(s.scrollY))*cellSz)
			sw := int32(float32(obj.Width) / float32(s.tileSize) * cellSz)
			sh := int32(float32(obj.Height) / float32(s.tileSize) * cellSz)
			if sx+sw < vx || sx > vx+vw || sy+sh < vy || sy > vy+vh {
				continue
			}
			rl.DrawRectangle(sx, sy, sw, sh, fill)
			rl.DrawRectangleLines(sx, sy, sw, sh, border)
			if obj.Name != "" && sw > 8 {
				rl.DrawText(obj.Name, sx+2, sy+2, 8, label)
			}
		}
	}

	// Drag-preview for a new object being drawn.
	if s.objDragActive {
		x0, x1 := s.objDragX0, s.objDragX1
		y0, y1 := s.objDragY0, s.objDragY1
		if x0 > x1 {
			x0, x1 = x1, x0
		}
		if y0 > y1 {
			y0, y1 = y1, y0
		}
		sx := int32(float32(vx) + float32(x0-s.scrollX)*cellSz)
		sy := int32(float32(vy) + float32(y0-s.scrollY)*cellSz)
		sw := int32(float32(x1-x0+1) * cellSz)
		sh := int32(float32(y1-y0+1) * cellSz)
		c := objLayerColor(s.activeLayer)
		rl.DrawRectangle(sx, sy, sw, sh, rl.NewColor(c.R, c.G, c.B, 60))
		rl.DrawRectangleLines(sx, sy, sw, sh, rl.NewColor(c.R, c.G, c.B, 255))
	}
}

// ── layers below drawing area ─────────────────────────────────────────────────

func drawMapBelowLayers(s *mapState) {
	lw := rl.MeasureText("LAYERS", 10)
	rl.DrawText("LAYERS", drawAreaX+(drawAreaSz-lw)/2, mapBelowLayersY-12, 10, rl.NewColor(180, 180, 180, 255))

	listX := drawAreaX
	listW := drawAreaSz

	rl.DrawRectangle(listX, mapBelowLayersY, listW, mapBelowListH, rl.NewColor(28, 28, 35, 255))
	rl.DrawRectangleLines(listX, mapBelowLayersY, listW, mapBelowListH, rl.NewColor(65, 65, 80, 255))

	visRows := int(mapBelowListH) / int(mapLayerRowH)
	mouse := rl.GetMousePosition()

	for slot := 0; slot < visRows; slot++ {
		i := s.layerScroll + slot
		if i >= len(s.layers) {
			break
		}
		layer := s.layers[i]
		rowY := mapBelowLayersY + int32(slot)*mapLayerRowH

		if i == s.activeLayer {
			rl.DrawRectangle(listX+1, rowY+1, listW-2, mapLayerRowH-2, rl.NewColor(55, 80, 130, 255))
		}

		vis := layer.visible
		raygui.CheckBox(rl.NewRectangle(float32(listX)+4, float32(rowY)+3, 14, 14), "", &vis)
		if vis != layer.visible {
			s.layers[i].visible = vis
			s.minimapDirty = true
		}

		// Kind badge
		var badgeCol rl.Color
		var badgeTxt string
		if layer.kind == layerKindTile {
			badgeCol = rl.NewColor(70, 130, 180, 255)
			badgeTxt = "T"
		} else {
			badgeCol = rl.NewColor(180, 130, 60, 255)
			badgeTxt = "O"
		}
		rl.DrawRectangle(listX+22, rowY+3, 14, 14, badgeCol)
		rl.DrawText(badgeTxt, listX+25, rowY+5, 9, rl.White)

		// Class tag area: right 68px of the row (before scrollbar margin)
		classAreaX := listX + listW - 75
		classAreaW := int32(65)

		if s.renaming && i == s.activeLayer {
			rl.DrawRectangle(listX+40, rowY+2, classAreaX-listX-42, mapLayerRowH-4, rl.NewColor(20, 20, 28, 255))
			rl.DrawRectangleLines(listX+40, rowY+2, classAreaX-listX-42, mapLayerRowH-4, rl.NewColor(100, 140, 220, 255))
			rl.DrawText(s.renameText+"_", listX+44, rowY+5, 10, rl.White)
		} else if s.classEditing && i == s.activeLayer {
			rl.DrawRectangle(listX+40, rowY+2, classAreaX-listX-42, mapLayerRowH-4, rl.NewColor(20, 20, 28, 255))
			nameCol := rl.NewColor(200, 200, 200, 255)
			if !layer.visible {
				nameCol = rl.NewColor(90, 90, 100, 255)
			}
			rl.DrawText(layer.name, listX+40, rowY+5, 10, nameCol)
			// Class edit input box
			rl.DrawRectangle(classAreaX, rowY+2, classAreaW, mapLayerRowH-4, rl.NewColor(20, 20, 28, 255))
			rl.DrawRectangleLines(classAreaX, rowY+2, classAreaW, mapLayerRowH-4, rl.NewColor(100, 200, 120, 255))
			rl.DrawText(s.classEditText+"_", classAreaX+3, rowY+5, 9, rl.NewColor(120, 220, 140, 255))
		} else {
			nameCol := rl.NewColor(200, 200, 200, 255)
			if !layer.visible {
				nameCol = rl.NewColor(90, 90, 100, 255)
			}
			rl.DrawText(layer.name, listX+40, rowY+5, 10, nameCol)
			// Class tag
			if layer.class != "" {
				rl.DrawRectangle(classAreaX, rowY+3, classAreaW, mapLayerRowH-6, rl.NewColor(40, 55, 40, 255))
				rl.DrawRectangleLines(classAreaX, rowY+3, classAreaW, mapLayerRowH-6, rl.NewColor(70, 110, 70, 255))
				cw := rl.MeasureText(layer.class, 9)
				cx := classAreaX + (classAreaW-cw)/2
				rl.DrawText(layer.class, cx, rowY+5, 9, rl.NewColor(130, 200, 130, 255))
			}

			// Name hit area (double-click = rename)
			nameR := rl.NewRectangle(float32(listX+40), float32(rowY), float32(classAreaX-listX-42), float32(mapLayerRowH))
			if !s.mouseOverMinimap() && rl.CheckCollisionPointRec(mouse, nameR) && rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
				now := rl.GetTime()
				if i == s.activeLayer && now-s.layerLastClickTime < 0.4 {
					s.layerRenameStart()
				} else {
					s.activeLayer = i
					s.classEditing = false
					s.ensureLayerVisible()
				}
				s.layerLastClickTime = now
			}
			// Class hit area (double-click = edit class)
			classR := rl.NewRectangle(float32(classAreaX), float32(rowY), float32(classAreaW), float32(mapLayerRowH))
			if !s.mouseOverMinimap() && rl.CheckCollisionPointRec(mouse, classR) && rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
				now := rl.GetTime()
				if i == s.activeLayer && now-s.layerLastClickTime < 0.4 {
					s.layerClassEditStart()
				} else {
					s.activeLayer = i
					s.renaming = false
					s.ensureLayerVisible()
				}
				s.layerLastClickTime = now
			}
		}
	}

	// Scrollbar (drawn inside list on right edge, draggable)
	if len(s.layers) > visRows {
		trackCol := rl.NewColor(35, 35, 48, 255)
		thumbCol := rl.NewColor(85, 110, 160, 255)
		sbX := listX + listW - 7
		sbY := mapBelowLayersY + 1
		sbH := mapBelowListH - 2
		sbW := int32(5)
		maxSc := int32(len(s.layers) - visRows)
		thumbH := sbH * int32(visRows) / int32(len(s.layers))
		if thumbH < 8 {
			thumbH = 8
		}
		if s.layerSbDrag {
			if rl.IsMouseButtonDown(rl.MouseButtonLeft) {
				newY := mouse.Y - s.layerSbDragOff
				if newY < float32(sbY) {
					newY = float32(sbY)
				}
				if maxY := float32(sbY + sbH - thumbH); newY > maxY {
					newY = maxY
				}
				if sbH-thumbH > 0 {
					s.layerScroll = int(float32(maxSc) * (newY - float32(sbY)) / float32(sbH-thumbH))
				}
				s.clampLayerScroll()
			} else {
				s.layerSbDrag = false
			}
		}
		thumbY := sbY + int32(s.layerScroll)*(sbH-thumbH)/maxSc
		hitR := rl.NewRectangle(float32(sbX)-2, float32(thumbY), float32(sbW)+4, float32(thumbH))
		if !s.layerSbDrag && !s.mouseOverMinimap() && rl.IsMouseButtonPressed(rl.MouseButtonLeft) && rl.CheckCollisionPointRec(mouse, hitR) {
			s.layerSbDrag = true
			s.layerSbDragOff = mouse.Y - float32(thumbY)
		}
		rl.DrawRectangle(sbX, sbY, sbW, sbH, trackCol)
		rl.DrawRectangle(sbX, thumbY, sbW, thumbH, thumbCol)
	}

	bsz := float32(mapBelowBtnH)
	bx := float32(listX)
	by := float32(mapBelowBtnsY)
	if raygui.Button(rl.NewRectangle(bx, by, bsz, bsz), raygui.IconText(raygui.ICON_FILE_ADD, "")) {
		s.layerAdd()
	}
	bx += bsz + 2
	if raygui.Button(rl.NewRectangle(bx, by, bsz, bsz), raygui.IconText(raygui.ICON_FILE_DELETE, "")) {
		s.layerDelete()
	}
	bx += bsz + 2
	if raygui.Button(rl.NewRectangle(bx, by, bsz, bsz), raygui.IconText(raygui.ICON_ARROW_UP, "")) {
		s.layerMoveUp()
	}
	bx += bsz + 2
	if raygui.Button(rl.NewRectangle(bx, by, bsz, bsz), raygui.IconText(raygui.ICON_ARROW_DOWN, "")) {
		s.layerMoveDown()
	}
}

// ── right panel ───────────────────────────────────────────────────────────────

var mapToolHighlight = rl.NewColor(100, 160, 220, 255)

func mapToolBtn(r rl.Rectangle, icon string, tool, active drawTool) bool {
	clicked := raygui.Button(r, icon)
	if active == tool {
		rl.DrawRectangleLines(int32(r.X)-1, int32(r.Y)-1, int32(r.Width)+2, int32(r.Height)+2, mapToolHighlight)
	}
	return clicked
}

func drawMapRightPanel(s *mapState) {
	if s.activeLayer >= 0 && s.activeLayer < len(s.layers) &&
		s.layers[s.activeLayer].kind == layerKindObject {
		drawMapObjectPanel(s)
	} else {
		drawMapTilePanel(s)
	}
}

// ── object panel ──────────────────────────────────────────────────────────────

func drawMapObjectPanel(s *mapState) {
	const (
		listY    = int32(mapRTools1Y + 28) // 74
		rowH     = int32(18)
		listH    = int32(302) // y=74..376
		sepY     = listY + listH + 2
		nameAreaY = sepY + 4
	)
	listX := int32(panelX + 4)
	listW := int32(panelW - 8)

	// Header
	lw := rl.MeasureText("OBJECTS", 10)
	rl.DrawText("OBJECTS", panelX+(panelW-lw)/2, mapRToolsLabelY, 10, rl.NewColor(180, 180, 180, 255))


	if s.activeLayer >= len(s.layers) {
		return
	}
	layer := &s.layers[s.activeLayer]
	c := objLayerColor(s.activeLayer)

	// List background
	rl.DrawRectangle(listX, listY, listW, listH, rl.NewColor(28, 28, 35, 255))
	rl.DrawRectangleLines(listX, listY, listW, listH, rl.NewColor(65, 65, 80, 255))

	visRows := int(listH / rowH)
	if s.objListScroll < 0 {
		s.objListScroll = 0
	}
	maxScroll := len(layer.objects) - visRows
	if maxScroll < 0 {
		maxScroll = 0
	}
	if s.objListScroll > maxScroll {
		s.objListScroll = maxScroll
	}

	mouse := rl.GetMousePosition()
	now := rl.GetTime()
	for slot := 0; slot < visRows; slot++ {
		i := s.objListScroll + slot
		if i >= len(layer.objects) {
			break
		}
		obj := &layer.objects[i]
		rowY := listY + int32(slot)*rowH

		if i == s.selectedObj {
			rl.DrawRectangle(listX+1, rowY+1, listW-2, rowH-2, rl.NewColor(55, 80, 130, 255))
		}
		// Color dot
		rl.DrawRectangle(listX+3, rowY+4, 8, rowH-8, rl.NewColor(c.R, c.G, c.B, 200))

		// Delete button (rightmost)
		delR := rl.NewRectangle(float32(listX+listW-19), float32(rowY+1), 17, float32(rowH-2))
		if raygui.Button(delR, raygui.IconText(raygui.ICON_CROSS_SMALL, "")) {
			layer.objects = append(layer.objects[:i], layer.objects[i+1:]...)
			if s.selectedObj >= len(layer.objects) {
				s.selectedObj = len(layer.objects) - 1
			}
			s.objRenaming = false
			s.objIDEditing = false
			s.markDirty()
			break
		}

		// ID region: fixed-width right of name, before delete button
		idLabel := fmt.Sprintf("#%d", obj.ID)
		idW := int32(rl.MeasureText(idLabel, 8)) + 6
		idX := listX + listW - 20 - idW
		idRect := rl.NewRectangle(float32(idX), float32(rowY), float32(idW), float32(rowH))

		// Name region: dot + name, left of ID
		nameRect := rl.NewRectangle(float32(listX), float32(rowY), float32(idX-listX), float32(rowH))

		if !s.mouseOverMinimap() && rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
			if rl.CheckCollisionPointRec(mouse, nameRect) {
				if i == s.selectedObj && now-s.lastClickTime < 0.4 && s.lastClickType == 1 {
					s.objRenaming = true
					s.objIDEditing = false
					s.objRenameText = obj.Name
				} else {
					s.selectedObj = i
					s.objRenaming = false
					s.objIDEditing = false
				}
				s.lastClickTime = now
				s.lastClickType = 1
			} else if rl.CheckCollisionPointRec(mouse, idRect) {
				if i == s.selectedObj && now-s.lastClickTime < 0.4 && s.lastClickType == 2 {
					s.objIDEditing = true
					s.objRenaming = false
					s.objIDText = strconv.Itoa(obj.ID)
				} else {
					s.selectedObj = i
					s.objRenaming = false
					s.objIDEditing = false
				}
				s.lastClickTime = now
				s.lastClickType = 2
			}
		}

		// Draw name (or rename edit box)
		if i == s.selectedObj && s.objRenaming {
			rl.DrawRectangle(listX+14, rowY+1, idX-listX-14, rowH-2, rl.NewColor(20, 20, 28, 255))
			rl.DrawRectangleLines(listX+14, rowY+1, idX-listX-14, rowH-2, rl.NewColor(100, 140, 220, 255))
			rl.DrawText(s.objRenameText+"_", listX+16, rowY+4, 8, rl.White)
		} else {
			displayName := obj.Name
			if displayName == "" {
				displayName = fmt.Sprintf("obj_%d", obj.ID)
			}
			rl.DrawText(displayName, listX+14, rowY+4, 8, rl.NewColor(200, 200, 200, 255))
		}

		// Draw ID (or ID edit box)
		if i == s.selectedObj && s.objIDEditing {
			rl.DrawRectangle(idX, rowY+1, idW, rowH-2, rl.NewColor(20, 20, 28, 255))
			rl.DrawRectangleLines(idX, rowY+1, idW, rowH-2, rl.NewColor(100, 140, 220, 255))
			rl.DrawText("#"+s.objIDText+"_", idX+2, rowY+4, 8, rl.White)
		} else {
			rl.DrawText(idLabel, idX+3, rowY+4, 8, rl.NewColor(150, 150, 170, 255))
		}
	}

	// Scrollbar (draggable)
	if len(layer.objects) > visRows {
		trackCol := rl.NewColor(35, 35, 48, 255)
		thumbCol := rl.NewColor(85, 110, 160, 255)
		sbX := listX + listW - 7
		sbY := listY + 1
		sbH := listH - 2
		sbW := int32(5)
		maxSc := int32(len(layer.objects) - visRows)
		thumbH := sbH * int32(visRows) / int32(len(layer.objects))
		if thumbH < 8 {
			thumbH = 8
		}
		if s.objSbDrag {
			if rl.IsMouseButtonDown(rl.MouseButtonLeft) {
				newY := mouse.Y - s.objSbDragOff
				if newY < float32(sbY) {
					newY = float32(sbY)
				}
				if maxY := float32(sbY + sbH - thumbH); newY > maxY {
					newY = maxY
				}
				if sbH-thumbH > 0 {
					s.objListScroll = int(float32(maxSc) * (newY - float32(sbY)) / float32(sbH-thumbH))
				}
			} else {
				s.objSbDrag = false
			}
		}
		thumbY := sbY + int32(s.objListScroll)*(sbH-thumbH)/maxSc
		hitR := rl.NewRectangle(float32(sbX)-2, float32(thumbY), float32(sbW)+4, float32(thumbH))
		if !s.objSbDrag && !s.mouseOverMinimap() && rl.IsMouseButtonPressed(rl.MouseButtonLeft) && rl.CheckCollisionPointRec(mouse, hitR) {
			s.objSbDrag = true
			s.objSbDragOff = mouse.Y - float32(thumbY)
		}
		rl.DrawRectangle(sbX, sbY, sbW, sbH, trackCol)
		rl.DrawRectangle(sbX, thumbY, sbW, thumbH, thumbCol)
	}

	// Separator
	rl.DrawLine(listX, sepY, listX+listW, sepY, rl.NewColor(55, 55, 70, 255))

	// Selected object info (read-only position/size; name/ID edited inline in list)
	if s.selectedObj >= 0 && s.selectedObj < len(layer.objects) {
		obj := &layer.objects[s.selectedObj]
		ny := nameAreaY
		rl.DrawText(fmt.Sprintf("x:%.0f  y:%.0f", obj.X, obj.Y), listX, ny, 8, rl.NewColor(120, 120, 135, 255))
		ny += 13
		rl.DrawText(fmt.Sprintf("w:%.0f  h:%.0f", obj.Width, obj.Height), listX, ny, 8, rl.NewColor(120, 120, 135, 255))
		ny += 13
		rl.DrawText("dbl-click name or #id to edit", listX, ny, 8, rl.NewColor(85, 85, 100, 255))
	}
}

func nextLayerObjID(layer *mapLayer) int {
	max := 0
	for _, obj := range layer.objects {
		if obj.ID > max {
			max = obj.ID
		}
	}
	return max + 1
}

func (s *mapState) createDefaultObject() {
	if s.activeLayer >= len(s.layers) {
		return
	}
	layer := &s.layers[s.activeLayer]
	if layer.kind != layerKindObject {
		return
	}
	id := nextLayerObjID(layer)
	idVal, _ := json.Marshal(id)
	obj := mapObject{
		ID:      id,
		Name:    fmt.Sprintf("Object"),
		X:       float64(s.scrollX * s.tileSize),
		Y:       float64(s.scrollY * s.tileSize),
		Width:   float64(s.tileSize),
		Height:  float64(s.tileSize),
		Visible: true,
		Properties: []tiledProperty{
			{Name: "id", Type: "int", Value: json.RawMessage(idVal)},
		},
	}
	layer.objects = append(layer.objects, obj)
	s.selectedObj = len(layer.objects) - 1
	s.markDirty()
}

func (s *mapState) createObjectFromDrag() {
	if s.activeLayer >= len(s.layers) {
		return
	}
	layer := &s.layers[s.activeLayer]
	if layer.kind != layerKindObject {
		return
	}
	x0, x1 := s.objDragX0, s.objDragX1
	y0, y1 := s.objDragY0, s.objDragY1
	if x0 > x1 {
		x0, x1 = x1, x0
	}
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	id := nextLayerObjID(layer)
	idVal, _ := json.Marshal(id)
	obj := mapObject{
		ID:      id,
		Name:    "Object",
		X:       float64(x0 * s.tileSize),
		Y:       float64(y0 * s.tileSize),
		Width:   float64((x1 - x0 + 1) * s.tileSize),
		Height:  float64((y1 - y0 + 1) * s.tileSize),
		Visible: true,
		Properties: []tiledProperty{
			{Name: "id", Type: "int", Value: json.RawMessage(idVal)},
		},
	}
	layer.objects = append(layer.objects, obj)
	s.selectedObj = len(layer.objects) - 1
	s.markDirty()
}

func (s *mapState) objRenameConfirm() {
	if s.activeLayer >= len(s.layers) {
		return
	}
	layer := &s.layers[s.activeLayer]
	if s.selectedObj >= 0 && s.selectedObj < len(layer.objects) {
		layer.objects[s.selectedObj].Name = s.objRenameText
	}
	s.objRenaming = false
	s.markDirty()
}

// ── tile panel ────────────────────────────────────────────────────────────────

func drawMapTilePanel(s *mapState) {
	const bsz = float32(24)
	step := bsz + 4

	lw := rl.MeasureText("TOOLS", 10)
	rl.DrawText("TOOLS", panelX+(panelW-lw)/2, mapRToolsLabelY, 10, rl.NewColor(180, 180, 180, 255))

	mouse := rl.GetMousePosition()
	var hoveredTip string
	tip := func(text string, bx, by float32) {
		if rl.CheckCollisionPointRec(mouse, rl.NewRectangle(bx, by, bsz, bsz)) {
			hoveredTip = text
		}
	}

	x := float32(panelX + 4)
	y1 := float32(mapRTools1Y)
	if mapToolBtn(rl.NewRectangle(x, y1, bsz, bsz), raygui.IconText(raygui.ICON_PENCIL, ""), toolPencil, s.activeTool) {
		s.activeTool = toolPencil
	}
	tip("Pencil (P)", x, y1)
	x += step
	if mapToolBtn(rl.NewRectangle(x, y1, bsz, bsz), raygui.IconText(raygui.ICON_RUBBER, ""), toolEraser, s.activeTool) {
		s.activeTool = toolEraser
	}
	tip("Eraser (E)", x, y1)
	x += step
	if mapToolBtn(rl.NewRectangle(x, y1, bsz, bsz), raygui.IconText(raygui.ICON_COLOR_BUCKET, ""), toolBucket, s.activeTool) {
		s.activeTool = toolBucket
	}
	tip("Fill (F)", x, y1)

	x = float32(panelX + 4)
	y2 := float32(mapRTools2Y)
	if raygui.Button(rl.NewRectangle(x, y2, bsz, bsz), raygui.IconText(raygui.ICON_SYMMETRY_HORIZONTAL, "")) {
		s.tileFlipH = !s.tileFlipH
	}
	if s.tileFlipH {
		rl.DrawRectangleLines(int32(x)-1, int32(y2)-1, int32(bsz)+2, int32(bsz)+2, mapToolHighlight)
	}
	tip("Flip Horizontal (H)", x, y2)
	x += step
	if raygui.Button(rl.NewRectangle(x, y2, bsz, bsz), raygui.IconText(raygui.ICON_SYMMETRY_VERTICAL, "")) {
		s.tileFlipV = !s.tileFlipV
	}
	if s.tileFlipV {
		rl.DrawRectangleLines(int32(x)-1, int32(y2)-1, int32(bsz)+2, int32(bsz)+2, mapToolHighlight)
	}
	tip("Flip Vertical (V)", x, y2)
	x += step
	if raygui.Button(rl.NewRectangle(x, y2, bsz, bsz), raygui.IconText(raygui.ICON_ROTATE_FILL, "")) {
		s.tileRotation = (s.tileRotation + 1) & 3
	}
	tip("Rotate CW (R)", x, y2)
	x += step
	if raygui.Button(rl.NewRectangle(x, y2, bsz, bsz), "") {
		s.tileRotation = (s.tileRotation + 3) & 3
	}
	tip("Rotate CCW (Shift+R)", x, y2)
	// Overlay the pre-rendered ROTATE_FILL icon flipped horizontally.
	if s.iconCCW.ID > 0 {
		tw := float32(s.iconCCW.Texture.Width)
		th := float32(s.iconCCW.Texture.Height)
		rl.DrawTexturePro(
			s.iconCCW.Texture,
			rl.NewRectangle(tw, th, -tw, -th),
			rl.NewRectangle(x+(bsz-tw)/2, y2+(bsz-th)/2, tw, th),
			rl.Vector2Zero(), 0, rl.White,
		)
	}

	if hoveredTip != "" {
		drawToolTip(hoveredTip, mouse.X, mouse.Y)
	}

	// Tile preview – 32×32 box at the right edge of the tools section
	const prevSz = int32(32)
	prevX := int32(panelX) + int32(panelW) - 4 - prevSz
	prevY := int32(mapRTools1Y) + (int32(mapRTools2Y+24)-int32(mapRTools1Y)-prevSz)/2 // vertically centred between rows
	rl.DrawRectangle(prevX-1, prevY-1, prevSz+2, prevSz+2, rl.NewColor(55, 55, 70, 255))
	// Checkerboard background (transparency indicator)
	const chkSz = int32(4)
	chkColours := [2]rl.Color{rl.NewColor(200, 200, 200, 255), rl.NewColor(160, 160, 160, 255)}
	for cy := int32(0); cy < prevSz; cy += chkSz {
		for cx := int32(0); cx < prevSz; cx += chkSz {
			rl.DrawRectangle(prevX+cx, prevY+cy, chkSz, chkSz, chkColours[((cx/chkSz)+(cy/chkSz))%2])
		}
	}
	if s.sheetTex.ID > 0 && s.sheetColumns > 0 {
		dst := rl.NewRectangle(float32(prevX), float32(prevY), float32(prevSz), float32(prevSz))
		drawTileTransformed(s.sheetTex, s.selectedTile, s.sheetColumns, s.tileSize, dst,
			s.tileFlipH, s.tileFlipV, float32(s.tileRotation)*90, rl.White)
	}

	sepX := int32(panelX + 4)
	sepW := int32(panelW - 8)
	rl.DrawLine(sepX, mapRToolsSepY, sepX+sepW, mapRToolsSepY, rl.NewColor(55, 55, 70, 255))

	tw := rl.MeasureText("TILESET", 10)
	rl.DrawText("TILESET", panelX+(panelW-tw)/2, mapRTilesetLblY, 10, rl.NewColor(180, 180, 180, 255))

	// (dropdown drawn last — see drawMapTilesetDropdown)

	drawMapTileSheet(s)
}

func drawMapTilesetDropdown(s *mapState) {
	if s.resize.active || s.saveActive || s.mapDropEdit {
		return
	}
	// Only visible in tile-layer mode
	if s.activeLayer >= 0 && s.activeLayer < len(s.layers) &&
		s.layers[s.activeLayer].kind == layerKindObject {
		return
	}
	dropX := float32(panelX + 4)
	dropW := float32(panelW - 8)

	prev := s.prevDropEdit
	s.prevDropEdit = s.sheetDropEdit

	txt := s.sheetText
	if txt == "" {
		txt = "(no sheets)"
	}
	if raygui.DropdownBox(rl.NewRectangle(dropX, float32(mapRTilesetDropY), dropW, 20), txt, &s.sheetActive, s.sheetDropEdit) {
		s.sheetDropEdit = !s.sheetDropEdit
	}
	if prev && !s.sheetDropEdit && len(s.sheetList) > 0 {
		if int(s.sheetActive) < len(s.sheetList) {
			s.pendingSheetName = s.sheetList[s.sheetActive]
		}
	}
}

func drawMapFileDropdown(s *mapState) {
	if s.resize.active || s.saveActive || s.sheetDropEdit {
		return
	}
	prev := s.prevMapDropEdit
	s.prevMapDropEdit = s.mapDropEdit

	txt := s.mapText
	if txt == "" {
		txt = "(no maps)"
	}
	if raygui.DropdownBox(rl.NewRectangle(92, 4, 160, 20), txt, &s.mapActive, s.mapDropEdit) {
		s.mapDropEdit = !s.mapDropEdit
	}
	if prev && !s.mapDropEdit && len(s.mapList) > 0 {
		if int(s.mapActive) < len(s.mapList) {
			path := filepath.Join("assets", "maps", s.mapList[s.mapActive])
			if path != s.mapPath {
				s.pendingMapPath = path
			}
		}
	}
}

// ── tile sheet picker ─────────────────────────────────────────────────────────

func drawMapTileSheet(s *mapState) {
	px, sz, scale := s.tileSheetLayout()
	tileSz := int32(s.tileSize)
	qSz := int32(s.quadrantSz())

	tabW := sz / 4
	if tabW <= 0 {
		tabW = 1
	}
	mouse := rl.GetMousePosition()
	for i := range 4 {
		tx := px + int32(i)*tabW
		var bg rl.Color
		if s.activeQuadrant == i {
			bg = rl.NewColor(70, 100, 140, 255)
		} else {
			bg = rl.NewColor(45, 45, 50, 255)
		}
		rl.DrawRectangle(tx, mapSheetTabY, tabW, mapSheetTabH, bg)
		rl.DrawRectangleLines(tx, mapSheetTabY, tabW, mapSheetTabH, rl.NewColor(60, 60, 60, 255))
		rl.DrawText(fmt.Sprintf("%d", i+1), tx+tabW/2-3, mapSheetTabY+4, 10, rl.White)
		r := rl.NewRectangle(float32(tx), float32(mapSheetTabY), float32(tabW), float32(mapSheetTabH))
		if !s.mouseOverMinimap() && rl.CheckCollisionPointRec(mouse, r) && rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
			s.activeQuadrant = i
		}
	}

	var quadOffX, quadOffY int32
	switch s.activeQuadrant {
	case 1:
		quadOffX = qSz
	case 2:
		quadOffY = qSz
	case 3:
		quadOffX, quadOffY = qSz, qSz
	}

	const chkSz = int32(6)
	chk := [2]rl.Color{rl.NewColor(232, 232, 232, 255), rl.NewColor(196, 196, 196, 255)}
	for cy := int32(0); cy < sz; cy += chkSz {
		for cx := int32(0); cx < sz; cx += chkSz {
			rl.DrawRectangle(px+cx, mapSheetGridY+cy, chkSz, chkSz, chk[((cx/chkSz)+(cy/chkSz))%2])
		}
	}

	if s.sheetTex.ID > 0 && qSz > 0 {
		src := rl.NewRectangle(float32(quadOffX), float32(quadOffY), float32(qSz), float32(qSz))
		dst := rl.NewRectangle(float32(px), float32(mapSheetGridY), float32(sz), float32(sz))
		rl.DrawTexturePro(s.sheetTex, src, dst, rl.NewVector2(0, 0), 0, rl.White)
	}

	if s.tileSize > 0 && qSz > 0 {
		tilesInQuad := int(qSz) / s.tileSize
		gc := rl.NewColor(80, 130, 200, 160)
		for i := 0; i <= tilesInQuad; i++ {
			x := px + int32(i)*tileSz*scale
			rl.DrawLine(x, mapSheetGridY, x, mapSheetGridY+sz, gc)
			yy := mapSheetGridY + int32(i)*tileSz*scale
			rl.DrawLine(px, yy, px+sz, yy, gc)
		}

		if s.sheetSz > 0 {
			tpr := s.sheetSz / s.tileSize
			lx := s.selectedTile%tpr - int(quadOffX)/s.tileSize
			ly := s.selectedTile/tpr - int(quadOffY)/s.tileSize
			if lx >= 0 && lx < tilesInQuad && ly >= 0 && ly < tilesInQuad {
				hx := px + int32(lx)*tileSz*scale
				hy := mapSheetGridY + int32(ly)*tileSz*scale
				rl.DrawRectangleLines(hx-1, hy-1, tileSz*scale+2, tileSz*scale+2, rl.White)
				rl.DrawRectangleLines(hx, hy, tileSz*scale, tileSz*scale, rl.NewColor(0, 0, 0, 180))
			}
		}

		mx, my := int32(mouse.X), int32(mouse.Y)
		if mx >= px && mx < px+sz && my >= mapSheetGridY && my < mapSheetGridY+sz {
			cellSz := tileSz * scale
			col := int((mx - px) / cellSz)
			row := int((my - mapSheetGridY) / cellSz)
			rl.DrawRectangleLines(px+int32(col)*cellSz, mapSheetGridY+int32(row)*cellSz,
				cellSz, cellSz, rl.NewColor(255, 255, 100, 200))
			if !s.mouseOverMinimap() && rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
				tpr := s.sheetSz / s.tileSize
				s.selectedTile = (int(quadOffY)/s.tileSize+row)*tpr + int(quadOffX)/s.tileSize + col
			}
		}
	}
	rl.DrawRectangleLines(px-1, mapSheetGridY-1, sz+2, sz+2, rl.NewColor(60, 60, 60, 255))
}

// ── resize dialog ─────────────────────────────────────────────────────────────

func drawMapResizeDialog(s *mapState) {
	if !s.resize.active {
		return
	}
	rl.DrawRectangle(0, 0, virtualW, virtualH, rl.NewColor(0, 0, 0, 140))
	const dw, dh = int32(220), int32(120)
	dx := (virtualW - dw) / 2
	dy := (virtualH - dh) / 2

	rl.DrawRectangle(dx, dy, dw, dh, rl.NewColor(38, 38, 50, 255))
	rl.DrawRectangleLines(dx, dy, dw, dh, rl.NewColor(80, 100, 150, 255))
	rl.DrawText("Resize Map", dx+8, dy+8, 11, rl.NewColor(200, 200, 210, 255))
	rl.DrawLine(dx, dy+22, dx+dw, dy+22, rl.NewColor(60, 60, 80, 255))

	fw := int32(60)
	rl.DrawText("Width (tiles):", dx+8, dy+36, 10, rl.LightGray)
	drawResizeField(s, dx+dw-fw-8, dy+32, fw, true)
	rl.DrawText("Height (tiles):", dx+8, dy+60, 10, rl.LightGray)
	drawResizeField(s, dx+dw-fw-8, dy+56, fw, false)

	bw := int32(60)
	if raygui.Button(rl.NewRectangle(float32(dx+dw-bw*2-14), float32(dy+dh-28), float32(bw), 20), "OK") {
		applyMapResize(s)
	}
	if raygui.Button(rl.NewRectangle(float32(dx+dw-bw-6), float32(dy+dh-28), float32(bw), 20), "Cancel") {
		s.resize.active = false
	}
}

func drawResizeField(s *mapState, x, y, w int32, isWidth bool) {
	focused := (isWidth && s.resize.focusW) || (!isWidth && !s.resize.focusW)
	bc := rl.NewColor(65, 65, 80, 255)
	if focused {
		bc = rl.NewColor(100, 140, 220, 255)
	}
	rl.DrawRectangle(x, y, w, 18, rl.NewColor(20, 20, 28, 255))
	rl.DrawRectangleLines(x, y, w, 18, bc)
	txt := s.resize.wText
	if !isWidth {
		txt = s.resize.hText
	}
	if focused {
		txt += "_"
	}
	rl.DrawText(txt, x+4, y+4, 10, rl.White)
	mouse := rl.GetMousePosition()
	if rl.CheckCollisionPointRec(mouse, rl.NewRectangle(float32(x), float32(y), float32(w), 18)) &&
		rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
		s.resize.focusW = isWidth
	}
}

func applyMapResize(s *mapState) {
	w, errW := strconv.Atoi(strings.TrimSpace(s.resize.wText))
	h, errH := strconv.Atoi(strings.TrimSpace(s.resize.hText))
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return
	}
	s.pushMapUndo()
	// Resize tile layer data
	for i, l := range s.layers {
		if l.kind != layerKindTile {
			continue
		}
		newData := make([]uint32, w*h)
		for ty := 0; ty < h && ty < s.mapH; ty++ {
			for tx := 0; tx < w && tx < s.mapW; tx++ {
				newData[ty*w+tx] = l.data[ty*s.mapW+tx]
			}
		}
		s.layers[i].data = newData
	}
	s.mapW, s.mapH = w, h
	s.resize.active = false
	s.markDirty()
}

// ── save-as dialog ────────────────────────────────────────────────────────────

func drawMapSaveDialog(s *mapState) {
	if !s.saveActive {
		return
	}
	rl.DrawRectangle(0, 0, virtualW, virtualH, rl.NewColor(0, 0, 0, 160))
	const dw, dh = int32(280), int32(100)
	dx := (virtualW - dw) / 2
	dy := (virtualH - dh) / 2

	rl.DrawRectangle(dx, dy, dw, dh, rl.NewColor(38, 38, 50, 255))
	rl.DrawRectangleLines(dx, dy, dw, dh, rl.NewColor(80, 100, 150, 255))
	rl.DrawText("Save Map As", dx+8, dy+8, 11, rl.NewColor(200, 200, 210, 255))
	rl.DrawLine(dx, dy+22, dx+dw, dy+22, rl.NewColor(60, 60, 80, 255))

	// Text input
	rl.DrawRectangle(dx+8, dy+30, dw-16, 18, rl.NewColor(20, 20, 28, 255))
	rl.DrawRectangleLines(dx+8, dy+30, dw-16, 18, rl.NewColor(100, 140, 220, 255))
	display := s.saveFilename + "_"
	rl.DrawText(display, dx+12, dy+34, 10, rl.White)

	hint := "assets/maps/" + s.saveFilename + ".tmj"
	rl.DrawText(hint, dx+8, dy+54, 9, rl.NewColor(100, 100, 120, 255))

	bw := int32(60)
	if raygui.Button(rl.NewRectangle(float32(dx+dw-bw*2-14), float32(dy+dh-28), float32(bw), 20), "Save") {
		commitMapSave(s)
	}
	if raygui.Button(rl.NewRectangle(float32(dx+dw-bw-6), float32(dy+dh-28), float32(bw), 20), "Cancel") {
		s.saveActive = false
	}
}

func commitMapSave(s *mapState) {
	name := strings.TrimSpace(s.saveFilename)
	if name == "" {
		return
	}
	if !strings.HasSuffix(strings.ToLower(name), ".tmj") {
		name += ".tmj"
	}
	path := filepath.Join("assets", "maps", name)
	if err := saveMapTMJ(s, path); err == nil {
		s.mapPath = path
		s.saveActive = false
		refreshMapFileList(s)
		s.toast.Notify("Saved " + filepath.Base(path))
		s.markClean()
	}
}

// ── status bar ────────────────────────────────────────────────────────────────

func drawMapStatusBar(s *mapState) {
	y := virtualH - statusBarH
	rl.DrawRectangle(0, y, virtualW, statusBarH, rl.NewColor(30, 30, 30, 255))
	rl.DrawLine(0, y, virtualW, y, rl.NewColor(60, 60, 60, 255))
	layerName := ""
	if s.activeLayer < len(s.layers) {
		l := s.layers[s.activeLayer]
		kind := "tile"
		if l.kind == layerKindObject {
			kind = "obj"
		}
		layerName = fmt.Sprintf("%s (%s)", l.name, kind)
	}
	gridStr := "off"
	if s.showGrid {
		gridStr = "on"
	}
	toolNames := [...]string{"pencil", "fill", "eraser"}
	toolStr := toolNames[s.activeTool]
	cursorStr := ""
	if s.hoverValid {
		cursorStr = fmt.Sprintf("  XY: %d,%d", s.hoverX, s.hoverY)
	}
	status := fmt.Sprintf("Map: %dx%d  Tile: %dpx  Zoom: %dx  Layer: %s  Grid: %s  Tool: %s  Tile#: %d%s",
		s.mapW, s.mapH, s.tileSize, s.zoom, layerName, gridStr, toolStr, s.selectedTile, cursorStr)
	rl.DrawText(status, 4, y+5, 10, rl.LightGray)
}

// ── scroll-key and tile drawing helpers ───────────────────────────────────────

// handleScrollKeys advances map scroll for held WASD / arrow keys with an
// initial 0.25 s delay then 15 tiles/sec repeat — independent of frame rate.
func (s *mapState) handleScrollKeys(dt float64, ctrl bool) {
	upHeld    := rl.IsKeyDown(rl.KeyW) || rl.IsKeyDown(rl.KeyUp)
	downHeld  := (!ctrl && rl.IsKeyDown(rl.KeyS)) || rl.IsKeyDown(rl.KeyDown)
	leftHeld  := rl.IsKeyDown(rl.KeyA) || rl.IsKeyDown(rl.KeyLeft)
	rightHeld := rl.IsKeyDown(rl.KeyD) || rl.IsKeyDown(rl.KeyRight)

	if !upHeld && !downHeld && !leftHeld && !rightHeld {
		s.scrollKeyTimer = 0
		return
	}

	// Detect the first frame any scroll key is pressed (not just held).
	justPressed := rl.IsKeyPressed(rl.KeyW) || rl.IsKeyPressed(rl.KeyUp) ||
		(!ctrl && rl.IsKeyPressed(rl.KeyS)) || rl.IsKeyPressed(rl.KeyDown) ||
		rl.IsKeyPressed(rl.KeyA) || rl.IsKeyPressed(rl.KeyLeft) ||
		rl.IsKeyPressed(rl.KeyD) || rl.IsKeyPressed(rl.KeyRight)

	fire := false
	if justPressed {
		fire = true
		s.scrollKeyTimer = 0.25 // initial delay before auto-repeat starts
	} else {
		s.scrollKeyTimer -= dt
		if s.scrollKeyTimer <= 0 {
			fire = true
			s.scrollKeyTimer += 1.0 / 15.0 // repeat at 15 tiles/sec
		}
	}

	if fire {
		if upHeld    { s.scrollY-- }
		if downHeld  { s.scrollY++ }
		if leftHeld  { s.scrollX-- }
		if rightHeld { s.scrollX++ }
		s.clampScroll()
	}
}

// applyTileDraw writes or erases a single tile at map position (mx, my).
func (s *mapState) applyTileDraw(mx, my int, erase bool) {
	if mx < 0 || mx >= s.mapW || my < 0 || my >= s.mapH {
		return
	}
	if s.activeLayer >= len(s.layers) {
		return
	}
	layer := &s.layers[s.activeLayer]
	if layer.kind != layerKindTile || len(layer.data) == 0 {
		return
	}
	if erase {
		layer.data[my*s.mapW+mx] = 0
	} else {
		firstGID := s.tilesetFirstGID
		if firstGID <= 0 {
			firstGID = 1
		}
		gid := uint32(s.selectedTile+firstGID) | tileGIDFlags(s.tileFlipH, s.tileFlipV, s.tileRotation)
		layer.data[my*s.mapW+mx] = gid
	}
	s.markDirty()
}

// floodFill replaces all tiles connected to (startX, startY) that share the
// same GID with newGID (0 = erase, >0 = tile index+1).
func (s *mapState) floodFill(startX, startY int, newGID uint32) {
	if s.activeLayer >= len(s.layers) {
		return
	}
	layer := &s.layers[s.activeLayer]
	if layer.kind != layerKindTile || len(layer.data) == 0 {
		return
	}
	if startX < 0 || startX >= s.mapW || startY < 0 || startY >= s.mapH {
		return
	}
	oldGID := layer.data[startY*s.mapW+startX]
	if oldGID == newGID {
		return
	}

	type pt struct{ x, y int }
	visited := make([]bool, s.mapW*s.mapH)
	queue := []pt{{startX, startY}}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if p.x < 0 || p.x >= s.mapW || p.y < 0 || p.y >= s.mapH {
			continue
		}
		idx := p.y*s.mapW + p.x
		if visited[idx] || layer.data[idx] != oldGID {
			continue
		}
		visited[idx] = true
		layer.data[idx] = newGID
		queue = append(queue, pt{p.x + 1, p.y}, pt{p.x - 1, p.y}, pt{p.x, p.y + 1}, pt{p.x, p.y - 1})
	}
	s.markDirty()
}

// ── input ─────────────────────────────────────────────────────────────────────

func handleMapInput(s *mapState, dt float64) {
	if s.exitConfirm.Active {
		return
	}
	if s.resize.active {
		handleMapResizeInput(s)
		return
	}
	if s.saveActive {
		handleMapSaveInput(s)
		return
	}
	if s.renaming {
		handleMapRenameInput(s)
		return
	}
	if s.classEditing {
		handleMapClassInput(s)
		return
	}
	if s.objRenaming || s.objIDEditing {
		handleMapObjRenameInput(s)
		return
	}
	if s.showHelp {
		if rl.IsKeyPressed(rl.KeyF1) || rl.IsKeyPressed(rl.KeyEscape) {
			s.showHelp = false
		}
		return
	}
	if rl.IsKeyPressed(rl.KeyF1) {
		s.showHelp = true
	}
	if rl.IsKeyPressed(rl.KeyM) {
		s.showMinimap = !s.showMinimap
		if s.showMinimap {
			s.minimapDirty = true
		}
	}
	if rl.IsKeyPressed(rl.KeyTab) {
		s.focusMode = !s.focusMode
		s.clampScroll()
	}

	// Process minimap drag before tile interaction checks
	if handleMinimapInput(s) {
		return
	}
	if rl.IsKeyPressed(rl.KeyPageUp) {
		if s.activeLayer > 0 {
			s.activeLayer--
			s.ensureLayerVisible()
		}
	}
	if rl.IsKeyPressed(rl.KeyPageDown) {
		if s.activeLayer < len(s.layers)-1 {
			s.activeLayer++
			s.ensureLayerVisible()
		}
	}

	ctrl := rl.IsKeyDown(rl.KeyLeftControl) || rl.IsKeyDown(rl.KeyRightControl)

	// Mouse-wheel scroll (skip when Ctrl is held; Ctrl+wheel is handled as zoom below)
	if wheel := rl.GetMouseWheelMove(); wheel != 0 && !ctrl {
		mouse := rl.GetMousePosition()
		mx, my := mouse.X, mouse.Y
		vx, vy, vw, vh := s.vpRect()
		overViewport := mx >= float32(vx) && mx < float32(vx+vw) &&
			my >= float32(vy) && my < float32(vy+vh)
		overLayers := !s.focusMode &&
			mx >= float32(drawAreaX) && mx < float32(drawAreaX+drawAreaSz) &&
			my >= float32(mapBelowLayersY) && my < float32(mapBelowLayersY+mapBelowListH)
		if overViewport {
			if rl.IsKeyDown(rl.KeyLeftShift) || rl.IsKeyDown(rl.KeyRightShift) {
				s.scrollX -= int(wheel)
			} else {
				s.scrollY -= int(wheel)
			}
			s.clampScroll()
		}
		if overLayers {
			s.layerScroll -= int(wheel)
			s.clampLayerScroll()
		}
	}

	// WASD / arrow key scroll
	s.handleScrollKeys(dt, ctrl)

	shift := rl.IsKeyDown(rl.KeyLeftShift) || rl.IsKeyDown(rl.KeyRightShift)

	// Tile drawing and right-click pick — only when cursor is over a valid map cell and no scrollbar is held
	if s.hoverValid && !s.isSbDragging() {
		layer := -1
		if s.activeLayer < len(s.layers) {
			layer = s.activeLayer
		}
		if layer >= 0 && s.layers[layer].kind == layerKindTile {
			// Push one undo entry at the start of each pencil/eraser stroke.
			if rl.IsMouseButtonPressed(rl.MouseButtonLeft) &&
				(s.activeTool == toolPencil || s.activeTool == toolEraser) {
				s.pushMapUndo()
			}
			switch s.activeTool {
			case toolPencil:
				if rl.IsMouseButtonDown(rl.MouseButtonLeft) {
					s.applyTileDraw(s.hoverX, s.hoverY, false)
				}
			case toolEraser:
				if rl.IsMouseButtonDown(rl.MouseButtonLeft) {
					s.applyTileDraw(s.hoverX, s.hoverY, true)
				}
			case toolBucket:
				if rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
					s.pushMapUndo()
					firstGID := s.tilesetFirstGID
					if firstGID <= 0 {
						firstGID = 1
					}
					gid := uint32(s.selectedTile+firstGID) | tileGIDFlags(s.tileFlipH, s.tileFlipV, s.tileRotation)
					s.floodFill(s.hoverX, s.hoverY, gid)
				}
			}
			// Right-click: pick tile + transform from the map layer
			if rl.IsMouseButtonPressed(rl.MouseButtonRight) {
				gid := s.layers[layer].data[s.hoverY*s.mapW+s.hoverX]
				rawID := gid &^ gidFlagMask
				firstGID := s.tilesetFirstGID
				if firstGID <= 0 {
					firstGID = 1
				}
				if rawID >= uint32(firstGID) {
					ti := int(rawID) - firstGID
					if s.sheetColumns > 0 {
						maxTile := s.sheetColumns * (s.sheetSz / s.tileSize)
						if ti >= 0 && ti < maxTile {
							s.selectedTile = ti
							// Decode the stored flip+rotation flags back to editing state.
							// Use the canonical representative for each (H,V,D) combination.
							flags := gid & gidFlagMask
							p := gidToRenderParams(flags)
							s.tileFlipH = p.flipH
							s.tileFlipV = p.flipV
							switch p.rotation {
							case 90:
								s.tileRotation = 1
							case 180:
								s.tileRotation = 2
							case 270:
								s.tileRotation = 3
							default:
								s.tileRotation = 0
							}
						}
					}
				}
			}
		}
		// Object drag-to-draw: start on mouse-down over an object layer
		if layer >= 0 && s.layers[layer].kind == layerKindObject {
			if rl.IsMouseButtonPressed(rl.MouseButtonLeft) && !s.objDragActive {
				s.objDragActive = true
				s.objDragX0 = s.hoverX
				s.objDragY0 = s.hoverY
				s.objDragX1 = s.hoverX
				s.objDragY1 = s.hoverY
			}
		}
	}

	// Object drag: update end position while hovering, finish on release
	if s.objDragActive {
		if s.hoverValid {
			s.objDragX1 = s.hoverX
			s.objDragY1 = s.hoverY
		}
		if rl.IsMouseButtonReleased(rl.MouseButtonLeft) {
			s.createObjectFromDrag()
			s.objDragActive = false
		}
	}

	if rl.IsKeyPressed(rl.KeyG) {
		s.showGrid = !s.showGrid
	}
	// Zoom in/out: +/= key or Ctrl+wheel when over viewport
	zoomIn  := rl.IsKeyPressed(rl.KeyEqual) || rl.IsKeyPressed(rl.KeyKpAdd)
	zoomOut := rl.IsKeyPressed(rl.KeyMinus) || rl.IsKeyPressed(rl.KeyKpSubtract)
	if ctrl {
		if wheel := rl.GetMouseWheelMove(); wheel != 0 {
			m := rl.GetMousePosition()
			vx, vy, vw, vh := s.vpRect()
			if m.X >= float32(vx) && m.X < float32(vx+vw) && m.Y >= float32(vy) && m.Y < float32(vy+vh) {
				if wheel > 0 {
					zoomIn = true
				} else {
					zoomOut = true
				}
			}
		}
	}
	if zoomIn && s.zoom < 4 {
		s.zoom++
		s.clampScroll()
		s.minimapDirty = true
	}
	if zoomOut && s.zoom > 1 {
		s.zoom--
		s.clampScroll()
		s.minimapDirty = true
	}
	// Space: toggle active layer visibility
	if rl.IsKeyPressed(rl.KeySpace) && s.activeLayer < len(s.layers) {
		s.layers[s.activeLayer].visible = !s.layers[s.activeLayer].visible
		s.minimapDirty = true
	}
	if rl.IsKeyPressed(rl.KeyP) {
		s.activeTool = toolPencil
	}
	if rl.IsKeyPressed(rl.KeyE) {
		s.activeTool = toolEraser
	}
	if rl.IsKeyPressed(rl.KeyF) {
		s.activeTool = toolBucket
	}
	if rl.IsKeyPressed(rl.KeyH) {
		s.tileFlipH = !s.tileFlipH
	}
	if rl.IsKeyPressed(rl.KeyV) {
		s.tileFlipV = !s.tileFlipV
	}
	if rl.IsKeyPressed(rl.KeyR) {
		if shift {
			s.tileRotation = (s.tileRotation + 3) & 3 // CCW
		} else {
			s.tileRotation = (s.tileRotation + 1) & 3 // CW
		}
	}
	// Ctrl+S / Ctrl+Z / Ctrl+Y
	if ctrl && rl.IsKeyPressed(rl.KeyS) {
		if s.mapPath != "" {
			if err := saveMapTMJ(s, s.mapPath); err == nil {
				s.toast.Notify("Saved " + filepath.Base(s.mapPath))
				s.markClean()
			}
		} else {
			s.saveActive = true
			s.saveFilename = ""
		}
	}
	if ctrl && rl.IsKeyPressed(rl.KeyZ) {
		if shift {
			s.mapRedo()
		} else {
			s.mapUndo()
		}
	}
	if ctrl && rl.IsKeyPressed(rl.KeyY) {
		s.mapRedo()
	}

	if rl.IsKeyPressed(rl.KeyEscape) {
		if s.dirty {
			s.exitConfirm.Show("Unsaved changes — quit anyway?", "Quit", "Cancel")
		} else {
			s.wantQuit = true
		}
	}
}

func handleMapObjRenameInput(s *mapState) {
	if s.objIDEditing {
		if rl.IsKeyPressed(rl.KeyEnter) {
			s.confirmObjIDEdit()
			return
		}
		if rl.IsKeyPressed(rl.KeyEscape) {
			s.objIDEditing = false
			return
		}
		if rl.IsKeyPressed(rl.KeyBackspace) && len(s.objIDText) > 0 {
			s.objIDText = s.objIDText[:len(s.objIDText)-1]
		}
		for c := rl.GetCharPressed(); c != 0; c = rl.GetCharPressed() {
			if c >= '0' && c <= '9' && len(s.objIDText) < 6 {
				s.objIDText += string(c)
			}
		}
		return
	}
	if rl.IsKeyPressed(rl.KeyEnter) {
		s.objRenameConfirm()
		return
	}
	if rl.IsKeyPressed(rl.KeyEscape) {
		s.objRenaming = false
		return
	}
	if rl.IsKeyPressed(rl.KeyBackspace) && len(s.objRenameText) > 0 {
		r := []rune(s.objRenameText)
		s.objRenameText = string(r[:len(r)-1])
	}
	for c := rl.GetCharPressed(); c != 0; c = rl.GetCharPressed() {
		if len(s.objRenameText) < 64 {
			s.objRenameText += string(c)
		}
	}
}

func (s *mapState) confirmObjIDEdit() {
	if s.activeLayer >= len(s.layers) {
		s.objIDEditing = false
		return
	}
	layer := &s.layers[s.activeLayer]
	if s.selectedObj < 0 || s.selectedObj >= len(layer.objects) {
		s.objIDEditing = false
		return
	}
	newID, err := strconv.Atoi(s.objIDText)
	if err != nil || newID <= 0 {
		s.objIDEditing = false
		return
	}
	// Reject if another object in this layer already uses this ID
	for i, obj := range layer.objects {
		if i != s.selectedObj && obj.ID == newID {
			s.objIDEditing = false
			return
		}
	}
	obj := &layer.objects[s.selectedObj]
	obj.ID = newID
	// Keep the "id" property in sync
	for i, p := range obj.Properties {
		if p.Name == "id" {
			idVal, _ := json.Marshal(newID)
			obj.Properties[i].Value = json.RawMessage(idVal)
			break
		}
	}
	s.objIDEditing = false
	s.markDirty()
}

func handleMapClassInput(s *mapState) {
	if rl.IsKeyPressed(rl.KeyEnter) {
		s.layerClassEditConfirm()
		return
	}
	if rl.IsKeyPressed(rl.KeyEscape) {
		s.classEditing = false
		return
	}
	if rl.IsKeyPressed(rl.KeyBackspace) && len(s.classEditText) > 0 {
		r := []rune(s.classEditText)
		s.classEditText = string(r[:len(r)-1])
	}
	for c := rl.GetCharPressed(); c != 0; c = rl.GetCharPressed() {
		if len(s.classEditText) < 32 {
			s.classEditText += string(c)
		}
	}
}

func handleMapRenameInput(s *mapState) {
	if rl.IsKeyPressed(rl.KeyEnter) {
		s.layerRenameConfirm()
		return
	}
	if rl.IsKeyPressed(rl.KeyEscape) {
		s.renaming = false
		return
	}
	if rl.IsKeyPressed(rl.KeyBackspace) && len(s.renameText) > 0 {
		r := []rune(s.renameText)
		s.renameText = string(r[:len(r)-1])
	}
	for c := rl.GetCharPressed(); c != 0; c = rl.GetCharPressed() {
		if len(s.renameText) < 32 {
			s.renameText += string(c)
		}
	}
}

func handleMapResizeInput(s *mapState) {
	if rl.IsKeyPressed(rl.KeyEscape) {
		s.resize.active = false
		return
	}
	if rl.IsKeyPressed(rl.KeyEnter) {
		applyMapResize(s)
		return
	}
	if rl.IsKeyPressed(rl.KeyTab) {
		s.resize.focusW = !s.resize.focusW
	}
	target := &s.resize.wText
	if !s.resize.focusW {
		target = &s.resize.hText
	}
	if rl.IsKeyPressed(rl.KeyBackspace) && len(*target) > 0 {
		r := []rune(*target)
		*target = string(r[:len(r)-1])
	}
	for c := rl.GetCharPressed(); c != 0; c = rl.GetCharPressed() {
		if c >= '0' && c <= '9' && len(*target) < 6 {
			*target += string(c)
		}
	}
}

func handleMapSaveInput(s *mapState) {
	if rl.IsKeyPressed(rl.KeyEscape) {
		s.saveActive = false
		return
	}
	if rl.IsKeyPressed(rl.KeyEnter) {
		commitMapSave(s)
		return
	}
	if rl.IsKeyPressed(rl.KeyBackspace) && len(s.saveFilename) > 0 {
		r := []rune(s.saveFilename)
		s.saveFilename = string(r[:len(r)-1])
	}
	for c := rl.GetCharPressed(); c != 0; c = rl.GetCharPressed() {
		if len(s.saveFilename) < 48 {
			s.saveFilename += string(c)
		}
	}
}
