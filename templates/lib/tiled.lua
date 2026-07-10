-- tiled.lua  –  Tiled map loader/renderer for LÖVE2D
-- Supports TMJ (Tiled JSON map) and TSJ (Tiled JSON tileset) files.
-- Encoding: plain list, base64, base64+gzip, base64+zlib.
-- Uses rxi/json.lua for JSON parsing.

local json     = require("lib.3pp.json")
local retrolib = require("lib.retrolib")

-- ─── Path utilities ───────────────────────────────────────────────────────────

local function dirname(path)
  -- Returns everything up to and including the last slash, or "".
  return path:match("^(.*[/\\])") or ""
end

local function path_join(dir, file)
  if dir == "" then return file end
  if dir:sub(-1) ~= "/" and dir:sub(-1) ~= "\\" then
    dir = dir .. "/"
  end
  -- Resolve ".." segments so Love2D's virtual FS can find the file.
  local path = dir .. file
  -- Normalise: repeatedly collapse "segment/.." until none remain.
  while true do
    local new = path:gsub("[^/]+/%.%./", "")
    if new == path then break end
    path = new
  end
  -- Remove any leading "./"
  path = path:gsub("^%./", "")
  return path
end

-- ─── Flip-flag helpers ────────────────────────────────────────────────────────

-- Tiled encodes H/V/D flip flags in the top 3 bits of every GID.
local FLIP_H = 0x80000000  -- bit 31
local FLIP_V = 0x40000000  -- bit 30
local FLIP_D = 0x20000000  -- bit 29 (diagonal / anti-diagonal transpose)

-- Strip the three flip bits from a raw GID and return them separately.
local function strip_flip_flags(gid)
  local h, v, d = false, false, false
  if gid >= FLIP_H then gid = gid - FLIP_H; h = true end
  if gid >= FLIP_V then gid = gid - FLIP_V; v = true end
  if gid >= FLIP_D then gid = gid - FLIP_D; d = true end
  return gid, h, v, d
end

-- ─── Binary helpers ───────────────────────────────────────────────────────────

-- Decode a little-endian uint32 starting at byte position i (1-based).
local function read_uint32_le(s, i)
  local b0, b1, b2, b3 = string.byte(s, i, i + 3)
  return b0 + b1 * 256 + b2 * 65536 + b3 * 16777216
end

