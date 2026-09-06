package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// The Tools menu is nine slots bound to Ctrl+1..Ctrl+9. The first ones are the
// editors that ship with fz and cannot be changed; the rest the user fills with
// whatever commands the project needs, through the Tools > Configure dialog.
//
// Nine is the whole story, not a page size: a tool with no shortcut would be a
// menu entry that Ctrl+N could never reach, so the list simply stops there.

const (
	edToolSlots = 9

	// User tools live with the project rather than with the user, because the
	// commands worth binding are usually the project's own (its linter, its
	// asset pipeline) and are worth committing alongside it.
	edToolsFile = ".fz/tools.json"
)

// edTool is one entry in the Tools menu. Fixed tools re-invoke this same fz
// binary with a subcommand; user tools run a command line through the shell.
type edTool struct {
	Label string `json:"label"`
	Cmd   string `json:"cmd"`

	sub   string // fz subcommand, fixed tools only
	fixed bool   // ships with fz: not editable, not removable
}

// edFixedTools are always the first slots, in this order, so the shortcut for a
// built-in editor never moves when the user adds or removes their own.
var edFixedTools = []edTool{
	{Label: "Sprite Editor", sub: "gfx", fixed: true},
	{Label: "Map Editor", sub: "map", fixed: true},
	{Label: "Sound Editor", sub: "sfx", fixed: true},
}

// edTools is the live list: the fixed tools followed by the user's.
var edTools []edTool

// edLoadTools reads the project's tool file, dropping anything that will not
// fit in the remaining slots. A missing or unreadable file is not an error -
// the editor just starts with the built-ins.
func edLoadTools() {
	edTools = append([]edTool(nil), edFixedTools...)

	data, err := os.ReadFile(edToolsFile)
	if err != nil {
		edRebuildToolsMenu()
		return
	}
	var user []edTool
	if json.Unmarshal(data, &user) != nil {
		edRebuildToolsMenu()
		return
	}
	for _, t := range user {
		if len(edTools) >= edToolSlots {
			break
		}
		if strings.TrimSpace(t.Label) == "" || strings.TrimSpace(t.Cmd) == "" {
			continue
		}
		edTools = append(edTools, edTool{Label: t.Label, Cmd: t.Cmd})
	}
	edRebuildToolsMenu()
}

// edSaveTools writes the user's tools back. The fixed ones are not written:
// they belong to the binary, and persisting them would freeze today's list into
// every project that ever opened the dialog.
func edSaveTools() error {
	user := []edTool{}
	for _, t := range edTools {
		if !t.fixed {
			user = append(user, t)
		}
	}
	data, err := json.MarshalIndent(user, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(edToolsFile), 0o755); err != nil {
		return err
	}
	return os.WriteFile(edToolsFile, append(data, '\n'), 0o644)
}

// edRebuildToolsMenu regenerates the Tools drop-down from the live list. The
// other menus are static; this one is data, so it is rebuilt whenever the list
// changes rather than being written out by hand.
func edRebuildToolsMenu() {
	items := make([]edMenuItem, 0, len(edTools)+2)
	for i, t := range edTools {
		items = append(items, edMenuItem{t.Label, "^" + strconv.Itoa(i+1), edActTool1 + edAction(i)})
	}
	items = append(items,
		edMenuItem{"", "", edActNone},
		edMenuItem{"Configure...", "", edActTools})

	for i := range edMenus {
		if edMenus[i].title == "Tools" {
			edMenus[i].items = items
			return
		}
	}
}

// ── running ───────────────────────────────────────────────────────────────────

// edExpandTool substitutes the editor's context into a command line: %f is the
// file being edited, %d its directory and %l the cursor's line. %% is a
// literal percent.
func edExpandTool(s *edState, line string) string {
	abs, err := filepath.Abs(s.path)
	if err != nil {
		abs = s.path
	}
	r := strings.NewReplacer(
		"%%", "\x00",
		"%f", abs,
		"%d", filepath.Dir(abs),
		"%l", strconv.Itoa(s.cy+1),
	)
	return strings.ReplaceAll(r.Replace(line), "\x00", "%")
}

