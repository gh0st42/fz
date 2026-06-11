-- retrolib.lua  –  Retro game helpers for LÖVE2D
-- Drop-in spiritual port of retrolib.py
--
-- Usage:
--   local retrolib = require("retrolib")
--   retrolib.SCREEN_SCALE = 4
--   retrolib.main(init, update, draw)   -- sets love callbacks and runs

local retrolib             = {}

-- ─── Screen settings ──────────────────────────────────────────────────────────
retrolib.SCREEN_WIDTH      = 320
retrolib.SCREEN_HEIGHT     = 200
retrolib.SCREEN_TARGET_FPS = 60
retrolib.WINDOW_TITLE      = "Retrolib"
retrolib.SCREEN_SCALE      = 3

-- ─── Button definitions ───────────────────────────────────────────────────────
-- keyboard keys use Love2D string names; gamepad buttons use Love2D gamepad names
retrolib.BTN_UP            = { keyboard = { "up" }, gamepad = { "dpup" }, dx = 0, dy = -1 }
retrolib.BTN_DOWN          = { keyboard = { "down" }, gamepad = { "dpdown" }, dx = 0, dy = 1 }
retrolib.BTN_LEFT          = { keyboard = { "left" }, gamepad = { "dpleft" }, dx = -1, dy = 0 }
retrolib.BTN_RIGHT         = { keyboard = { "right" }, gamepad = { "dpright" }, dx = 1, dy = 0 }
retrolib.BTN_A             = { keyboard = { "return", "x" }, gamepad = { "a" }, dx = 0, dy = 0 }
retrolib.BTN_B             = { keyboard = { "rshift", "y", "z" }, gamepad = { "b" }, dx = 0, dy = 0 }
retrolib.BTN_X             = { keyboard = { "space", "s" }, gamepad = { "x" }, dx = 0, dy = 0 }
retrolib.BTN_Y             = { keyboard = { "tab", "a" }, gamepad = { "y" }, dx = 0, dy = 0 }

-- ─── Common colors (0-1 normalized, compatible with love.graphics.setColor) ──
retrolib.WHITE             = { 1, 1, 1, 1 }
retrolib.BLACK             = { 0, 0, 0, 1 }
retrolib.RED               = { 1, 0, 0, 1 }
retrolib.GREEN             = { 0, 1, 0, 1 }
retrolib.BLUE              = { 0, 0, 1, 1 }
retrolib.YELLOW            = { 1, 1, 0, 1 }

-- ─── Internal state ───────────────────────────────────────────────────────────
local _canvas              = nil
local _font_cache          = {}
local _pressed_keys        = {} -- keys pressed THIS frame
local _pressed_buttons     = {} -- gamepad buttons pressed THIS frame

-- ─── Input ────────────────────────────────────────────────────────────────────

--- Returns true while any key/button in btn_data is held.
function retrolib.btn(btn_data)
  for _, k in ipairs(btn_data.keyboard) do
    if love.keyboard.isDown(k) then return true end
  end
  local sticks = love.joystick.getJoysticks()
  if sticks[1] and sticks[1]:isGamepad() then
    for _, g in ipairs(btn_data.gamepad) do
      if sticks[1]:isGamepadDown(g) then return true end
    end
  end
  return false
end

--- Returns true only on the first frame any key/button in btn_data was pressed.
function retrolib.btnp(btn_data)
  for _, k in ipairs(btn_data.keyboard) do
    if _pressed_keys[k] then return true end
  end
  for _, g in ipairs(btn_data.gamepad) do
    if _pressed_buttons[g] then return true end
  end
  return false
end

-- ─── Font helpers ─────────────────────────────────────────────────────────────

local function _get_font(size)
  if not _font_cache[size] then
    -- "mono" forces 1-bit monochrome rendering (no anti-aliasing) so glyphs
    -- have hard edges that stay sharp when the canvas is scaled up.
    -- Drop a pixel TTF into assets/ and change this path for custom fonts.
    local f = love.graphics.newFont(size, "mono")
    f:setFilter("nearest", "nearest")
    _font_cache[size] = f
  end
  return _font_cache[size]
end

--- Returns the pixel width of text rendered at the given font size.
function retrolib.measure_text(text, size)
  return _get_font(size):getWidth(text)
end

-- ─── Drawing helpers ──────────────────────────────────────────────────────────

--- Clears the current render target to the given color table {r,g,b,a}.
function retrolib.clear_background(color)
  love.graphics.clear(color[1], color[2], color[3], color[4] or 1)
end

