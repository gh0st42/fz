-- retroui.lua  –  Simple retro UI widgets for LÖVE2D
-- Provides Msgbox, Menubox, LinePrinter and VerticalScroller,
-- styled with the VGA palette from retrolib.

local retrolib           = require("lib.retrolib")

local retroui            = {}

-- Optional Source objects a caller can assign (e.g. retroui.SFX_MENU_SELECT
-- = love.audio.newSource(...)) to have Menubox play a sound on navigate/
-- confirm. Left nil, navigation is silent.
retroui.SFX_MENU_SELECT  = nil
retroui.SFX_MENU_CHANGED = nil

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
-- @param msg   String to display.
-- @param opts  Table of options.  Currently supports:
--                font_size:       Size of the text (default 10).
--                margin:          Padding around text (default 4).
--                auto_hide_delay: If set, the box closes itself after this
--                                 many seconds instead of waiting for input.
--                x, y:            Override the default centering of the box.
function Msgbox.new(msg, opts)
  local self           = setmetatable({}, Msgbox)
  self.msg             = msg
  self.font_size       = (opts and opts.font_size) or 10
  self.margin          = (opts and opts.margin) or 4
  self.auto_hide_delay = (opts and opts.auto_hide_delay) or nil
  self.started_at      = nil
  self.x               = (opts and opts.x) or nil
  self.y               = (opts and opts.y) or nil
  self.tw, self.th     = retrolib.measure_text_ex(msg, self.font_size)
  return self
end

--- Call every frame while the dialog is open.
-- Returns false when the player dismisses it (BTN_A pressed), true otherwise.
function Msgbox:update()
  if self.started_at == nil then
    self.started_at = love.timer.getTime()
  elseif self.auto_hide_delay and love.timer.getTime() - self.started_at >= self.auto_hide_delay then
    return false
  end
  if self.auto_hide_delay then
    return nil -- Don't block input if we're auto-hiding; let the caller remove us when the time comes.
  elseif retrolib.btnp(retrolib.BTN_A) or retrolib.btnp(retrolib.BTN_START) then
    return false
  end
  return true
end

--- Draw the message box centered on the virtual screen.
function Msgbox:draw()
  local w    = self.tw + self.margin * 2
  local h    = self.th + self.margin * 2
  local left = self.x or math.floor((retrolib.SCREEN_WIDTH - w) / 2)
  local top  = self.y or math.floor((retrolib.SCREEN_HEIGHT - h) / 2)

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
  self.headline  = (opts and opts.headline) or nil

  -- Measure the widest label.
  local max_w    = 0
  for _, c in ipairs(choices) do
    local w = retrolib.measure_text(c[1], self.font_size)
    if w > max_w then max_w = w end
  end
  if self.headline then
    local w = retrolib.measure_text(self.headline, self.font_size)
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
    if retroui.SFX_MENU_CHANGED then retroui.SFX_MENU_CHANGED:play() end
  end
  if retrolib.btnp(retrolib.BTN_DOWN) then
    self.selected = self.selected % #self.choices + 1
    if retroui.SFX_MENU_CHANGED then retroui.SFX_MENU_CHANGED:play() end
  end
  if retrolib.btnp(retrolib.BTN_A) or retrolib.btnp(retrolib.BTN_START) then
    local action = self.choices[self.selected][2]
    if action then action() end
    if retroui.SFX_MENU_SELECT then retroui.SFX_MENU_SELECT:play() end
    return false
  end
  return true
end

--- Draw the menu centered on the virtual screen.
function Menubox:draw()
  local row_h = self.font_size + self.margin
  local w     = self.tw + self.margin * 2
  local h     = self.margin * 2 + #self.choices * row_h
  if self.headline then
    h = h + row_h
  end
  local left = self.x or math.floor((retrolib.SCREEN_WIDTH - w) / 2)
  local top  = self.y or math.floor((retrolib.SCREEN_HEIGHT - h) / 2)

  retrolib.draw_rectangle(left, top, w, h, retrolib.pal_vga(1))
  retrolib.draw_rectangle_lines(left, top, w, h, retrolib.pal_vga(15))

  if self.headline then
    retrolib.draw_text(self.headline, left + self.margin, top + self.margin,
      self.font_size, retrolib.WHITE)
  end

  for i, choice in ipairs(self.choices) do
    local color = (i == self.selected) and retrolib.WHITE or retrolib.pal_vga(7)
    local y     = top + self.margin + (i - 1) * row_h
    if self.headline then
      y = y + row_h
    end
    retrolib.draw_text(choice[1], left + self.margin, y, self.font_size, color)
  end
end