// edRunTool launches the tool in slot i, detached, so the editor keeps running.
func edRunTool(s *edState, i int) {
	if i < 0 || i >= len(edTools) {
		s.toast.Notify(fmt.Sprintf("No tool in slot %d", i+1))
		return
	}
	t := edTools[i]
	if t.fixed {
		edSpawnTool(s, strings.ToLower(t.Label), t.sub)
		return
	}

	// User commands go through the shell so a binding can use the pipes,
	// globs and variables the user would have typed at a prompt.
	line := edExpandTool(s, t.Cmd)
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", line)
	} else {
		cmd = exec.Command("sh", "-c", line)
	}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		s.toast.Notify("Could not run " + t.Label + ": " + err.Error())
		return
	}
	go func() { _ = cmd.Wait() }() // reap the child so it does not linger
	s.toast.Notify("Running " + t.Label)
}

// ── the configure dialog ──────────────────────────────────────────────────────

// edToolRow renders one slot for the list: its shortcut, its name, and either
// the command it runs or a note that it is built in.
func edToolRow(i int, t edTool) string {
	what := t.Cmd
	if t.fixed {
		what = "(built in)"
	}
	return fmt.Sprintf("^%d %-18s %s", i+1, edEllipsis(t.Label, 18), edEllipsis(what, 22))
}

func edShowToolsDialog(s *edState) {
	edFillToolList(s)
	s.dlgActive = 0
	s.dlgScroll = 0
	s.openDialog(edDlgTools)
}

func edFillToolList(s *edState) {
	s.dlgList = s.dlgList[:0]
	for i, t := range edTools {
		s.dlgList = append(s.dlgList, edToolRow(i, t))
	}
}

// edToolsRemove drops the selected user tool. The fixed ones stay put, so the
// slots below a removal shift up and their shortcuts change with them.
func edToolsRemove(s *edState) {
	i := int(s.dlgActive)
	if i < 0 || i >= len(edTools) {
		return
	}
	if edTools[i].fixed {
		s.toast.Notify(edTools[i].Label + " is built in")
		return
	}
	label := edTools[i].Label
	edTools = append(edTools[:i], edTools[i+1:]...)
	edRebuildToolsMenu()
	edFillToolList(s)
	s.dlgActive = int32(max(0, min(i, len(edTools)-1)))
	if err := edSaveTools(); err != nil {
		s.toast.Notify("Removed " + label + ", but could not save: " + err.Error())
		return
	}
	s.toast.Notify("Removed " + label)
}

// edToolsAdd opens the small input dialog that appends a slot.
func edToolsAdd(s *edState) {
	if len(edTools) >= edToolSlots {
		s.toast.Notify(fmt.Sprintf("All %d tool slots are in use", edToolSlots))
		return
	}
	s.dlgInput = ""
	s.openDialog(edDlgToolAdd)
}

// edParseTool splits "Name = command" into its two halves. Without the "=" the
// command stands for itself and its first word names the tool.
func edParseTool(text string) (edTool, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return edTool{}, false
	}
	label, cmd := "", text
	if i := strings.Index(text, "="); i >= 0 {
		label = strings.TrimSpace(text[:i])
		cmd = strings.TrimSpace(text[i+1:])
	}
	if cmd == "" {
		return edTool{}, false
	}
	if label == "" {
		label = strings.Fields(cmd)[0]
	}
	return edTool{Label: label, Cmd: cmd}, true
}

// edToolsAddConfirm takes what was typed into the add dialog. It returns to the
// tool list rather than closing outright, so several tools can be added in a row.
func edToolsAddConfirm(s *edState) {
	t, ok := edParseTool(s.dlgInput)
	if !ok {
		s.toast.Notify("Type a command, or 'Name = command'")
		return
	}
	if len(edTools) >= edToolSlots {
		s.toast.Notify(fmt.Sprintf("All %d tool slots are in use", edToolSlots))
		return
	}
	edTools = append(edTools, t)
	edRebuildToolsMenu()

	edFillToolList(s)
	s.dlgActive = int32(len(edTools) - 1)
	s.openDialog(edDlgTools)

	if err := edSaveTools(); err != nil {
		s.toast.Notify("Added " + t.Label + ", but could not save: " + err.Error())
		return
	}
	s.toast.Notify(fmt.Sprintf("Added %s as ^%d", t.Label, len(edTools)))
}
