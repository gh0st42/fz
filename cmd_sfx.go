package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gen2brain/raylib-go/raygui"
	rl "github.com/gen2brain/raylib-go/raylib"
)

// fz sfx is a sound-effect generator in the sfxr/bfxr line, on the same
// 640x480 canvas and raygui widgets as the other fz editors. The window is
// three columns: the sound's controls on the left, the parameter sliders in
// the middle, and a browser on the right listing the preset generators above
// the project's own sounds, so both are one click from being tweaked.

// ── layout ────────────────────────────────────────────────────────────────────

const (
	sfxLeftX = int32(6)
	sfxLeftW = int32(126)

	sfxMidX = sfxLeftX + sfxLeftW + 6 // 138
	sfxMidW = int32(292)

	sfxListX = sfxMidX + sfxMidW + 6 // 436
	sfxListW = virtualW - sfxListX - 6

	sfxTopY = toolbarH + 4 // 32

	// One slider and one section heading. Twenty-two sliders and six headings
	// have to fit between sfxTopY and the status bar, which is what fixes
	// these at 15 and 13 rather than anything rounder.
	sfxRowH = int32(15)
	sfxHdrH = int32(13)

	// Inside a slider row: the name, then the bar, then the value.
	sfxLabelW = int32(78)
	sfxValueW = int32(38)

	sfxWaveBtn = int32(60) // wave-type buttons, two per row

	sfxScopeH = int32(56) // waveform preview above the browser
)

// ── colours ───────────────────────────────────────────────────────────────────

var (
	sfxBg        = rl.NewColor(24, 26, 34, 255)
	sfxPanelBg   = rl.NewColor(34, 37, 48, 255)
	sfxPanelLine = rl.NewColor(70, 76, 96, 255)
	sfxHeading   = rl.NewColor(120, 200, 255, 255)
	sfxText      = rl.NewColor(200, 205, 220, 255)
	sfxDim       = rl.NewColor(130, 136, 155, 255)
	sfxScopeInk  = rl.NewColor(120, 230, 150, 255)
	sfxScopeAxis = rl.NewColor(60, 70, 90, 255)
	sfxPresetInk = rl.NewColor(255, 210, 110, 255)
)

// ── state ─────────────────────────────────────────────────────────────────────

// sfxRow is one line of the browser: either a preset generator or a saved
// parameter set. Presets are kept in the same list as files because both are
// starting points, and the ask was to be able to reach either in one click.
type sfxRow struct {
	label  string
	preset int    // index into sfxPresets, or -1
	file   string // base name under assets/sfx, or ""
}

type sfxState struct {
	params sfxParams
	name   string // base name, without extension

	// The rendered sound, kept so the scope and the exporters all describe
	// exactly what was last heard.
	samples []float32
	peak    float32 // loudest sample, for the scope; never zero
	sound   rl.Sound
	haveSnd bool
	audioOK bool

	rows      []sfxRow
	rowsTop   int32 // first visible row
	rowActive int32
	fileCount int // how many rows came from assets/sfx

	dirty    bool // parameters changed since the last save
	autoPlay bool // play whenever the sound is regenerated
	nameEdit bool
	nameBuf  string

	rng   sfxRand
	toast Toast

	exitConfirm ConfirmDialog
	wantQuit    bool
}

// ── entry point ───────────────────────────────────────────────────────────────