--- Draws text at (x,y) using a font of the given pixel size.
-- color is a table {r,g,b,a} with components in 0-1 range.
function retrolib.draw_text(text, x, y, size, color)
  love.graphics.setFont(_get_font(size))
  love.graphics.setColor(color)
  love.graphics.print(text, x, y)
  love.graphics.setColor(1, 1, 1, 1)
end

--- Draws the current FPS counter at (x,y).
function retrolib.draw_fps(x, y)
  love.graphics.setFont(_get_font(10))
  love.graphics.setColor(0, 1, 0, 1)
  love.graphics.print(tostring(love.timer.getFPS()) .. " FPS", x, y)
  love.graphics.setColor(1, 1, 1, 1)
end

--- Filled rectangle. color is {r,g,b,a}.
function retrolib.draw_rectangle(x, y, w, h, color)
  love.graphics.setColor(color)
  love.graphics.rectangle("fill", x, y, w, h)
  love.graphics.setColor(1, 1, 1, 1)
end

--- Outlined rectangle. color is {r,g,b,a}.
function retrolib.draw_rectangle_lines(x, y, w, h, color)
  love.graphics.setColor(color)
  love.graphics.rectangle("line", x, y, w, h)
  love.graphics.setColor(1, 1, 1, 1)
end

--- Loads a texture image with nearest-neighbour filtering (pixel-art friendly).
function retrolib.load_texture(path)
  local img = love.graphics.newImage(path)
  img:setFilter("nearest", "nearest")
  return img
end

--- Draws a single sprite from a texture atlas.
-- sprite_id is 0-based; square_sprite_size is the tile size in pixels.
function retrolib.draw_sprite(texture, sprite_id, x, y, square_sprite_size)
  local iw, ih = texture:getDimensions()
  local cols   = math.floor(iw / square_sprite_size)
  local src_x  = (sprite_id % cols) * square_sprite_size
  local src_y  = math.floor(sprite_id / cols) * square_sprite_size
  local quad   = love.graphics.newQuad(src_x, src_y,
    square_sprite_size, square_sprite_size,
    iw, ih)
  love.graphics.setColor(1, 1, 1, 1)
  love.graphics.draw(texture, quad, x, y)
end

-- ─── Camera (mirrors raylib Camera2D semantics) ───────────────────────────────
-- camera = { target={x,y}, offset={x,y}, zoom=1.0, rotation=0.0 }

--- Push a 2D camera transform onto the graphics stack.
-- Equivalent to raylib's BeginMode2D.
function retrolib.begin_mode_2d(camera)
  love.graphics.push()
  love.graphics.translate(camera.offset.x, camera.offset.y)
  love.graphics.scale(camera.zoom, camera.zoom)
  love.graphics.translate(-camera.target.x, -camera.target.y)
end

--- Pop the 2D camera transform. Equivalent to raylib's EndMode2D.
function retrolib.end_mode_2d()
  love.graphics.pop()
end

-- ─── Palette helpers ──────────────────────────────────────────────────────────

--- Returns the Picotron palette color for index idx (0-31) as {r,g,b,a} (0-1).
function retrolib.pal_picotron(idx)
  local p = {
    { 0,   0,  0 }, { 29, 43, 83 }, { 126, 37, 83 }, { 0, 135, 81 },
    { 171, 82, 54 }, { 95, 87, 79 }, { 194, 195, 199 }, { 255, 241, 232 },
    { 255, 0,   77 }, { 255, 163, 0 }, { 255, 236, 39 }, { 0, 228, 54 },
    { 41,  173, 255 }, { 131, 118, 156 }, { 255, 119, 168 }, { 255, 204, 170 },
    { 41,  24, 20 }, { 17, 29, 53 }, { 66, 33, 54 }, { 18, 83, 89 },
    { 116, 47, 41 }, { 73, 51, 59 }, { 162, 136, 121 }, { 243, 239, 125 },
    { 190, 18, 80 }, { 255, 108, 36 }, { 168, 231, 46 }, { 0, 181, 142 },
    { 6,   90, 181 }, { 117, 70, 101 }, { 255, 110, 204 }, { 255, 157, 129 },
  }
  local c = p[idx + 1] or { 0, 0, 0 }
  return { c[1] / 255, c[2] / 255, c[3] / 255, 1 }
end

