function love.conf(t)
  t.identity = "{{.Identity}}"
  t.version = "11.5"
  t.console = false
  t.window.title = "{{.Title}}"
  t.window.width = 800
  t.window.height = 600
  t.window.resizable = true
end