func runSfx(args []string) error {
	var name string
	if len(args) > 0 {
		name = strings.TrimSuffix(filepath.Base(args[0]), filepath.Ext(args[0]))
	}

	rl.SetConfigFlags(rl.FlagWindowResizable | rl.FlagWindowHighdpi)
	rl.InitWindow(virtualW*2, virtualH*2, "fz sfx")
	fixRetinaStartupScale()
	rl.SetTargetFPS(60)
	defer rl.CloseWindow()

	rl.InitAudioDevice()
	defer rl.CloseAudioDevice()

	s := &sfxState{
		params:   sfxDefaults(),
		name:     "sound",
		autoPlay: true,
		rng:      newSfxRand(),
		audioOK:  rl.IsAudioDeviceReady(),
	}
	if !s.audioOK {
		s.toast.Notify("No audio device - export still works")
	}
	s.refreshRows()

	// Named a sound that already exists, open it; named a new one, start from
	// the default parameters under that name.
	if name != "" {
		s.name = name
		if p, err := sfxLoadJSON(sfxPath(name, ".json")); err == nil {
			s.params = p
		}
	}
	s.nameBuf = s.name
	if os.Getenv("FZ_SHOT") != "" {
		s.rowActive = 2
		s.params = sfxExplosion(s.rng)
		s.autoPlay = false
	}
	s.regenerate(false)
	defer s.unloadSound()

	canvas := rl.LoadRenderTexture(virtualW, virtualH)
	defer rl.UnloadRenderTexture(canvas)

	rl.SetExitKey(0)
	shotFrame := 0
	running := true
	osClosed := false

	for running {
		if rl.WindowShouldClose() && !osClosed {
			osClosed = true
			if s.dirty {
				s.exitConfirm.Show("Unsaved changes — quit anyway?", "Quit", "Cancel")
			} else {
				running = false
			}
		}

		scale, offsetX, offsetY := virtualScale()
		dpi := float32(rl.GetRenderWidth()) / float32(rl.GetScreenWidth())
		screenScale := scale / dpi
		screenOffX, screenOffY := offsetX/dpi, offsetY/dpi

		rl.SetMouseOffset(int(-screenOffX), int(-screenOffY))
		rl.SetMouseScale(1/screenScale, 1/screenScale)

		sfxInput(s)

		rl.BeginTextureMode(canvas)
		sfxDrawScene(s)
		rl.EndTextureMode()

		shotFrame++
		if shot := os.Getenv("FZ_SHOT"); shot != "" && shotFrame > 20 {
			img := rl.LoadImageFromTexture(canvas.Texture)
			rl.ImageFlipVertical(img)
			rl.ExportImage(*img, shot)
			return nil
		}

		if s.wantQuit {
			running = false
		}

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

// ── sound ─────────────────────────────────────────────────────────────────────

func (s *sfxState) unloadSound() {
	if s.haveSnd {
		rl.UnloadSound(s.sound)
		s.haveSnd = false
	}
}

// regenerate re-renders the sound from the current parameters and hands it to
// the audio device. The preview goes through the same WAV encoder as the
// exporter, so what you hear is byte-for-byte what gets written.
func (s *sfxState) regenerate(play bool) {
	s.samples = s.params.render()
	s.peak = sfxPeak(s.samples)
	if !s.audioOK {
		return
	}
	wav := sfxEncodeWAV(s.samples)
	w := rl.LoadWaveFromMemory(".wav", wav, int32(len(wav)))
	s.unloadSound()
	s.sound = rl.LoadSoundFromWave(w)
	rl.UnloadWave(w)
	s.haveSnd = true
	if play || s.autoPlay {
		rl.PlaySound(s.sound)
	}
}

func (s *sfxState) play() {
	if s.haveSnd {
		rl.PlaySound(s.sound)
	}
}

// setParams installs a new sound, marking it unsaved and previewing it.
func (s *sfxState) setParams(p sfxParams) {
	s.params = p
	s.dirty = true
	s.regenerate(true)
}

// ── browser ───────────────────────────────────────────────────────────────────

// refreshRows rebuilds the browser: the generators first, then whatever JSON
// the project has under assets/sfx.
func (s *sfxState) refreshRows() {
	keep := ""
	if int(s.rowActive) < len(s.rows) {
		keep = s.rows[s.rowActive].label
	}

	s.rows = s.rows[:0]
	for i, p := range sfxPresets {
		s.rows = append(s.rows, sfxRow{label: p.name, preset: i, file: ""})
	}
	files := sfxListJSON()
	s.fileCount = len(files)
	for _, f := range files {
		s.rows = append(s.rows, sfxRow{label: f, preset: -1, file: f})
	}

	s.rowActive = 0
	for i, r := range s.rows {
		if r.label == keep {
			s.rowActive = int32(i)
			break
		}
	}
	s.followRow()
}

func sfxListRows() int32 {
	return (sfxListViewH() - 2) / sfxRowH
}

func sfxListViewH() int32 {
	top := sfxTopY + sfxScopeH + 6 + sfxHdrH
	return virtualH - statusBarH - 6 - top
}

func sfxListViewY() int32 { return sfxTopY + sfxScopeH + 6 + sfxHdrH }

// followRow scrolls the browser so the selection stays on screen.
func (s *sfxState) followRow() {
	vis := sfxListRows()
	if s.rowActive < s.rowsTop {
		s.rowsTop = s.rowActive
	}
	if s.rowActive >= s.rowsTop+vis {
		s.rowsTop = s.rowActive - vis + 1
	}
	s.rowsTop = int32(max(0, min(int(s.rowsTop), max(0, len(s.rows)-int(vis)))))
}

// activateRow runs the selected generator, or loads the selected file.
func (s *sfxState) activateRow() {
	if int(s.rowActive) >= len(s.rows) {
		return
	}
	r := s.rows[s.rowActive]
	if r.preset >= 0 {
		s.setParams(sfxPresets[r.preset].gen(s.rng))
		s.toast.Notify(sfxPresets[r.preset].name)
		return
	}
	p, err := sfxLoadJSON(sfxPath(r.file, ".json"))
	if err != nil {
		s.toast.Notify("Load failed: " + err.Error())
		return
	}
	s.params = p
	s.name, s.nameBuf = r.file, r.file
	s.dirty = false
	s.regenerate(true)
	s.toast.Notify("Loaded " + r.file)
}

// ── saving ────────────────────────────────────────────────────────────────────

// saveBoth writes the parameter set and the rendered WAV side by side. A sound
// is only useful to a game as audio, and only editable again as parameters, so
// the two are always written together rather than behind separate buttons.
func (s *sfxState) saveBoth() {
	name := strings.TrimSpace(s.nameBuf)
	name = strings.TrimSuffix(name, filepath.Ext(name))
	if name == "" {
		s.toast.Notify("Enter a name first")
		return
	}
	s.name = name

	if err := sfxSaveJSON(sfxPath(name, ".json"), s.params); err != nil {
		s.toast.Notify("Save failed: " + err.Error())
		return
	}
	if err := os.WriteFile(sfxPath(name, ".wav"), sfxEncodeWAV(s.samples), 0o644); err != nil {
		s.toast.Notify("WAV failed: " + err.Error())
		return
	}
	s.dirty = false
	s.refreshRows()
	s.toast.Notify("Saved " + name + ".json and .wav")
}

// ── input ─────────────────────────────────────────────────────────────────────

func sfxInput(s *sfxState) {
	if s.exitConfirm.Active {
		return // the dialog owns the keyboard; its buttons are drawn later
	}
	// While the name is being typed, the keyboard belongs to the text box.
	if s.nameEdit {
		return
	}

	switch {
	case rl.IsKeyPressed(rl.KeySpace):
		s.play()
	case rl.IsKeyPressed(rl.KeyR):
		s.setParams(sfxRandomize(s.rng))
		s.toast.Notify("Randomized")
	case rl.IsKeyPressed(rl.KeyM):
		s.setParams(sfxMutate(s.rng, s.params))
		s.toast.Notify("Mutated")
	case rl.IsKeyPressed(rl.KeyS) && (rl.IsKeyDown(rl.KeyLeftControl) || rl.IsKeyDown(rl.KeyLeftSuper)):
		s.saveBoth()
	case rl.IsKeyPressed(rl.KeyF5):
		s.refreshRows()
		s.toast.Notify("Reloaded assets/sfx")
	case rl.IsKeyPressed(rl.KeyDown):
		s.rowActive = int32(min(int(s.rowActive)+1, len(s.rows)-1))
		s.followRow()
	case rl.IsKeyPressed(rl.KeyUp):
		s.rowActive = int32(max(int(s.rowActive)-1, 0))
		s.followRow()
	case rl.IsKeyPressed(rl.KeyEnter), rl.IsKeyPressed(rl.KeyKpEnter):
		s.activateRow()
	case rl.IsKeyPressed(rl.KeyEscape):
		if s.dirty {
			s.exitConfirm.Show("Unsaved changes — quit anyway?", "Quit", "Cancel")
		} else {
			s.wantQuit = true
		}
	}
}

// ── drawing ───────────────────────────────────────────────────────────────────

func sfxDrawScene(s *sfxState) {
	rl.ClearBackground(sfxBg)

	sfxDrawToolbar(s)
	sfxDrawLeftPanel(s)
	sfxDrawSliders(s)
	sfxDrawScope(s)
	sfxDrawBrowser(s)
	sfxDrawStatusBar(s)
	s.toast.Draw()

	if s.exitConfirm.Draw() {
		s.wantQuit = true
	}
}

func sfxPanel(x, y, w, h int32, title string) {
	rl.DrawRectangle(x, y, w, h, sfxPanelBg)
	rl.DrawRectangleLines(x, y, w, h, sfxPanelLine)
	if title != "" {
		rl.DrawText(title, x+4, y-11, 10, sfxHeading)
	}
}

func sfxDrawToolbar(s *sfxState) {
	rl.DrawRectangle(0, 0, virtualW, toolbarH, sfxPanelBg)
	rl.DrawLine(0, toolbarH, virtualW, toolbarH, sfxPanelLine)
	rl.DrawText("fz sfx", 8, 9, 10, sfxHeading)

	name := s.name
	if s.dirty {
		name += " *"
	}
	rl.DrawText(name, 60, 9, 10, sfxText)

	// Transport lives in the toolbar so it is reachable from anywhere.
	bw, bh := float32(58), float32(18)
	y := float32(5)
	if raygui.Button(rl.NewRectangle(float32(virtualW)-4*bw-24, y, bw, bh), "Play") {
		s.play()
	}
	if raygui.Button(rl.NewRectangle(float32(virtualW)-3*bw-18, y, bw, bh), "Random") {
		s.setParams(sfxRandomize(s.rng))
	}
	if raygui.Button(rl.NewRectangle(float32(virtualW)-2*bw-12, y, bw, bh), "Mutate") {
		s.setParams(sfxMutate(s.rng, s.params))
	}
	if raygui.Button(rl.NewRectangle(float32(virtualW)-bw-6, y, bw, bh), "Save") {
		s.saveBoth()
	}
}

func sfxDrawLeftPanel(s *sfxState) {
	x, w := sfxLeftX, sfxLeftW
	y := sfxTopY

	// Waveform, two per row.
	rl.DrawText("WAVE", x, y, 10, sfxHeading)
	y += sfxHdrH
	for i, name := range sfxWaveNames {
		bx := x + int32(i%2)*(sfxWaveBtn+4)
		by := y + int32(i/2)*20
		on := s.params.Wave == sfxWave(i)
		if raygui.Toggle(rl.NewRectangle(float32(bx), float32(by), float32(sfxWaveBtn), 18), name, &on) && on {
			if s.params.Wave != sfxWave(i) {
				s.params.Wave = sfxWave(i)
				s.dirty = true
				s.regenerate(true)
			}
		}
	}
	y += 44

	// Master volume gets its own place rather than sitting among the sliders,
	// because it changes loudness rather than the shape of the sound.
	rl.DrawText("VOLUME", x, y, 10, sfxHeading)
	y += sfxHdrH
	vol := s.params.Volume
	raygui.Slider(rl.NewRectangle(float32(x), float32(y), float32(w), 14), "", "", &vol, 0, 1)
	if vol != s.params.Volume {
		s.params.Volume = vol
		s.dirty = true
		s.regenerate(false)
	}
	rl.DrawText(fmt.Sprintf("%.2f", s.params.Volume), x+w-26, y+2, 10, sfxDim)
	y += 22

	// Name and export.
	rl.DrawText("NAME", x, y, 10, sfxHeading)
	y += sfxHdrH
	box := rl.NewRectangle(float32(x), float32(y), float32(w), 18)
	if rl.CheckCollisionPointRec(rl.GetMousePosition(), box) && rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
		s.nameEdit = true
	}
	rl.DrawRectangleRec(box, sfxBg)
	if raygui.TextBox(box, &s.nameBuf, 64, s.nameEdit) && s.nameEdit {
		// raygui ends edit mode on ENTER or a click outside; only the key
		// should commit, so a stray click cannot trigger a write.
		if rl.IsKeyPressed(rl.KeyEnter) || rl.IsKeyPressed(rl.KeyKpEnter) {
			s.saveBoth()
		}
		s.nameEdit = false
	}
	y += 24

	if raygui.Button(rl.NewRectangle(float32(x), float32(y), float32(w), 18), "Save WAV + JSON") {
		s.saveBoth()
	}
	y += 22
	if raygui.Button(rl.NewRectangle(float32(x), float32(y), float32(w), 18), "Reset to default") {
		s.setParams(sfxDefaults())
	}
	y += 24

	auto := s.autoPlay
	if raygui.CheckBox(rl.NewRectangle(float32(x), float32(y), 12, 12), "Auto play", &auto) {
		s.autoPlay = auto
	}
	y += 20

	dur := float32(len(s.samples)) / float32(sfxSampleRate)
	rl.DrawText(fmt.Sprintf("%.2f s", dur), x, y, 10, sfxDim)
	rl.DrawText(fmt.Sprintf("%d smp", len(s.samples)), x, y+11, 10, sfxDim)
}

// sfxDrawSliders lays the parameter table out as labelled rows. Dragging any
// of them re-renders immediately, which is what makes the tool playable.
func sfxDrawSliders(s *sfxState) {
	x, w := sfxMidX, sfxMidW
	y := sfxTopY
	changed := false

	for _, spec := range sfxParamSpecs {
		if spec.group != "" {
			rl.DrawText(spec.group, x, y+2, 10, sfxHeading)
			y += sfxHdrH
		}
		v := spec.get(&s.params)
		before := *v

		rl.DrawText(spec.label, x, y+2, 10, sfxText)
		bar := rl.NewRectangle(
			float32(x+sfxLabelW), float32(y+1),
			float32(w-sfxLabelW-sfxValueW), float32(sfxRowH-3))

		// A signed parameter gets a centre tick, so "no sweep" is findable
		// without reading the number.
		if spec.min < 0 {
			cx := bar.X + bar.Width/2
			rl.DrawLine(int32(cx), int32(bar.Y), int32(cx), int32(bar.Y+bar.Height), sfxPanelLine)
		}
		raygui.Slider(bar, "", "", v, spec.min, spec.max)
		if *v != before {
			changed = true
		}
		rl.DrawText(fmt.Sprintf("%+.2f", *v), x+w-sfxValueW+2, y+2, 10, sfxDim)
		y += sfxRowH
	}

	if changed {
		s.dirty = true
		s.regenerate(false)
	}
}

// sfxDrawScope plots the rendered sound, decimated to one column per pixel so
// the shape of the envelope is visible at a glance.
func sfxDrawScope(s *sfxState) {
	x, y, w, h := sfxListX, sfxTopY, sfxListW, sfxScopeH
	sfxPanel(x, y, w, h, "")
	mid := y + h/2
	rl.DrawLine(x+1, mid, x+w-1, mid, sfxScopeAxis)

	if len(s.samples) == 0 {
		return
	}
	// The waveform peaks well below full scale at sfxr's trim, so the plot is
	// normalised against the loudest sample. Measured once when the sound is
	// rendered: it is a scan of every sample, and this draws every frame.
	scale := float32(h/2-2) / s.peak

	cols := int(w - 2)
	per := max(1, len(s.samples)/cols)
	for c := 0; c < cols; c++ {
		lo, hi := float32(0), float32(0)
		start := c * per
		for i := start; i < start+per && i < len(s.samples); i++ {
			lo = min(lo, s.samples[i])
			hi = max(hi, s.samples[i])
		}
		top := mid - int32(hi*scale)
		bot := mid - int32(lo*scale)
		rl.DrawLine(x+1+int32(c), top, x+1+int32(c), bot+1, sfxScopeInk)
	}
}

// sfxPeak returns the largest absolute sample, never zero, so it is safe to
// divide by.
func sfxPeak(samples []float32) float32 {
	peak := float32(0.0001)
	for _, v := range samples {
		if v < 0 {
			v = -v
		}
		if v > peak {
			peak = v
		}
	}
	return peak
}

func sfxDrawBrowser(s *sfxState) {
	x, w := sfxListX, sfxListW
	y := sfxTopY + sfxScopeH + 6

	rl.DrawText("PRESETS & assets/sfx", x, y, 10, sfxHeading)
	vy, vh := sfxListViewY(), sfxListViewH()
	sfxPanel(x, vy, w, vh, "")

	vis := int(sfxListRows())
	mouse := rl.GetMousePosition()
	rows := rl.NewRectangle(float32(x+1), float32(vy+1), float32(w-2), float32(vh-2))
	hover := -1
	if rl.CheckCollisionPointRec(mouse, rows) {
		if row := int((mouse.Y - rows.Y) / float32(sfxRowH)); row >= 0 && row < vis {
			if i := int(s.rowsTop) + row; i < len(s.rows) {
				hover = i
				if rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
					s.rowActive = int32(i)
					s.activateRow()
				}
			}
		}
		if wheel := rl.GetMouseWheelMove(); wheel != 0 {
			s.rowsTop = int32(max(0, min(int(s.rowsTop)-int(wheel*3), max(0, len(s.rows)-vis))))
		}
	}

	for r := 0; r < vis; r++ {
		i := int(s.rowsTop) + r
		if i >= len(s.rows) {
			break
		}
		ry := vy + 1 + int32(r)*sfxRowH
		fg := sfxText
		if s.rows[i].preset >= 0 {
			fg = sfxPresetInk
		} else if i > 0 && s.rows[i-1].preset >= 0 {
			rl.DrawLine(x+3, ry, x+w-3, ry, sfxPanelLine) // the presets end here
		}
		switch i {
		case int(s.rowActive):
			rl.DrawRectangle(x+1, ry, w-2, sfxRowH, sfxPanelLine)
		case hover:
			rl.DrawRectangle(x+1, ry, w-2, sfxRowH, rl.NewColor(50, 56, 72, 255))
		}
		rl.DrawText(s.rows[i].label, x+5, ry+3, 10, fg)
	}

	// A project with no sounds yet says so, rather than showing presets over
	// an unexplained empty half.
	if s.fileCount == 0 {
		rl.DrawText("(no .json in assets/sfx yet)", x+5, vy+vh-13, 10, sfxDim)
	}
}

func sfxDrawStatusBar(s *sfxState) {
	y := virtualH - statusBarH
	rl.DrawRectangle(0, y, virtualW, statusBarH, rl.NewColor(30, 30, 30, 255))
	rl.DrawLine(0, y, virtualW, y, rl.NewColor(60, 60, 60, 255))

	audio := ""
	if !s.audioOK {
		audio = "  |  no audio device"
	}
	rl.DrawText(fmt.Sprintf(
		"%s  %s  |  Space play  R random  M mutate  ^S save  F5 rescan  Esc quit%s",
		s.params.Wave, sfxDurationStr(s.samples), audio),
		4, y+5, 10, rl.LightGray)
}

func sfxDurationStr(samples []float32) string {
	return fmt.Sprintf("%.2fs", float32(len(samples))/float32(sfxSampleRate))
}
