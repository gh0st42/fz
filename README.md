# friendzone (fz)

> **Personal tool.** This is built for my own hobby use — retro-inspired games and game jams. The workflows, defaults, and opinions baked in reflect my personal preferences (and ports/rewrites of old tools i have developed in the past). You're welcome to use or fork it, but don't expect a general-purpose tool.

´fz´ is a project management utility for [Love2D](https://love2d.org) games. 
Its features include amongst other things the following:
- Scaffolds new projects
- Builds distributable `.love` files
- Exports to the web via [love.js](https://github.com/Davidobot/love.js), and serves the result locally with the headers browsers require for `SharedArrayBuffer`
- A project runner including auto-(re)launching after file changes (watch)
- A tile-sheet editor (`fz gfx`)
- A tile-map editor (`fz map`).

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
