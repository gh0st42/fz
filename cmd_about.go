package main

import "fmt"

func runAbout() {
	fmt.Printf(`friendzone (fz)  v%s
  
Copyright (c) 2026 %s
MIT License
https://github.com/%s/%s

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
`, version, toolAuthor, ghOwner, ghRepo)

	fmt.Println(`
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Third-party Go libraries
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

github.com/gen2brain/raylib-go  (raylib and raygui Go bindings)
zlib License
Copyright (C) 2016 Milan Nikolic (gen2brain)
https://github.com/gen2brain/raylib-go

  Bundled C library: raylib
  zlib License
  Copyright (c) 2013-2026 Ramon Santamaria (@raysan5)
  https://github.com/raysan5/raylib

  Bundled C library: GLFW
  zlib License
  Copyright (c) 2002-2006 Marcus Geelnard
  Copyright (c) 2006-2019 Camilla Löwy
  https://github.com/glfw/glfw

github.com/ebitengine/purego  v0.10.0
Apache License 2.0
https://github.com/ebitengine/purego

github.com/fsnotify/fsnotify  v1.10.1
BSD 3-Clause License
Copyright © 2012 The Go Authors. All rights reserved.
Copyright © fsnotify Authors. All rights reserved.
https://github.com/fsnotify/fsnotify

github.com/jupiterrider/ffi  v0.7.0
MIT License
Copyright (c) 2024-present JupiterRider
https://github.com/jupiterrider/ffi

golang.org/x/exp  and  golang.org/x/sys
BSD 3-Clause License
Copyright 2009 The Go Authors.
https://pkg.go.dev/golang.org/x

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Third-party Lua libraries  (shipped in new projects)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

classic.lua  —  simple class / prototype-based OOP
MIT License
Copyright (c) 2014 rxi
https://github.com/rxi/classic

json.lua  —  JSON encoder / decoder
MIT License
Copyright (c) 2020 rxi
https://github.com/rxi/json.lua

push.lua  v0.4  —  virtual-resolution canvas
MIT License
Copyright (c) 2020 Ulysse Ramage
https://github.com/Ulysse66/push

tick.lua  —  fixed-timestep game loop
MIT License
Copyright (c) bjornbytes
https://github.com/bjornbytes/tick

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Bundled fonts
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

unscii-8  —  8x8 bitmap font  (fz edit, and shipped in new projects)
Public domain
Viznut and contributors
http://viznut.fi/unscii/

Flexi IBM VGA True  —  8x16 IBM VGA font  (fz edit)
Creative Commons Attribution-ShareAlike 4.0 International (CC BY-SA 4.0)
Copyright (c) 2020 VileR
https://int10h.org/oldschool-pc-fonts/
http://creativecommons.org/licenses/by-sa/4.0/

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Bundled tools
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

BeepBox (offline build)  —  browser-based chiptune music editor
MIT License
Copyright (c) John Nesky and contributing authors
https://github.com/johnnesky/beepbox`)
}
