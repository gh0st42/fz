package main

import (
	"runtime"

	"github.com/gen2brain/raylib-go/raygui"
	rl "github.com/gen2brain/raylib-go/raylib"
)

// fixRetinaStartupScale works around a raylib/GLFW-Cocoa bug (raysan5/raylib#5059)
// where a window created directly on a Retina display — i.e. launched with no
// external monitor ever attached — gets the wrong backing-store scale baked
// into GetRenderWidth/Height at InitWindow time. Everything derived from that
// (mouse-to-canvas mapping, the canvas blit rect) ends up offset by roughly
// the title-bar height until some later event forces Cocoa to recompute it:
// moving the window to another display, or toggling fullscreen. This replays
// that fullscreen round-trip once at startup, before the window is shown to
// the user, so they don't have to do it by hand. Non-Retina Mac displays and
// external monitors report a 1:1 DPI scale and are left untouched; other
// platforms don't exhibit this bug at all.
func fixRetinaStartupScale() {
	if runtime.GOOS != "darwin" {
		return
	}
	if s := rl.GetWindowScaleDPI(); s.X <= 1 && s.Y <= 1 {
		return
	}
	rl.ToggleFullscreen()
	settleWindowFrames()
	rl.ToggleFullscreen()
	settleWindowFrames()
}

func settleWindowFrames() {
	for i := 0; i < 10; i++ {
		rl.BeginDrawing()
		rl.ClearBackground(rl.Black)
		rl.EndDrawing()
	}
}

// Toast is a transient notification shown near the bottom of the canvas.
type Toast struct {
	msg   string
	until float64
}

// Notify shows msg for 1.6 s with a fade-out.
func (t *Toast) Notify(msg string) {
	t.msg = msg
	t.until = float64(rl.GetTime()) + 1.6
}

// Draw renders the toast if it is still visible; call every frame on top of everything else.
func (t *Toast) Draw() {
	now := float64(rl.GetTime())
	if now >= t.until {
		return
	}
	remaining := t.until - now
	alpha := uint8(255)
	if remaining < 0.35 {
		alpha = uint8(remaining / 0.35 * 255)
	}
	msg := t.msg
	tw := rl.MeasureText(msg, 11)
	pw := tw + 24
	ph := int32(20)
	npx := (virtualW - int32(pw)) / 2
	npy := virtualH - statusBarH - ph - 5
	rl.DrawRectangle(npx, npy, int32(pw), ph, rl.NewColor(30, 58, 105, alpha))
	rl.DrawRectangleLines(npx, npy, int32(pw), ph, rl.NewColor(70, 118, 210, alpha))
	rl.DrawText(msg, npx+12, npy+4, 11, rl.NewColor(220, 235, 255, alpha))
}

// ── ConfirmDialog ─────────────────────────────────────────────────────────────

// ConfirmDialog is a modal overlay asking the user to confirm or cancel an action.
type ConfirmDialog struct {
	Active   bool
	msg      string
	yesLabel string
	noLabel  string
}

// Show opens the dialog. Ignored if already active.
func (d *ConfirmDialog) Show(msg, yesLabel, noLabel string) {
	if d.Active {
		return
	}
	d.Active = true
	d.msg = msg
	d.yesLabel = yesLabel
	d.noLabel = noLabel
}

// Draw renders the dialog when Active. Returns true once when the user confirms;
// dismisses on the cancel button or ESC.
func (d *ConfirmDialog) Draw() bool {
	if !d.Active {
		return false
	}
	rl.DrawRectangle(0, 0, virtualW, virtualH, rl.NewColor(0, 0, 0, 160))
	const dw, dh = int32(280), int32(90)
	dx := (virtualW - dw) / 2
	dy := (virtualH - dh) / 2
	rl.DrawRectangle(dx, dy, dw, dh, rl.NewColor(38, 38, 50, 255))
	rl.DrawRectangleLines(dx, dy, dw, dh, rl.NewColor(80, 100, 150, 255))
	tw := rl.MeasureText(d.msg, 10)
	rl.DrawText(d.msg, dx+(dw-tw)/2, dy+18, 10, rl.NewColor(200, 200, 210, 255))
	const bw = float32(90)
	const gap = float32(10)
	btnX := float32(dx) + (float32(dw)-bw*2-gap)/2
	btnY := float32(dy+dh) - 28
	if raygui.Button(rl.NewRectangle(btnX, btnY, bw, 20), d.yesLabel) {
		d.Active = false
		return true
	}
	if raygui.Button(rl.NewRectangle(btnX+bw+gap, btnY, bw, 20), d.noLabel) || rl.IsKeyPressed(rl.KeyEscape) {
		d.Active = false
	}
	return false
}
