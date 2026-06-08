-- retroui.lua  –  Simple retro UI widgets for LÖVE2D
-- Provides Msgbox, Menubox, LinePrinter and VerticalScroller,
-- styled with the VGA palette from retrolib.

local retrolib = require("retrolib")

-- ─── Shared helpers ───────────────────────────────────────────────────────────

--- Split text into lines, preserving empty lines.
local function split_lines(text)
  local out = {}
  for line in ((text or "") .. "\n"):gmatch("(.-)\n") do
    out[#out + 1] = line
  end
  return out
end

-- ─── Msgbox ───────────────────────────────────────────────────────────────────

local Msgbox = {}
Msgbox.__index = Msgbox

--- Create a centered message box.
-- @param msg  String to display.
function Msgbox.new(msg)
  local self     = setmetatable({}, Msgbox)
  self.msg       = msg
  self.font_size = 10
  self.margin    = 4
  self.tw        = retrolib.measure_text(msg, self.font_size)
  return self
end

--- Call every frame while the dialog is open.
-- Returns false when the player dismisses it (BTN_A pressed), true otherwise.
function Msgbox:update()
  if retrolib.btnp(retrolib.BTN_A) then
    return false
  end
  return true
end

--- Draw the message box centered on the virtual screen.
function Msgbox:draw()
  local w    = self.tw + self.margin * 2
  local h    = self.margin * 2 + self.font_size
  local left = math.floor((retrolib.SCREEN_WIDTH - w) / 2)
  local top  = math.floor((retrolib.SCREEN_HEIGHT - h) / 2)

  retrolib.draw_rectangle(left, top, w, h, retrolib.pal_vga(1))
  retrolib.draw_rectangle_lines(left, top, w, h, retrolib.pal_vga(15))
  retrolib.draw_text(self.msg, left + self.margin, top + self.margin,
    self.font_size, retrolib.WHITE)
end

-- ─── Menubox ──────────────────────────────────────────────────────────────────

local Menubox = {}
Menubox.__index = Menubox

--- Create a centered selection menu.
-- @param choices   Array of {label, callback} pairs.  e.g. {{"Go!", fn}, ...}
-- @param opts      Table of options.  Currently supports:
--                    abortable: If true (default), BTN_B closes without selecting.
--                    font_size: Size of the text (default 10).
--                    margin:    Padding around text (default 4).
--                    x, y:      Override the default centering of the menu.
function Menubox.new(choices, opts)
  local self     = setmetatable({}, Menubox)
  self.choices   = choices
  self.font_size = (opts and opts.font_size) or 10
  self.margin    = (opts and opts.margin) or 4
  self.selected  = 1 -- 1-based in Lua
  self.abortable = (opts and opts.abortable ~= false)
  self.x         = (opts and opts.x) or nil
  self.y         = (opts and opts.y) or nil

  -- Measure the widest label.
  local max_w    = 0
  for _, c in ipairs(choices) do
    local w = retrolib.measure_text(c[1], self.font_size)
    if w > max_w then max_w = w end
  end
  self.tw = max_w

  return self
end

--- Call every frame while the dialog is open.
-- Returns false when the player selects or aborts, true otherwise.
function Menubox:update()
  if self.abortable and retrolib.btnp(retrolib.BTN_B) then
    return false
  end
  if retrolib.btnp(retrolib.BTN_UP) then
    self.selected = (self.selected - 2) % #self.choices + 1
  end
  if retrolib.btnp(retrolib.BTN_DOWN) then
    self.selected = self.selected % #self.choices + 1
  end
  if retrolib.btnp(retrolib.BTN_A) then
    local action = self.choices[self.selected][2]
    if action then action() end
    return false
  end
  return true
end

--- Draw the menu centered on the virtual screen.
function Menubox:draw()
  local row_h = self.font_size + self.margin
  local w     = self.tw + self.margin * 2
  local h     = self.margin * 2 + #self.choices * row_h
  local left  = self.x or math.floor((retrolib.SCREEN_WIDTH - w) / 2)
  local top   = self.y or math.floor((retrolib.SCREEN_HEIGHT - h) / 2)

  retrolib.draw_rectangle(left, top, w, h, retrolib.pal_vga(1))
  retrolib.draw_rectangle_lines(left, top, w, h, retrolib.pal_vga(15))

  for i, choice in ipairs(self.choices) do
    local color = (i == self.selected) and retrolib.WHITE or retrolib.pal_vga(7)
    local y     = top + self.margin + (i - 1) * row_h
    retrolib.draw_text(choice[1], left + self.margin, y, self.font_size, color)
  end
end

-- ─── Module exports ───────────────────────────────────────────────────────────

-- ─── LinePrinter ──────────────────────────────────────────────────────────────

local LinePrinter = {}
LinePrinter.__index = LinePrinter

--- Create a typewriter-effect text box pinned to the bottom of the screen.
-- @param text   String to display (may contain newlines).
-- @param opts   Table of options:
--                 fg:    Text color (default: WHITE).
--                 bg:    Background color (default: BLACK).
--                 delay: Seconds per character; 0 = instant (default 0).
function LinePrinter.new(text, opts)
  opts             = opts or {}
  local self       = setmetatable({}, LinePrinter)
  self.lines       = split_lines(text)
  self.full_text   = text or ""
  self.total_chars = #self.full_text
  self.fg          = opts.fg or retrolib.WHITE
  self.bg          = opts.bg or retrolib.BLACK
  self.delay       = opts.delay or 0
  self.font_size   = opts.font_size or 10
  self.margin      = opts.margin or 6
  self.shown_chars = (self.delay <= 0) and self.total_chars or 0
  self.timer       = 0
  return self
end

--- Call every frame while the dialog is open.
-- @param dt  Delta time in seconds.
-- Returns false when the player dismisses the box.
function LinePrinter:update(dt)
  if self.shown_chars < self.total_chars then
    if self.delay > 0 then
      self.timer       = self.timer + dt
      self.shown_chars = math.min(self.total_chars, math.floor(self.timer / self.delay))
    end
    -- A skips straight to the end
    if retrolib.btnp(retrolib.BTN_A) then
      self.shown_chars = self.total_chars
    end
  else
    if retrolib.btnp(retrolib.BTN_A) then
      return false
    end
  end
  return true
end

--- Draw the typewriter box at the bottom of the virtual screen.
function LinePrinter:draw()
  local sw    = retrolib.SCREEN_WIDTH
  local sh    = retrolib.SCREEN_HEIGHT
  local row_h = self.font_size + 2
  local box_h = self.margin * 2 + #self.lines * row_h
  local box_y = sh - box_h - 4

  retrolib.draw_rectangle(4, box_y, sw - 8, box_h, self.bg)
  retrolib.draw_rectangle_lines(4, box_y, sw - 8, box_h, retrolib.WHITE)

  local chars_left = self.shown_chars
  for i, line in ipairs(self.lines) do
    if chars_left <= 0 then break end
    retrolib.draw_text(line:sub(1, chars_left),
      4 + self.margin, box_y + self.margin + (i - 1) * row_h,
      self.font_size, self.fg)
    chars_left = chars_left - #line - 1 -- -1 for the implicit newline separator
  end
end

-- ─── VerticalScroller ─────────────────────────────────────────────────────────

local VerticalScroller = {}
VerticalScroller.__index = VerticalScroller

--- Create a vertically-scrolling text overlay (credits/intro style).
-- @param text   String to scroll (may contain newlines).
-- @param opts   Table of options:
--                 fg:          Text color (default: WHITE).
--                 bg:          Background color (default: BLACK).
--                 speed:       Scroll speed in pixels/second (default 24).
--                 line_height: Vertical spacing per line in pixels (default 12).
function VerticalScroller.new(text, opts)
  opts              = opts or {}
  local self        = setmetatable({}, VerticalScroller)
  self.lines        = split_lines(text)
  self.fg           = opts.fg or retrolib.WHITE
  self.bg           = opts.bg or retrolib.BLACK
  self.speed        = opts.speed or 24
  self.line_height  = opts.line_height or 12
  self.font_size    = opts.font_size or 10
  self.scroll_y     = retrolib.SCREEN_HEIGHT  -- start below the screen
  self.total_height = #self.lines * self.line_height
  return self
end

--- Call every frame while the dialog is open.
-- @param dt  Delta time in seconds.
-- Returns false when the text has scrolled fully off screen, or when skipped.
function VerticalScroller:update(dt)
  if retrolib.btnp(retrolib.BTN_A) or retrolib.btnp(retrolib.BTN_B) then
    return false
  end
  self.scroll_y = self.scroll_y - self.speed * dt
  if self.scroll_y + self.total_height < 0 then
    return false
  end
  return true
end

--- Draw the scrolling text over a full-screen background.
function VerticalScroller:draw()
  local sw = retrolib.SCREEN_WIDTH
  local sh = retrolib.SCREEN_HEIGHT
  retrolib.draw_rectangle(0, 0, sw, sh, self.bg)
  for i, line in ipairs(self.lines) do
    local y = self.scroll_y + (i - 1) * self.line_height
    if y + self.font_size >= 0 and y < sh then
      local tw = retrolib.measure_text(line, self.font_size)
      retrolib.draw_text(line, math.floor((sw - tw) / 2), y, self.font_size, self.fg)
    end
  end
end

-- ─── Module exports ───────────────────────────────────────────────────────────
return {
  Msgbox           = Msgbox,
  Menubox          = Menubox,
  LinePrinter      = LinePrinter,
  VerticalScroller = VerticalScroller,
}