-- Internal HSV→RGB (h,s,v in 0-1, returns r,g,b in 0-1)
local function _hsv_to_rgb(h, s, v)
  if s == 0 then return v, v, v end
  local i  = math.floor(h * 6)
  local f  = h * 6 - i
  local p2 = v * (1 - s)
  local q  = v * (1 - f * s)
  local t  = v * (1 - (1 - f) * s)
  i        = i % 6
  if i == 0 then
    return v, t, p2
  elseif i == 1 then
    return q, v, p2
  elseif i == 2 then
    return p2, v, t
  elseif i == 3 then
    return p2, q, v
  elseif i == 4 then
    return t, p2, v
  else
    return v, p2, q
  end
end

--- Returns the VGA Mode 13h palette color for index idx (0-255) as {r,g,b,a} (0-1).
function retrolib.pal_vga(idx)
  assert(idx >= 0 and idx <= 255, "pal_vga: index must be 0-255")

  -- First 16: standard EGA/CGA
  local ega = {
    { 0,   0, 0 }, { 0, 0, 170 }, { 0, 170, 0 }, { 0, 170, 170 },
    { 170, 0, 0 }, { 170, 0, 170 }, { 170, 85, 0 }, { 170, 170, 170 },
    { 85,  85, 85 }, { 85, 85, 255 }, { 85, 255, 85 }, { 85, 255, 255 },
    { 255, 85, 85 }, { 255, 85, 255 }, { 255, 255, 85 }, { 255, 255, 255 },
  }
  if idx < 16 then
    local c = ega[idx + 1]
    return { c[1] / 255, c[2] / 255, c[3] / 255, 1 }
  end

  -- 16-31: greyscale ramp
  if idx <= 31 then
    local s = (idx - 16) * (1 / 15)
    return { s, s, s, 1 }
  end

  -- 32-247: 24-hue × 3-intensity block
  if idx <= 247 then
    local offset  = idx - 32
    local row     = math.floor(offset / 24)
    local col     = offset % 24
    local mult    = ({ 1.0, 0.5, 0.25 })[row + 1] or 0.25
    local r, g, b = _hsv_to_rgb(col / 24.0, 1.0, 1.0)
    return { r * mult, g * mult, b * mult, 1 }
  end

  -- 248-255: black/unused
  return { 0, 0, 0, 1 }
end

-- ─── Tile flags (fz_meta PNG chunk) ──────────────────────────────────────────
-- fz sprite sheets embed a tEXt chunk with keyword "fz_meta" containing JSON:
--   {"tile_size":16,"flags":{"0":2,"5":1,...}}
-- Keys are 0-based tile indices (strings); values are uint8 bitmasks (0-255).

local _json = (function()
  local ok, m = pcall(require, "lib.3pp.json")
  return ok and m or nil
end)()

local function _u32be(s, i)
  return string.byte(s, i)   * 0x1000000
       + string.byte(s, i+1) * 0x10000
       + string.byte(s, i+2) * 0x100
       + string.byte(s, i+3)
end

--- Reads tile-flag metadata from a PNG file produced by `fz gfx`.
-- path is a love.filesystem path (e.g. "assets/gfx/tiles.png").
-- Returns { tile_size=N, flags={[tile_id]=bitmask, ...} } or nil on failure.
-- Tile IDs are 0-based numbers; bitmask is 0–255.
function retrolib.load_tile_meta(path)
  assert(_json, "retrolib.load_tile_meta requires lib/3pp/json.lua")
  local data = love.filesystem.read(path)
  if not data or #data < 8 then return nil end
  if data:sub(1, 8) ~= "\137PNG\r\n\26\n" then return nil end

  local pos = 9
  while pos + 11 <= #data do
    local len   = _u32be(data, pos)
    local ctype = data:sub(pos + 4, pos + 7)
    if pos + 8 + len > #data then break end
    if ctype == "tEXt" then
      local cdata = data:sub(pos + 8, pos + 8 + len - 1)
      local sep   = cdata:find("\0", 1, true)
      if sep and cdata:sub(1, sep - 1) == "fz_meta" then
        local ok, meta = pcall(_json.decode, cdata:sub(sep + 1))
        if ok and type(meta) == "table" then
          local flags = {}
          if type(meta.flags) == "table" then
            for k, v in pairs(meta.flags) do
              flags[tonumber(k)] = v
            end
          end
          return { tile_size = meta.tile_size or 16, flags = flags }
        end
      end
    end
    pos = pos + 4 + 4 + len + 4
    if ctype == "IEND" then break end
  end
  return nil
end

--- Returns true if tile tile_id has flag bit bit_index set.
-- meta is the table returned by load_tile_meta().
-- tile_id is 0-based; bit_index is 0–7 (0 = least-significant bit).
-- Mirrors the PICO-8 fget(n, f) API.
function retrolib.fget(meta, tile_id, bit_index)
  if not meta or not meta.flags then return false end
  local mask = meta.flags[tile_id] or 0
  return math.floor(mask / (2 ^ bit_index)) % 2 == 1
