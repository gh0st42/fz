local yml = {}

local function is_blank_or_comment(content)
  return content == "" or content:match("^#") ~= nil
end

local function split_lines(text)
  text = (text or ""):gsub("\r\n", "\n"):gsub("\r", "\n")
  local lines = {}
  for line in (text .. "\n"):gmatch("(.-)\n") do
    local cleaned = line:gsub("%s+$", "")
    local indent = #cleaned:match("^%s*")
    if cleaned:find("\t", 1, true) then
      error("yml.parse: tabs are not supported for indentation")
    end
    lines[#lines + 1] = {
      raw = cleaned,
      indent = indent,
      content = cleaned:sub(indent + 1),
    }
  end
  return lines
end

local function decode_double_quoted(s)
  s = s:gsub("\\n", "\n")
  s = s:gsub("\\t", "\t")
  s = s:gsub('\\"', '"')
  s = s:gsub("\\\\", "\\")
  return s
end

local function parse_scalar(raw)
  local v = raw:gsub("^%s+", ""):gsub("%s+$", "")

  if v == "" then return "" end
  if v == "true" then return true end
  if v == "false" then return false end
  if v == "null" or v == "~" then return nil end

  local sq = v:match("^'(.*)'$")
  if sq ~= nil then
    return sq
  end

  local dq = v:match('^"(.*)"$')
  if dq ~= nil then
    return decode_double_quoted(dq)
  end

  local n = tonumber(v)
  if n ~= nil then
    return n
  end

  return v
end

local function parse_key_value(content, line_no)
  local key, rest = content:match("^([^:]+):%s*(.*)$")
  if not key then
    error("yml.parse: expected key: value at line " .. line_no)
  end
  key = key:gsub("%s+$", "")
  return key, rest
end

local function next_significant(lines, i)
  while i <= #lines do
    if not is_blank_or_comment(lines[i].content) then
      return i
    end
    i = i + 1
  end
  return nil
end

local function parse_literal_block(lines, i, parent_indent)
  local start_i = i + 1
  local j = start_i
  local min_indent = nil

  while j <= #lines do
    local line = lines[j]
    if line.content == "" then
      j = j + 1
    elseif line.indent <= parent_indent then
      break
    else
      if not min_indent or line.indent < min_indent then
        min_indent = line.indent
      end
      j = j + 1
    end
  end

  if not min_indent then
    return "", j
  end

  local out = {}
  for k = start_i, j - 1 do
    local line = lines[k]
    if line.content == "" then
      out[#out + 1] = ""
    elseif line.indent > parent_indent then
      out[#out + 1] = line.raw:sub(min_indent + 1)
    end
  end

  return table.concat(out, "\n"), j
end

local parse_block

local function parse_mapping(lines, i, indent)
  local out = {}

  while i <= #lines do
    local line = lines[i]
    if is_blank_or_comment(line.content) then
      i = i + 1
    elseif line.indent < indent then
      break
    elseif line.indent > indent then
      error("yml.parse: unexpected indentation at line " .. i)
    elseif line.content:match("^%-") then
      break
    else
      local key, rest = parse_key_value(line.content, i)
      if rest == "|" or rest == "|-" or rest == "|+" then
        local value
        value, i = parse_literal_block(lines, i, indent)
        out[key] = value
      elseif rest == "" then
        local child_i = next_significant(lines, i + 1)
        if child_i and lines[child_i].indent > indent then
          local value
          value, i = parse_block(lines, child_i, lines[child_i].indent)
          out[key] = value
        else
          out[key] = nil
          i = i + 1
        end
      else
        out[key] = parse_scalar(rest)
        i = i + 1
      end
    end
  end

  return out, i
end

local function parse_sequence(lines, i, indent)
  local out = {}

  while i <= #lines do
    local line = lines[i]

    if is_blank_or_comment(line.content) then
      i = i + 1
    elseif line.indent < indent then
      break
    elseif line.indent > indent then
      error("yml.parse: unexpected indentation at line " .. i)
    else
      local item_text = line.content:match("^%-%s*(.*)$")
      if item_text == nil then
        break
      end

      if item_text == "" then
        local child_i = next_significant(lines, i + 1)
        if child_i and lines[child_i].indent > indent then
          local value
          value, i = parse_block(lines, child_i, lines[child_i].indent)
          out[#out + 1] = value
        else
          out[#out + 1] = nil
          i = i + 1
        end
      else
        local key, rest = item_text:match("^([^:]+):%s*(.*)$")
        if key then
          local item = {}
          key = key:gsub("%s+$", "")

          if rest == "|" or rest == "|-" or rest == "|+" then
            local value
            value, i = parse_literal_block(lines, i, indent)
            item[key] = value
          elseif rest == "" then
            local child_i = next_significant(lines, i + 1)
            if child_i and lines[child_i].indent > indent then
              local value
              value, i = parse_block(lines, child_i, lines[child_i].indent)
              item[key] = value
            else
              item[key] = nil
              i = i + 1
            end
          else
            item[key] = parse_scalar(rest)
            i = i + 1
          end

          local child_i = next_significant(lines, i)
          if child_i and lines[child_i].indent > indent then
            local tail, next_i = parse_mapping(lines, child_i, lines[child_i].indent)
            for tail_key, tail_value in pairs(tail) do
              item[tail_key] = tail_value
            end
            i = next_i
          end

          out[#out + 1] = item
        else
          out[#out + 1] = parse_scalar(item_text)
          i = i + 1
        end
      end
    end
  end

  return out, i
end

parse_block = function(lines, i, indent)
  local line = lines[i]
  if line.content:match("^%-") then
    return parse_sequence(lines, i, indent)
  end
  return parse_mapping(lines, i, indent)
end

function yml.parse(text)
  local lines = split_lines(text)
  local start = next_significant(lines, 1)
  if not start then
    return {}
  end

  local value = parse_block(lines, start, lines[start].indent)
  return value
end

function yml.read(path)
  local text

  if love and love.filesystem and love.filesystem.read then
    local content, err = love.filesystem.read(path)
    if not content then
      error("yml.read: cannot read '" .. tostring(path) .. "': " .. tostring(err))
    end
    text = content
  elseif usagi and usagi.read_text then
    text = usagi.read_text(path)
  else
    error("yml.read: no filesystem backend available")
  end

  return yml.parse(text)
end

return yml
