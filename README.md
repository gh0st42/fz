# friendzone (fz)

> **Personal tool.** This is built for my own hobby use — retro-inspired games and game jams. The workflows, defaults, and opinions baked in reflect my personal preferences (and ports/rewrites of old tools i have developed in the past). You're welcome to use or fork it, but don't expect a general-purpose tool.

´fz´ is a project management utility for [Love2D](https://love2d.org) games. 
Its features include amongst other things the following:
- Scaffolds new projects
- Builds distributable `.love` files
- Exports to the web via [love.js](https://github.com/Davidobot/love.js), and serves the result locally with the headers browsers require for `SharedArrayBuffer`
- A project runner including auto-(re)launching after file changes (watch)
- A tile-sheet editor (`fz gfx`)
- A tile-map editor (`fz map`)
- A Lua source editor (`fz edit`).

## Installation

**Pre-built binaries**

Download the latest binary for your platform from the [Releases](../../releases/latest) page:

| File | Platform |
|---|---|
| `fz-<version>-darwin-arm64` | macOS (Apple Silicon) |
| `fz-<version>-darwin-amd64` | macOS (Intel) |
| `fz-<version>-linux-arm64` | Linux ARM64 |
| `fz-<version>-linux-amd64` | Linux x86-64 |
| `fz-<version>-windows-amd64.exe` | Windows x86-64 |

Make the binary executable and place it somewhere on your `PATH`:

```
chmod +x fz-*-darwin-arm64
mv fz-*-darwin-arm64 /usr/local/bin/fz
```

**From source**

```
go install github.com/gh0st42/fz@latest
```

or clone and build:

```
git clone <repo>
cd fz
make install      # go install .
```

## Commands

### `fz new <name>`

Creates a new Love2D project directory. Prompts for game title and author, writes scaffolded files, and runs `git init` if git is available.

```
$ fz new mygame
Game title [mygame]: Space Shooter
Author [gh0st42]:
Initialized empty Git repository in mygame/.git/
Created Love2D project in mygame
```

### `fz init`

Same as `new` but initialises a project in the current directory.

### `fz build [--web] [--compat]`

Builds a `.love` archive into `dist/`. Common noise (`node_modules/`, `.git/`, `.DS_Store`, hidden files, editor swap files, `.love` files at the project root) is excluded automatically.

| Flag | Description |
|---|---|
| `--web` | Also builds a web export into `dist/www/` using love.js (requires Node.js / npx) |
| `--compat` | Passes `-c` to love.js, building without `SharedArrayBuffer`. Use only when deploying to hosts that block cross-origin isolation headers (e.g. older itch.io embeds) |
| `--portmaster` | Also builds a PortMaster zip (`dist/<project>.portmaster.zip`) for installation on Linux gaming handhelds |

**PortMaster target**

`fz build --portmaster` produces a zip that follows the PortMaster port layout:

```
dist/<project>.portmaster.zip
└── ports/
    ├── <project>.sh       ← launcher script
    └── <project>/
        ├── port.json      ← port metadata (name, author, runtime)
        └── <project>.love ← bundled game archive
```

The launcher script uses PortMaster's `pm_runtime` to run the game with the `love-11.5.aarch64.squashfs` runtime, which PortMaster downloads automatically on first launch. Game title and author are read from the project's `conf.lua` and `main.lua`.

To install, copy the zip to your handheld and extract it into the PortMaster ports folder (typically `/roms/ports/`). The port then appears in the PortMaster UI.

**`.distignore`**

Place a `.distignore` file in the project root to control which files and directories are included in the `.love` archive. The syntax is the same as `.gitignore`:

- Blank lines and lines starting with `#` are ignored.
- `*` matches any sequence of characters except `/`; `?` matches a single such character.
- `**` matches across directory boundaries (`assets/**/tmp` matches `assets/a/b/tmp`).
- A pattern without a `/` matches against the file or directory name at any depth (`*.psd` excludes all PSD files everywhere).
- A leading `/` anchors the pattern to the project root (`/secrets.lua` excludes only the top-level file).
- A trailing `/` matches directories only (`scratch/` excludes the whole directory tree).
- A leading `!` negates a previous rule and re-includes the matching path.
- Rules are evaluated in order; the last matching rule wins.

```
# exclude design sources and scratch work
*.psd
*.aseprite
scratch/

# exclude a specific top-level file
/TODO.txt

# keep one specific file that would otherwise be excluded
!assets/fonts/keep.txt
```

The hardcoded exclusions (`dist/`, `.git/`, `node_modules/`, hidden files, editor swap files) are always applied before `.distignore` rules and cannot be overridden.

### `fz serve [--port N]`

Starts a local HTTP server on `dist/www/` (default port 8000). Sets the two headers browsers require before exposing `SharedArrayBuffer`:

```
Cross-Origin-Opener-Policy: same-origin
Cross-Origin-Embedder-Policy: require-corp
```

Use this instead of `python3 -m http.server`, which does not set these headers.

### `fz run`

Launches the game in the current directory with `love` or `love2d`.

### `fz watch`

Watches `.lua` files and `assets/` for changes and automatically restarts the game.

### `fz refresh [--yes]`

Adds any template files that are missing from the current project. For files that already exist, shows the size delta and asks interactively whether to replace them. If `diff` is available on `PATH`, the `d` option shows a unified diff before asking again.

```
Replace conf.lua  [1.2KB → 1.4KB (+200B)]? [y/N/d]: d
--- conf.lua (current)
+++ conf.lua (template)
@@ -3,7 +3,7 @@
...
Replace conf.lua  [1.2KB → 1.4KB (+200B)]? [y/N/d]: y
Replaced conf.lua
Added assets/sfx/.keep
Kept  main.lua
```

| Flag | Description |
|---|---|
| `--yes` | Replace all existing files without prompting |

Project info (title, author) is read silently from the existing `conf.lua` and `main.lua` so you are not re-prompted unless those files are missing.

### `fz bgm new <name>` / `fz bgm edit <name>`

Manages BeepBox song files stored in the `bgm/` directory of your project.

```
fz bgm new theme        # creates bgm/theme.html and opens it in the browser
fz bgm edit theme       # opens bgm/theme.html in the browser
```

`fz bgm new` copies the bundled offline BeepBox editor to `bgm/<name>.html` and opens it in your default browser. Edit your song in BeepBox and use its export/share features to save state — BeepBox encodes the full song in the URL hash, so saving the page (Ctrl+S) or bookmarking the URL preserves your work. When you are happy with the result, use BeepBox's **Export** button to produce a `.wav` or `.mid` and place it in `assets/sfx/` or `assets/bgm/`.

`fz bgm edit` opens an already-created song file. The `.html` extension is optional for both subcommands.

### `fz gfx [file]`

Opens a pixel-art sprite-sheet editor. `file` is resolved under `assets/gfx/`; omit it to start with a blank 256×256 sheet.

Sheets are saved as PNG files with an embedded `fz_meta` chunk that stores the tile size and per-tile flags. These files are directly usable as Tiled tilesets.

**Keyboard shortcuts**

| Key | Action |
|---|---|
| P | Pencil |
| F | Fill / bucket |
| E | Eraser |
| H | Flip tile horizontal |
| V | Flip tile vertical |
| R / Alt+R | Rotate tile CW / CCW |
| Ctrl+C / X / V | Copy / Cut / Paste tile |
| Ctrl+D | Clear tile |
| Ctrl+Z / Y | Undo / Redo |
| Ctrl+S | Save |

### fz_meta — tile metadata embedded in PNG

`fz gfx` embeds metadata directly into the PNG file as a standard ancillary `tEXt` chunk. The file remains a fully valid PNG that any image editor can open; the extra chunk is simply ignored by software that doesn't know about it.

**Chunk layout**

A PNG `tEXt` chunk has this binary structure:

```
4 bytes  — data length (big-endian uint32)
4 bytes  — chunk type: "tEXt"  (0x74455874)
N bytes  — chunk data  (see below)
4 bytes  — CRC-32 of type + data
```

The chunk data is:

```
"fz_meta"  NUL  <JSON string>
  7 bytes   1    variable
```

The keyword `fz_meta` followed by a null byte (`0x00`) is the standard `tEXt` separator. Everything after it is UTF-8 JSON. The chunk is inserted just before the final `IEND` chunk.

**JSON structure**

```json
{"tile_size":16,"flags":{"0":2,"5":1,"12":255}}
```

| Field | Type | Description |
|---|---|---|
| `tile_size` | integer | Side length in pixels of each square tile (e.g. `8`, `16`, `32`) |
| `flags` | object, optional | Per-tile flag bitmasks. Omitted entirely when no flags are set. |

`flags` keys are **0-based tile indices** (as JSON strings). Values are unsigned 8-bit integers (0–255). Tiles with no flags set are absent from the map — a missing key is equivalent to `0`.

Tiles are indexed left-to-right, top-to-bottom:

```
tile 0  tile 1  tile 2  tile 3
tile 4  tile 5  tile 6  tile 7
...
```

**Flag bits**

Each tile has 8 independent flag bits (bits 0–7, LSB first). Their meaning is entirely up to the game — common uses are "solid", "deadly", "animated", "climbable", etc. In the `fz gfx` editor, keys `0`–`7` toggle the corresponding bit on the selected tile; the current bitmask is shown in the status bar as `Flags: 0x__`.

```
bit 0  key 0  (value 0x01)
bit 1  key 1  (value 0x02)
bit 2  key 2  (value 0x04)
bit 3  key 3  (value 0x08)
bit 4  key 4  (value 0x10)
bit 5  key 5  (value 0x20)
bit 6  key 6  (value 0x40)
bit 7  key 7  (value 0x80)
```

This mirrors the PICO-8 `fget(n, f)` / `fset(n, f, v)` API.

**Reading from Lua (retrolib)**

```lua
local meta = retrolib.load_tile_meta("assets/gfx/tiles.png")
-- meta.tile_size → 16
-- meta.flags[5]  → 1   (raw bitmask for tile 5)

if retrolib.fget(meta, 5, 0) then
  -- bit 0 is set on tile 5
end
```

`retrolib.load_tile_meta` requires `lib/3pp/json.lua` to decode the JSON. `retrolib.fget(meta, tile_id, bit_index)` tests a single bit.

**Reading from Go**

```go
jsonStr, err := readPNGMeta("assets/gfx/tiles.png")
var meta tileMeta
json.Unmarshal([]byte(jsonStr), &meta)
// meta.TileSize → 16
// meta.Flags["5"] → 1
```

### `fz map [file]`

Opens a tile-map editor compatible with [Tiled](https://www.mapeditor.org/) `.tmj` / `.tsj` file formats. Run from the Love2D project root so that `assets/maps/` and `assets/gfx/` are resolved correctly.

```
cd mygame
fz map                              # new empty 32×32 map
fz map overworld.tmj                # open assets/maps/overworld.tmj
fz map /absolute/path/to/map.tmj   # open any TMJ by absolute path
```

**Keyboard shortcuts**

| Key | Action |
|---|---|
| WASD / Arrows | Scroll map |
| Mouse wheel | Scroll vertically |
| Shift+Wheel | Scroll horizontally |
| + / − | Zoom in / out (1×–4×) |
| Ctrl+Wheel | Zoom in / out over viewport |
| P | Pencil |
| F | Fill / bucket |
| E | Eraser |
| H | Flip tile horizontal |
| V | Flip tile vertical |
| R / Shift+R | Rotate tile CW / CCW |
| Right-click | Pick tile + transform from map |
| G | Toggle grid |
| Tab | Toggle focus mode (hides UI, full-screen viewport) |
| M | Toggle minimap |
| Page Up / Down | Cycle active layer |
| Space | Toggle active layer visibility |
| Ctrl+S | Save |
| Ctrl+Z / Y | Undo / Redo |
| F1 | Keyboard shortcut reference |

**Supported Tiled features**

| Feature | Status |
|---|---|
| Tile layers (`tilelayer`) | ✓ |
| Object layers (`objectgroup`) | ✓ |
| Per-tile flip and rotation flags | ✓ |
| TSJ tileset with relative image path | ✓ |
| `firstgid` per tileset ref | ✓ |
| Raw (uncompressed) layer data | ✓ |
| Round-trip of unknown TMJ/TSJ fields | ✓ |

**Known limitations**

- **Multiple tilesets per map are not fully supported.** Only the first tileset entry is loaded. GIDs that belong to a second or later tileset will be looked up against the first sheet, producing wrong tiles. Maps with a single tileset work correctly.
- Base64-encoded or compressed layer data (Tiled's "zlib", "gzip", "zstd" options) is not decoded; those layers will appear empty.

### `fz edit [file]`

Opens a Lua source editor built on the same virtual 640×480 canvas and raygui widgets as `fz gfx` and `fz map`, styled after Turbo Pascal / QBasic / PICO-8: a menu bar with red hotkey letters, an EGA-blue editing window and a key-hint status bar.

```
cd mygame
fz edit                 # opens the project's main.lua
fz edit conf            # .lua is appended automatically
fz edit retrolib        # bare names fall back to lib/retrolib.lua
fz edit newthing        # a name that does not exist yet starts a new file
```

With no argument and no `main.lua` in the current directory there is no project to edit, so the editor says so and exits rather than opening on an empty buffer:

```
$ fz edit
error: no main.lua in this directory
Run 'fz init' to set up a project here, or 'fz new <name>' to create one
```

Lua syntax highlighting covers keywords, standard-library and Love2D globals, strings (including `[[long]]` brackets), numbers and comments. Text is drawn on a fixed character grid with the bundled unscii-8 bitmap font.

`Alt+F2` opens an outline of the file: every `function f()`, `local function f()`, `function M.f()` / `M:f()` and `f = function()` declaration, sorted by name and shown with its line number. Only the bare name is listed; scope shows in the colour instead — yellow for top-level functions visible to the whole project, cyan for top-level `local` ones, grey for functions declared inside another function, and white for the `(top of file)` entry. A nested one also names the top-level function it belongs to in its own column, however deeply it is nested. Declarations inside comments or long strings are skipped. The list always starts with a `(top of file)` entry, so it is never empty and there is always a jump back to line 1. The function around the cursor is preselected, and Enter (or **Go To**) jumps to it.

**Fonts.** Two faces are embedded and switchable from the **Options** menu. Each renders at one fixed size — the one where it lands on the pixel grid — and there is no font zoom:

| Font | Cell | Window |
|---|---|---|
| unscii-8 (default) | 8×12 | 72×34 |
| DOS VGA | 9×16 | 63×25 |

raylib scales a face by its ascent-descent, so what a given size yields is a property of the font's em. unscii-8 puts an 8×8 cell on a square em, so size 8 is its native cell and the 12px pitch just adds leading. Perfect DOS VGA 437 puts the 9×16 cell of real IBM VGA text mode on a 4096-unit em with exactly 256 units to the design pixel, so at size 16 one design pixel is one screen pixel: the advance comes out at exactly 9 and the ink at exactly 16, with every stem landing whole. Any other size rounds the advance and shaves a fraction off the stems, leaving the glyphs unevenly weighted however hard-edged the atlas is — which is why neither face is offered at any other size.

The atlas covers ASCII, Latin-1 and Latin Extended-A plus common punctuation and the euro sign, so umlauts and accented text render rather than turning into `?`. unscii-8 covers all of it; the DOS VGA face is a code page 437 font, so it has the umlauts and the rest of the accented characters DOS knew, but Latin Extended-A and the typographic punctuation fall back to `?`. The window is laid out from the character cell, so it re-flows when you switch face. The choice is not persisted. Both faces are rasterised with raylib's `FONT_BITMAP` mode, which thresholds glyph coverage instead of anti-aliasing it, so the pixels stay hard-edged.

**Code assistance.** With `lua-language-server` on PATH the editor talks to it for four things: symbol bounds (above), completion, inline help and signature hints.

- **Completion** — `Ctrl+Space`, and automatically after typing `.` or `:`. The popup filters as you keep typing, `↑`/`↓`/`PgUp`/`PgDn` move, `Enter` or `Tab` accepts as a single undoable edit, `Esc` dismisses. Without a language server it falls back to the identifiers already in the buffer plus Lua's own keywords, which is enough to finish a long name.
- **Inline help** — `Ctrl+I` shows what the server knows about the symbol under the caret: signature, return type and doc comment, with the markdown flattened to the editor's one font.
- **Signature hints** — typing `(` or `,` shows the call's parameter list on the line above the caret, with the argument you are currently filling in picked out in red. `)` dismisses it.

Every request runs on a goroutine and carries a sequence number, so a reply that arrives after the caret has moved on is discarded rather than shown. The server is started and left to finish its workspace scan in the background when a file is opened — hover, completion and signature help are semantic and answer with a "Workspace loading" placeholder until that scan completes, unlike formatting and symbols, which are syntactic and answer immediately. If the server dies it is restarted on the next request rather than disabling assistance for the session.

**Problems.** The language server publishes syntax errors and lints for the buffer as it is edited — the editor pushes the text 0.4 s after the typing settles rather than on every keystroke. Reported ranges are underlined and their line numbers coloured, red for errors and yellow for lints. Putting the caret on a reported line replaces the key hints in the status bar with the message; otherwise a short tally sits next to the position readout, `2E 3L` for two errors and three lints. `Ctrl+E` (or **Search → Next Problem**) jumps to the next one, wrapping.

**Turning it off.** **Options → Language Server** switches the whole thing off, including the resident server process, which is worth doing on a machine where indexing a workspace costs more than the help is worth. With it off, completion falls back to the identifiers in the buffer, function bounds fall back to the line scanner, and diagnostics, hover and signature hints go away. Formatting still works if `stylua` is installed, since that is a separate program — with the server off it takes over automatically.

**Function focus.** `F8` (or **View → Function Focus**) narrows the window to one function at a time, the way QBasic and VB showed a single procedure. The buffer still holds the whole file — focus is only a view — so line numbers stay real, the status bar keeps counting the whole document, and saving writes everything. `Alt+F2` switches between functions from the outline, and picking `(top of file)` steps back out to the module-level view. Putting the caret outside every function shows the whole file, which is how you navigate back in.

The declaration and its closing `end` are read-only and tinted to say so: only the body and the text inside the parentheses can change, so a function cannot lose its shape from the inside. Line-wise operations (indent, comment toggle) skip the sealed lines rather than refusing outright. A comment block sitting directly above the declaration comes along with the function, so it arrives with its documentation.

Bounds come from a Lua block scanner that counts `function` / `if` / `do` against `end`, consulting the syntax highlighter so keywords inside comments and strings do not count. When `lua-language-server` is installed, its `textDocument/documentSymbol` ranges are used instead; the request runs on a goroutine and the scanner's answer stands until it lands, so nothing waits on the server.

**Running, and what happens when it breaks.** `F5` saves and launches the game, and the editor reads its output. When Lua reports an error the editor opens the file it names, puts the caret on the line, marks it red in the gutter and shows the message in the status bar — no hunting through a terminal. The error is kept apart from the language server's findings, so a later diagnostics publish cannot wipe it out; the next run clears it.

One caveat worth knowing: love block-buffers its output when it is going to a pipe rather than a terminal, so nothing arrives while the crashed game is still on screen — the editor jumps to the fault the moment you close it. The game's output still reaches your terminal as before, and both streams are read, because love is not consistent about which one an error goes to.

**Project and tools.** The **Project** menu runs the game (`F5`, same as `fz run`) and builds it (`F9`), calling `fz`'s own build straight from the editor — the archive is written on a background goroutine so the window keeps responding, and the result is reported in the status toast.

The **Tools** menu is nine slots bound to `Ctrl+1` … `Ctrl+9`. The first two are the sprite (`fz gfx`) and map (`fz map`) editors, which ship with fz and cannot be changed or removed; raylib allows one window per process, so each opens as its own `fz` process in the same working directory. The remaining slots are yours.

**Tools → Configure...** lists every slot with its shortcut, built-in ones dimmed. `Insert` or **Add** appends a tool, `Delete` or **Remove** drops the selected one, and `Enter` or **Run** launches it. Add takes a command line, optionally named:

```
Lint = luacheck %f
stylua %f
git diff
```

Without a `Name =` prefix the command's first word names the tool. Four placeholders are substituted at launch: `%f` the file being edited, `%d` its directory, `%l` the cursor's line, and `%%` a literal percent. Commands run through the shell (`sh -c`, or `cmd /c` on Windows), so pipes, globs and variables work as they would at a prompt, and they run detached with their output going to the terminal the editor was started from — the editor does not wait for them.

Nine is the whole list rather than a page size: a tool past `Ctrl+9` would be a menu entry no shortcut could reach. Removing a tool shifts the ones below it up, so their shortcuts change with them.

Your tools are stored per project in `.fz/tools.json`, next to the game rather than in your home directory, because the commands worth binding are usually the project's own — so they survive restarts and can be committed with it. Only your tools are written; the built-ins come from the binary. A missing or malformed file is not an error, and the editor just starts with the two built-ins.

**Search and replace.** `Ctrl+F` finds, `F4` repeats, and `Ctrl+H` replaces. The replace dialog has both fields at once — `Tab` or a click moves between them — with **Replace** for the highlighted match, **All** for every match, and `Enter` as a shortcut for Replace so you can walk a file without touching the mouse. Matching is case-insensitive like Find, replacements go in literally, and everything is confined to what the view currently shows, so a replace-all inside function focus cannot reach past the function on screen or rewrite its sealed declaration. Opening the dialog with a selection spanning several lines scopes **All** to those lines, which the title says. A replace-all is one undo step; one that matches nothing leaves no undo step at all.

**Line editing.** `Ctrl+D` duplicates the caret's line or the selected block, `Alt+Up` / `Alt+Down` move it. Double-clicking selects the word under the pointer. Typing an opening bracket or quote writes its partner and puts the caret between them — typing the closer steps over it instead of doubling it, backspace between an empty pair takes both, wrapping a selection in brackets keeps the selection, and an apostrophe inside a word stays an apostrophe. **Options → Auto-close Brackets** turns that off for those who would rather it did not.

**Indentation.** The indent step is read from the file itself on load — every place a line is indented deeper than the one before votes for that width, and the most-voted wins — so a four-space project keeps four spaces without being told. **Options → Indent Width** cycles 2, 4, 8 for the current buffer.

**Block structure.** Putting the caret on `function`, `if`, `do`, `repeat`, `end` or `until` boxes both that keyword and the one it pairs with, so a stray `end` can be traced to the block it closes — the classic Lua mistake. Landing on `for` or `while` matches the `end` of the block their `do` opens, which is what you actually want to see. Keywords inside comments and strings do not count, since the matcher reads the syntax highlighter's tokens. Faint rules run down each level of indentation, continuing through blank lines inside a block rather than dashing; **View → Indent Guides** turns them off.

**Comments.** `Ctrl` `/`, `Ctrl` `-` and `Ctrl` `B` all toggle Lua line comments (three bindings because which physical key yields `/` or `-` depends on the keyboard layout). With a selection it works across every line in it: if all of them are already commented the markers come off, otherwise `-- ` goes on, aligned to the shallowest indentation in the block so a mixed selection round-trips. Blank lines are left alone, a selection resting at column 0 does not reach that last line, and the caret and selection are kept over the same text.

**Buffers.** Several files can be open at once. `F6` / `Alt+F6` cycle forward and back, `Alt+0` opens the buffer list, and `^W` closes the current buffer (the last one closing leaves an empty scratch buffer rather than exiting). Opening a file that is already open switches to it instead of loading a second copy. The frame title shows `[2/4] name.lua *` — position, file, and a marker when it has unsaved changes — and quitting warns if *any* buffer is dirty. The marker tracks the text rather than the fact that you typed: editing and then undoing back to the saved content, by hand or with `Ctrl+Z`, clears it again. The **Window** menu holds the same commands plus the list, whose **Close** button acts on the highlighted buffer.

**Formatting.** `F7` (or **Edit → Format**) reformats the buffer in place as one undoable edit, keeping the cursor on its line and column. Two backends are possible and the choice is made per format, not fixed at startup:

- `lua-language-server` over the LSP session (`textDocument/formatting`) is used when it is available and enabled — it is already running, and it formats to the same rules it lints by.
- [stylua](https://github.com/JohnnyMorganz/StyLua) is used when **Options → Format with stylua** is ticked, and stands in whenever the server is unavailable or switched off. Worth forcing on a project that keeps a `stylua.toml`, since stylua reads it.

Saving formats first, so what lands on disk is what the formatter would produce and a file never drifts between saves — **Options → Format on Save** turns that off. That step is deliberately synchronous, since the point is that the bytes being written are the formatted ones; if the formatter fails, which usually means a syntax error, or takes more than 1.5 s, the save goes ahead with the text as it stands and says so. It is one undo step, and only applies to `.lua` files.

With neither installed, `F7` says so and changes nothing; the override does nothing if stylua is not installed. The formatter in force is named at the bottom of the `F1` help page, and the toast after a format names the tool that actually did the work. Formatting runs off the render loop, so a slow tool never freezes the editor.

**Keyboard shortcuts**

| Key | Action |
|---|---|
| F2 / Ctrl+S | Save |
| F12 / Ctrl+Shift+S | Save As |
| F3 / Ctrl+O | Open |
| Ctrl+N | New buffer |
| F5 | Save and launch the game |
| Ctrl+Q | Quit |
| Ctrl+Z / Y | Undo / Redo |
| Ctrl+X / C / V | Cut / Copy / Paste |
| Ctrl+A | Select all |
| Ctrl+F / F4 | Find / Find next |
| Ctrl+G | Go to line |
| Alt+F2 | Function outline — all functions in the file, sorted by name; Enter jumps |
| F7 | Format the document (language server, or stylua) |
| F6 / Alt+F6 | Next / previous buffer |
| Alt+0 | Buffer list |
| Alt++ / Alt+- | Step the text size up / down |
| Ctrl+H | Replace |
| Ctrl+D | Duplicate line or selection |
| Alt+Up / Alt+Down | Move line or selection |
| Ctrl+/ , Ctrl+- , Ctrl+B | Toggle line comments |
| F9 | Build the project (`fz build`) |
| F8 | Function focus — show one function at a time |
| Ctrl+Space | Complete the word at the caret |
| Ctrl+I | Inline help for the symbol at the caret |
| Ctrl+E | Jump to the next problem |
| Ctrl+W | Close buffer |
| Tab / Shift+Tab | Indent / unindent line or selection |
| Shift+arrows | Extend selection |
| Ctrl+←/→ | Word jump |
| Ctrl+Home / End | Start / end of buffer |
| F10 / Alt+key | Open the menu bar |
| F1 | Keyboard shortcut reference |

### `fz about`

Prints the tool's MIT license and full attribution for every bundled dependency: the Go libraries, the vendored C libraries (raylib, GLFW), and the Lua libraries shipped into new projects.

### `fz clean`

Removes the `dist/` directory.

### `fz update`

Checks the latest release on GitHub and, if a newer version is available, downloads the matching binary for the current platform and replaces the running executable in-place.

```
$ fz update
New version available: 0.3.1 → 0.4.0
Downloading fz-v0.4.0-darwin-arm64 ...
Updated to 0.4.0.
```

The binary must be writable by the current user. On systems where it lives in a protected directory (e.g. `/usr/local/bin` installed by root), run with `sudo`. On Windows the current binary is renamed to `fz.exe.old` before the new one is placed — the old file can be deleted afterwards.

### `fz version`

Prints the version and author.

## retrolib

New projects include `lib/retrolib.lua`, a LÖVE2D helper library with a PICO-8-inspired API:

| Function | Description |
|---|---|
| `retrolib.main(init, update, draw)` | Set up Love2D callbacks and run the game loop with a virtual-resolution canvas |
| `retrolib.btn(btn)` / `retrolib.btnp(btn)` | Held / just-pressed button test (keyboard + gamepad) |
| `retrolib.draw_sprite(tex, id, x, y, sz)` | Draw a tile from a sprite sheet by 0-based tile index |
| `retrolib.load_tile_meta(path)` | Parse the `fz_meta` PNG chunk; returns `{tile_size, flags}` |
| `retrolib.fget(meta, tile_id, bit)` | Test a flag bit on a tile (mirrors PICO-8 `fget`) |
| `retrolib.sfx(path)` | Play a sound effect (cached after first load) |
| `retrolib.bgm(path, loop)` | Start background music; calling again with the same path is a no-op |
| `retrolib.bgm_stop()` | Stop background music |
| `retrolib.bgm_pause(bool)` | Pause / resume background music |
| `retrolib.begin_mode_2d(cam)` / `retrolib.end_mode_2d()` | Push / pop a 2D camera transform |

Pre-defined button constants: `BTN_UP`, `BTN_DOWN`, `BTN_LEFT`, `BTN_RIGHT`, `BTN_A`, `BTN_B`, `BTN_X`, `BTN_Y`.

## Custom templates (`FZ_TEMPLATE`)

By default `fz` uses the templates bundled in the binary. Set `FZ_TEMPLATE` to override them.

**Local directory**

```
FZ_TEMPLATE=/path/to/my-template fz new mygame
```

**Git repository**

```
FZ_TEMPLATE=https://github.com/example/fz-template fz new mygame
```

The repository is cloned with `--depth=1` into a temporary directory and removed afterwards. `git` must be available in `PATH`.

### Template structure

A custom template directory mirrors the layout of a Love2D project. Any file named `main.lua` or `conf.lua` is rendered as a [Go text/template](https://pkg.go.dev/text/template) with the following fields:

| Field | Example |
|---|---|
| `{{.Title}}` | `Space Shooter` |
| `{{.Identity}}` | `space_shooter` |
| `{{.Author}}` | `gh0st42` |

All other files are copied verbatim, so third-party libraries placed in `assets/` or `lib/3pp/` are never modified.

Empty directories can be preserved in the template by adding a `.keep` file; `fz` creates the directory but does not copy `.keep` into the project.