-- ─── Module exports ───────────────────────────────────────────────────────────

-- ─── LinePrinter ──────────────────────────────────────────────────────────────

local LinePrinter = {}
LinePrinter.__index = LinePrinter

--- Print lines top to bottom, either with a delay line by line or instantaneously.
-- @param text   String to display (may contain newlines) or a table of lines.
-- @param opts   Table of options:
--                 fg:    Text color (default: WHITE).
--                 bg:    Background color (default: BLACK) or -1 for transparent.
--                 delay: Seconds per line; 0 = instant (default 0).
function LinePrinter.new(text, opts)
  opts       = opts or {}
  local self = setmetatable({}, LinePrinter)
  if type(text) == "table" then
    self.lines = text
  else
    self.lines = split_lines(text)
  end
  self.fg                = opts.fg or retrolib.WHITE
  self.bg                = opts.bg or retrolib.BLACK
  self.delay             = opts.delay or 0
  self.font_size         = opts.font_size or 10
  -- measure_text_ex("", size) with a single (empty) line reliably returns
  -- just that one line's height, regardless of how many lines self.lines has.
  local _, line_h        = retrolib.measure_text_ex("", self.font_size)
  self.line_height       = line_h * 1.2 -- a little breathing room between rows
  self.total_text_height = #self.lines * self.line_height
  self.row_index         = (self.delay <= 0) and #self.lines or 1
  self.done              = self.delay <= 0
  self.timer             = 0
  return self
end

