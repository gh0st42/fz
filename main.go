package main

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed templates/main.lua templates/conf.lua templates/.gitignore templates/.luarc.json templates/assets/.keep
var templatesFS embed.FS

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	var err error

	switch os.Args[1] {
	case "new":
		err = runNew(os.Args[2:])
	case "init":
		err = runInit()
	case "build":
		err = runBuild(os.Args[2:])
	case "run":
		err = runGame()
	case "watch":
		err = runWatch()
	case "clean":
		err = runClean()
	case "serve":
		err = runServe(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Printf("%s v%s by %s\n", filepath.Base(os.Args[0]), version, toolAuthor)
		return
	case "help", "-h", "--help":
		printUsage()
		return
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	bin := filepath.Base(os.Args[0])
	fmt.Printf("friendzone (v%s) by %s - Love2D project manager\n", version, toolAuthor)
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Printf("  %s new <name>        Create a new Love2D project directory\n", bin)
	fmt.Printf("  %s init              Initialize a Love2D project in current directory\n", bin)
	fmt.Printf("  %s build [--web] [--compat]  Build dist/<project>.love; --web also builds dist/www via love.js/npx\n", bin)
	fmt.Printf("  %s serve [--port N]  Serve dist/www with the headers required for SharedArrayBuffer (default port 8000)\n", bin)
	fmt.Printf("  %s run               Launch the game in the current directory with love/love2d\n", bin)
	fmt.Printf("  %s watch             Watch .lua files and assets/ and auto-restart the game on changes\n", bin)
	fmt.Printf("  %s clean             Remove dist directory\n", bin)
	fmt.Printf("  %s version           Print version\n", bin)
}