end

-- ─── Audio ────────────────────────────────────────────────────────────────────

-- Internal state
local _sfx_sources = {}  -- cached Source objects keyed by path
local _bgm_source  = nil -- currently playing BGM Source (or nil)

--- Play a sound effect once.
-- path is relative to the Love2D source directory (e.g. "assets/sfx/jump.wav").
-- The source is cached after the first call so subsequent calls are cheap.
function retrolib.sfx(path)
  local src = _sfx_sources[path]
  if not src then
    src = love.audio.newSource(path, "static")
    _sfx_sources[path] = src
  end
  src:stop()
  src:play()
end

--- Start background music.
-- path is relative to the Love2D source directory (e.g. "assets/bgm/theme.ogg").
-- loop defaults to true.  Calling bgm() again with the same path is a no-op if
-- music is already playing; pass a different path to switch tracks (stop + start).
function retrolib.bgm(path, loop)
  if loop == nil then loop = true end
  -- Already playing the same track — do nothing.
  if _bgm_source and _bgm_source._path == path and _bgm_source:isPlaying() then
    return
  end
  -- Stop previous track.
  if _bgm_source then
    _bgm_source:stop()
    _bgm_source = nil
  end
  if not path then return end
  local src = love.audio.newSource(path, "stream")
  src:setLooping(loop)
  src._path = path
  src:play()
  _bgm_source = src
end

--- Stop background music.
function retrolib.bgm_stop()
  if _bgm_source then
    _bgm_source:stop()
    _bgm_source = nil
  end
end

--- Pause or resume background music.
function retrolib.bgm_pause(paused)
  if not _bgm_source then return end
  if paused then
    _bgm_source:pause()
  else
    _bgm_source:play()
  end
end

-- ─── Lifecycle / main loop ────────────────────────────────────────────────────

local function _init_graphics()
  love.window.setTitle(retrolib.WINDOW_TITLE)
  love.window.setMode(
    retrolib.SCREEN_WIDTH * retrolib.SCREEN_SCALE,
    retrolib.SCREEN_HEIGHT * retrolib.SCREEN_SCALE,
    { resizable = false, vsync = true }
  )
  _canvas = love.graphics.newCanvas(retrolib.SCREEN_WIDTH, retrolib.SCREEN_HEIGHT)
  _canvas:setFilter("nearest", "nearest")
  love.graphics.setDefaultFilter("nearest", "nearest")
end

--- Sets up all Love2D callbacks and starts the game loop.
-- Call this at the bottom of main.lua, passing your init/update/draw functions.
function retrolib.main(init_func, update_func, draw_func)
  love.load = function()
    _init_graphics()
    init_func()
  end

  love.update = function(dt)
    update_func(dt)
    -- Single-frame press state is consumed; reset for next frame.
    _pressed_keys    = {}
    _pressed_buttons = {}
  end

  love.draw = function()
    -- Render the game at the virtual resolution …
    love.graphics.setCanvas(_canvas)
    love.graphics.clear(0, 0, 0, 1)
    draw_func()
    love.graphics.setCanvas()

    -- Compute the largest integer scale that fits the current window,
    -- then centre the canvas with black bars on the remaining space.
    local ww, wh = love.graphics.getDimensions()
    local scale  = math.floor(math.min(
      ww / retrolib.SCREEN_WIDTH,
      wh / retrolib.SCREEN_HEIGHT
    ))
    if scale < 1 then scale = 1 end
    local ox = math.floor((ww - retrolib.SCREEN_WIDTH * scale) / 2)
    local oy = math.floor((wh - retrolib.SCREEN_HEIGHT * scale) / 2)

    love.graphics.clear(0, 0, 0, 1)
    love.graphics.setColor(1, 1, 1, 1)
    love.graphics.draw(_canvas, ox, oy, 0, scale, scale)
  end

  love.keypressed = function(key)
    _pressed_keys[key] = true
    if key == "escape" then
      love.event.quit()
    end
    -- Ctrl+F toggles fullscreen (desktop mode keeps native resolution)
    if key == "f" and love.keyboard.isDown("lctrl") then
      local fs = not love.window.getFullscreen()
      love.window.setFullscreen(fs, "desktop")
    end
  end

  love.gamepadpressed = function(_joystick, button)
    _pressed_buttons[button] = true
  end
end

return retrolib
