-- {{.Title}}{{if .Author}} by {{.Author}}{{end}}

function love.load()
  love.window.setTitle("{{.Title}}")
end

function love.update(dt)
  -- Update game state.
end

function love.draw()
  love.graphics.print("Hello, Love2D!", 20, 20)
end