--- Call every frame while the dialog is open.
-- @param dt  Delta time in seconds.
-- Returns false when the player dismisses the box.
function LinePrinter:update(dt)
  if not self.done then
    if self.delay > 0 then
      self.timer     = self.timer + dt
      self.row_index = math.min(#self.lines, math.floor(self.timer / self.delay) + 1)
    end
    -- A skips straight to the end
    if retrolib.btnp(retrolib.BTN_A) then
      self.row_index = #self.lines
    elseif retrolib.btn(retrolib.BTN_B) then
      self.timer = self.timer + dt * 3 -- B speeds up the text
    end
    if self.row_index >= #self.lines then
      self.done = true
    end
  else
    if retrolib.btnp(retrolib.BTN_A) then
      return false
    end
  end
  return true
end

--- Draw the lines top to bottom on the virtual screen.
function LinePrinter:draw()
  local sw    = retrolib.SCREEN_WIDTH
  local sh    = retrolib.SCREEN_HEIGHT
  local row_h = self.line_height
  local box_h = self.total_text_height
  local box_y = (sh - box_h) / 2 -- center vertically

  if self.bg ~= -1 then
    retrolib.clear_background(self.bg)
  end

  -- retrolib.draw_rectangle(4, box_y, sw - 8, box_h, self.bg)
  -- retrolib.draw_rectangle_lines(4, box_y, sw - 8, box_h, retrolib.WHITE)

  for i = 1, self.row_index do
    local line = self.lines[i]
    retrolib.draw_text_centered(line, box_y + (i - 1) * row_h,
      self.font_size, self.fg)
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
--                 font:        TTF path to render with, or false to force
--                              LÖVE2D's own built-in system font. Omitted
--                              (nil) leaves retrolib.FONT_PATH untouched.
function VerticalScroller.new(text, opts)
  opts              = opts or {}
  local self        = setmetatable({}, VerticalScroller)
  self.lines        = split_lines(text)
  self.fg           = opts.fg or retrolib.WHITE
  self.bg           = opts.bg or retrolib.BLACK
  self.speed        = opts.speed or 24
  self.line_height  = opts.line_height or 12
  self.font_size    = opts.font_size or 10
  self.has_font     = opts.font ~= nil       -- an explicit choice was made
  self.font         = opts.font or nil       -- false collapses to nil (LÖVE's default font)
  self.scroll_y     = retrolib.SCREEN_HEIGHT -- start below the screen
  self.total_height = #self.lines * self.line_height
  return self
end

--- Call every frame while the dialog is open.
-- @param dt  Delta time in seconds.
-- Returns false when the text has scrolled fully off screen, or when skipped.
function VerticalScroller:update(dt)
  if retrolib.btnp(retrolib.BTN_A) or retrolib.btnp(retrolib.BTN_START) then
    return false
  end
  local fac = 1
  if retrolib.btn(retrolib.BTN_B) then
    fac = 6 -- speed up if B is held
  end

  self.scroll_y = self.scroll_y - (self.speed * dt * fac)
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

  -- Swap in this scroller's font for the duration of the text draw, if one
  -- was given (a path, or false for LÖVE's built-in font); otherwise
  -- retrolib's own current default font is left untouched.
  local prev_font_path = retrolib.FONT_PATH
  if self.has_font then
    retrolib.FONT_PATH = self.font
  end

  for i, line in ipairs(self.lines) do
    local y = self.scroll_y + (i - 1) * self.line_height
    if y + self.font_size >= 0 and y < sh then
      local tw = retrolib.measure_text(line, self.font_size)
      retrolib.draw_text(line, math.floor((sw - tw) / 2), y, self.font_size, self.fg)
    end
  end

  if self.has_font then
    retrolib.FONT_PATH = prev_font_path
  end
end

-- ─── PrompterBox ─────────────────────────────────────────────────────────

local PrompterBox = {}
PrompterBox.__index = PrompterBox

-- Show multiple lines of text in a box, with a teleprompter typing effect.  The caller
-- is responsible for calling :update() and :draw() each frame.
function PrompterBox.new(text, opts)
  opts                = opts or {}
  local self          = setmetatable({}, PrompterBox)
  self.text           = text
  self.font_size      = opts.font_size or 10
  self.margin         = opts.margin or 4
  self.x              = 8
  self.y              = retrolib.SCREEN_HEIGHT - (retrolib.SCREEN_HEIGHT / 3 + 8)
  self.w              = retrolib.SCREEN_WIDTH - 16
  self.h              = retrolib.SCREEN_HEIGHT / 3
  self.speaker        = (opts and opts.speaker) or nil
  self.tw, self.th    = retrolib.measure_text_ex(text, self.font_size)
  self.awaiting_input = false
  self.char_index     = 1
  self.char_timer     = 0
  self.char_delay     = opts.char_delay or 0.05 -- seconds per character
  self.row_index      = 1
  return self
end

function PrompterBox:update(dt)
  -- Update the teleprompter effect here.
  -- This is a stub implementation; actual implementation would
  -- advance the text based on dt and possibly user input.

  if self.awaiting_input then
    if retrolib.btnp(retrolib.BTN_A) or retrolib.btnp(retrolib.BTN_START) then
      self.awaiting_input = false
      return false -- Return false to indicate the box should close.
    end
  else
    self.char_timer = self.char_timer + dt
    if retrolib.btn(retrolib.BTN_B) then
      self.char_timer = self.char_timer + dt * 3 -- B speeds up the text
    end
    if retrolib.btnp(retrolib.BTN_A) then
      -- Skip to the end of the current line or all text.
      if self.row_index < #self.text then
        self.row_index = #self.text
        self.char_index = #self.text[self.row_index]
      else
        self.awaiting_input = true
      end
    end
    if self.char_timer >= self.char_delay then
      self.char_timer = self.char_timer - self.char_delay
      self.char_index = self.char_index + 1
      if self.char_index > #self.text[self.row_index] then
        self.row_index = self.row_index + 1
        self.char_index = 1
        if self.row_index > #self.text then
          self.awaiting_input = true
        end
      end
    end
  end

  return true -- Return true to indicate the box is still active.
end

function PrompterBox:draw()
  retrolib.draw_rectangle(self.x, self.y, self.w, self.h, retrolib.pal_vga(1))
  retrolib.draw_rectangle_lines(self.x, self.y, self.w, self.h, retrolib.pal_vga(15))
  if self.speaker then
    local speaker_text = self.speaker .. ":"
    retrolib.draw_text(speaker_text, self.x + self.margin, self.y + self.margin,
      self.font_size, retrolib.WHITE)
  end
  local text_y = self.y + self.margin
  if self.speaker then
    text_y = text_y + self.font_size + self.margin
  end
  for i = 1, self.row_index do
    local line = self.text[i]
    if not line then break end
    local display_line = line
    if i == self.row_index then
      display_line = line:sub(1, self.char_index)
    end
    retrolib.draw_text(display_line, self.x + self.margin, text_y,
      self.font_size, retrolib.WHITE)
    text_y = text_y + self.font_size + self.margin
  end
  if self.awaiting_input and love.timer.getTime() % 1 < 0.5 then
    local prompt = "..."
    local prompt_w = retrolib.measure_text(prompt, self.font_size)
    retrolib.draw_text(prompt, self.x + self.w - prompt_w - self.margin,
      self.y + self.h - self.font_size - self.margin, self.font_size, retrolib.WHITE)
  end
end

-- ─── Module exports ───────────────────────────────────────────────────────────
retroui.Msgbox           = Msgbox
retroui.Menubox          = Menubox
retroui.LinePrinter      = LinePrinter
retroui.VerticalScroller = VerticalScroller
retroui.PrompterBox      = PrompterBox
return retroui
