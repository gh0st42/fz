# friendzone (fz)

A project management utility for [Love2D](https://love2d.org) games. Scaffolds new projects, builds distributable `.love` files, exports to the web via [love.js](https://github.com/Davidobot/love.js), and serves the result locally with the headers browsers require for `SharedArrayBuffer`.

## Installation

**From source**

```
go install github.com/gh0st42/friendzone@latest
```

or clone and build:

```
git clone <repo>
cd friendzone
make install      # go install .
```

**Pre-built binaries**

```
make release
```

Produces binaries for all supported platforms in `dist/release/`:

| File | Platform |
|---|---|
| `fz-<version>-darwin-arm64` | macOS (Apple Silicon) |
| `fz-<version>-darwin-amd64` | macOS (Intel) |
| `fz-<version>-linux-arm64` | Linux ARM64 |
| `fz-<version>-linux-amd64` | Linux x86-64 |
| `fz-<version>-windows-arm64.exe` | Windows ARM64 |
| `fz-<version>-windows-amd64.exe` | Windows x86-64 |

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

Builds a `.love` archive into `dist/`.

| Flag | Description |
|---|---|
| `--web` | Also builds a web export into `dist/www/` using love.js (requires Node.js / npx) |
| `--compat` | Passes `-c` to love.js, building without `SharedArrayBuffer`. Use only when deploying to hosts that block cross-origin isolation headers (e.g. older itch.io embeds) |

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

### `fz refresh`

Adds any template files that are missing from the current project. For files that already exist, asks interactively whether to replace them.

```
Replace .gitignore? [y/N]: n
Kept .gitignore
Added assets/tick.lua
Replace main.lua? [y/N]: y
Replaced main.lua
```

Project info (title, author) is read silently from the existing `conf.lua` and `main.lua` so you are not re-prompted unless those files are missing.

### `fz gfx [file]`

Opens a pixel-art sprite-sheet editor. `file` is resolved under `assets/gfx/`; omit it to start with a blank 256×256 sheet.

Sheets are saved as PNG files with an embedded `fz_meta` chunk that stores the tile size and per-tile flags. These files are directly usable as Tiled tilesets.

### `fz map [file]`

Opens a tile-map editor compatible with [Tiled](https://www.mapeditor.org/) `.tmj` / `.tsj` file formats. Run from the Love2D project root so that `assets/maps/` and `assets/gfx/` are resolved correctly.

```
cd mygame
fz map                              # new empty 32×32 map
fz map overworld.tmj                # open assets/maps/overworld.tmj
fz map /absolute/path/to/map.tmj   # open any TMJ by absolute path
```

**Supported Tiled features**

| Feature | Status |
|---|---|
| Tile layers (`tilelayer`) | ✓ |
| Object layers (`objectgroup`) | ✓ (objects preserved on load/save; not yet editable) |
| TSJ tileset with relative image path | ✓ |
| `firstgid` per tileset ref | ✓ |
| Raw (uncompressed) layer data | ✓ |

**Known limitations**

- **Multiple tilesets per map are not fully supported.** Only the first tileset entry is loaded. GIDs that belong to a second or later tileset will be looked up against the first sheet, producing wrong tiles. Maps with a single tileset work correctly.
- Base64-encoded or compressed layer data (Tiled's "zlib", "gzip", "zstd" options) is not decoded; those layers will appear empty.
- Object layer contents (points, rectangles, etc.) are round-tripped through the file but not displayed or editable in the map editor.

### `fz clean`

Removes the `dist/` directory.

### `fz version`

Prints the version and author.

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