-- Decode a base64(+gzip/zlib) layer data string into an array of tile GIDs.
local function decode_layer_data(raw, compression)
  local bytes = love.data.decode("string", "base64", raw)
  if compression == "gzip" then
    bytes = love.data.decompress("string", "gzip", bytes)
  elseif compression == "zlib" then
    bytes = love.data.decompress("string", "zlib", bytes)
  end
  local ids = {}
  for i = 1, #bytes, 4 do
    ids[#ids + 1] = read_uint32_le(bytes, i)
  end
  return ids
end

-- ─── TileSheet ────────────────────────────────────────────────────────────────

local TileSheet = {}
TileSheet.__index = TileSheet

--- Load a Tiled tileset file (.tsj).
-- @param filename  Path relative to the Love2D source directory.
function TileSheet.new(filename)
  local self = setmetatable({}, TileSheet)
  self.filename = filename

  local content = assert(love.filesystem.read(filename),
    "TileSheet: cannot read " .. filename)
  self.data = json.decode(content)

  local sheet_path = path_join(dirname(filename), self.data.image)
  self.texture = love.graphics.newImage(sheet_path)
  self.texture:setFilter("nearest", "nearest")

  -- Cached quads: self._quads[spr_id] = Quad
  self._quads        = {}

  -- Pre-process tile properties for fast lookup.
  self.tile_props    = {}
  self.default_props = {}

  if self.data.properties then
    for _, p in ipairs(self.data.properties) do
      self.default_props[p.name] = p.value
    end
  end
  if self.data.tiles then
    for _, tile_entry in ipairs(self.data.tiles) do
      local t_id = tile_entry.id
      local props = {}
      if tile_entry.properties then
        for _, p in ipairs(tile_entry.properties) do
          props[p.name] = p.value
        end
      end
      self.tile_props[t_id] = props
    end
  end

  return self
end

--- Return a property for a tile.
-- Lookup order: tile-specific → tileset default → fallback.
function TileSheet:get_property(tile_id, key, default)
  local tp = self.tile_props[tile_id]
  if tp and tp[key] ~= nil then return tp[key] end
  local dp = self.default_props[key]
  if dp ~= nil then return dp end
  return default
end

--- Return (and lazily cache) the Quad for spr_id (0-based local tile index).
function TileSheet:_get_quad(spr_id)
  if not self._quads[spr_id] then
    local tw            = self.data.tilewidth
    local th            = self.data.tileheight
    local iw, ih        = self.texture:getDimensions()
    local cols          = math.floor(iw / tw)
    local sx            = (spr_id % cols) * tw
    local sy            = math.floor(spr_id / cols) * th
    self._quads[spr_id] = love.graphics.newQuad(sx, sy, tw, th, iw, ih)
  end
  return self._quads[spr_id]
end

--- Draw one sprite from this sheet at world coordinates (x, y).
-- spr_id is the 0-based local tile index (not a global GID).
-- flip_h, flip_v, flip_d are the Tiled flip flags (booleans).
function TileSheet:draw(spr_id, x, y, flip_h, flip_v, flip_d)
  love.graphics.setColor(1, 1, 1, 1)
  if not flip_h and not flip_v and not flip_d then
    love.graphics.draw(self.texture, self:_get_quad(spr_id), x, y)
    return
  end
  local tw = self.data.tilewidth
  local th = self.data.tileheight
  local r, sx, sy = 0, 1, 1
  if flip_d then
    if     flip_h and flip_v then r, sx, sy = -math.pi / 2,  1, -1  -- anti-transpose
    elseif flip_h             then r, sx, sy =  math.pi / 2,  1,  1  -- 90° CW
    elseif flip_v             then r, sx, sy = -math.pi / 2,  1,  1  -- 90° CCW
    else                           r, sx, sy = -math.pi / 2, -1,  1  -- transpose
    end
  else
    sx = flip_h and -1 or 1
    sy = flip_v and -1 or 1
  end
  love.graphics.push()
  love.graphics.translate(x + tw / 2, y + th / 2)
  love.graphics.rotate(r)
  love.graphics.scale(sx, sy)
  love.graphics.draw(self.texture, self:_get_quad(spr_id), -tw / 2, -th / 2)
  love.graphics.pop()
end

-- ─── TileMap ──────────────────────────────────────────────────────────────────

local TileMap = {}
TileMap.__index = TileMap

--- Load a Tiled map file (.tmj).
-- @param filename  Path relative to the Love2D source directory.
function TileMap.new(filename)
  local self         = setmetatable({}, TileMap)
  self.filename      = filename

  local content      = assert(love.filesystem.read(filename),
    "TileMap: cannot read " .. filename)
  self.data          = json.decode(content)

  self.width         = self.data.width
  self.height        = self.data.height
  self.tile_w        = self.data.tilewidth
  self.tile_h        = self.data.tileheight

  self.tilesets      = {}
  self.layers        = {}
  self.object_layers = {}

  local base_path    = dirname(filename)

  for _, ts in ipairs(self.data.tilesets) do
    local ts_path = path_join(base_path, ts.source)
    self.tilesets[#self.tilesets + 1] = {
      firstgid = ts.firstgid,
      sheet    = TileSheet.new(ts_path),
    }
  end

  for _, layer in ipairs(self.data.layers) do
    if layer.type == "tilelayer" then
      self.layers[#self.layers + 1] = self:_decode_layer(layer)
    elseif layer.type == "objectgroup" then
      self.object_layers[layer.name:lower()] = layer.objects or {}
    end
  end

  return self
end

--- Decode a raw Tiled tile layer into a normalized table.
function TileMap:_decode_layer(layer)
  local tile_ids
  if layer.encoding == "base64" then
    tile_ids = decode_layer_data(layer.data, layer.compression)
  else
    -- Plain CSV / unencoded list already parsed by json.decode
    tile_ids = layer.data
  end
  return {
    name    = layer.name,
    class   = layer["class"] or "",
    data    = tile_ids,
    visible = (layer.visible ~= false),
  }
end

--- Find a decoded tile layer by name. Returns nil if not found.
function TileMap:get_layer(name)
  local lower = name:lower()
  for _, layer in ipairs(self.layers) do
    if layer.name:lower() == lower then return layer end
  end
  return nil
end

--- Return all objects in a named object layer (empty table if missing).
function TileMap:get_objects(layer_name)
  return self.object_layers[layer_name:lower()] or {}
end

--- Return objects whose "type" field matches obj_type.
function TileMap:get_objects_by_type(layer_name, obj_type)
  local result = {}
  for _, o in ipairs(self:get_objects(layer_name)) do
    if o.type == obj_type then
      result[#result + 1] = o
    end
  end
  return result
end

--- Resolve a global GID to its TileSheet and local tile id.
-- Strips Tiled flip flags before the lookup and returns them as extra values.
-- Returns (sheet, local_id, flip_h, flip_v, flip_d) or (nil, 0, …) for gid == 0.
function TileMap:_get_sheet_for_gid(raw_gid)
  local gid, flip_h, flip_v, flip_d = strip_flip_flags(raw_gid)
  -- Iterate tilesets in reverse so we match the highest firstgid ≤ gid.
  for i = #self.tilesets, 1, -1 do
    local ts = self.tilesets[i]
    if gid >= ts.firstgid then
      return ts.sheet, gid - ts.firstgid, flip_h, flip_v, flip_d
    end
  end
  return nil, 0, false, false, false
end

--- Draw a single decoded layer with optional camera or scroll offset.
-- layer_data is the table returned by _decode_layer.
-- camera is the same table used with retrolib.begin_mode_2d / end_mode_2d.
function TileMap:draw_layer(layer_data, offset_x, offset_y, camera)
  if not layer_data or not layer_data.visible then return end

  offset_x = offset_x or 0
  offset_y = offset_y or 0

  local start_col, start_row, end_col, end_row

  if camera then
    local zoom         = camera.zoom
    local world_left   = camera.target.x - camera.offset.x / zoom
    local world_top    = camera.target.y - camera.offset.y / zoom
    local world_right  = world_left + retrolib.SCREEN_WIDTH / zoom
    local world_bottom = world_top + retrolib.SCREEN_HEIGHT / zoom

    start_col          = math.max(0, math.floor(world_left / self.tile_w))
    start_row          = math.max(0, math.floor(world_top / self.tile_h))
    end_col            = math.min(self.width, math.floor(world_right / self.tile_w) + 1)
    end_row            = math.min(self.height, math.floor(world_bottom / self.tile_h) + 1)
  else
    start_col = math.max(0, math.floor(-offset_x / self.tile_w))
    start_row = math.max(0, math.floor(-offset_y / self.tile_h))
    end_col   = math.min(self.width, math.floor((-offset_x + retrolib.SCREEN_WIDTH) / self.tile_w) + 1)
    end_row   = math.min(self.height, math.floor((-offset_y + retrolib.SCREEN_HEIGHT) / self.tile_h) + 1)
  end

  for row = start_row, end_row - 1 do
    for col = start_col, end_col - 1 do
      local idx = row * self.width + col + 1       -- Lua 1-based
      local gid = layer_data.data[idx]

      if gid and gid ~= 0 then
        local sheet, local_id, flip_h, flip_v, flip_d = self:_get_sheet_for_gid(gid)
        if sheet then
          local tx, ty
          if camera then
            tx = col * self.tile_w
            ty = row * self.tile_h
          else
            tx = col * self.tile_w + offset_x
            ty = row * self.tile_h + offset_y
          end
          sheet:draw(local_id, tx, ty, flip_h, flip_v, flip_d)
        end
      end
    end
  end
end

--- Draw all tile layers in map order.
-- @param options  Optional table: { camera=..., layer_classes={...}, offset_x=0, offset_y=0 }
--   layer_classes filters by the Tiled layer "class" field (Tiled 1.9+).
--   If layer_classes is nil, all layers are drawn.
function TileMap:draw(options)
  options             = options or {}
  local camera        = options.camera
  local layer_classes = options.layer_classes
  local offset_x      = options.offset_x or 0
  local offset_y      = options.offset_y or 0

  for _, layer in ipairs(self.layers) do
    local draw_it = true
    if layer_classes then
      draw_it = false
      for _, cls in ipairs(layer_classes) do
        if cls == layer.class then
          draw_it = true
          break
        end
      end
    end
    if draw_it then
      self:draw_layer(layer, offset_x, offset_y, camera)
    end
  end
end

-- ─── Module exports ───────────────────────────────────────────────────────────
return {
  TileSheet = TileSheet,
  TileMap   = TileMap,
}
