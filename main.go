package main

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed all:templates
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
	case "refresh":
		err = runRefresh(os.Args[2:])
	case "gfx":
		err = runGfx(os.Args[2:])
	case "map":
		err = runMap(os.Args[2:])
	case "serve":
		err = runServe(os.Args[2:])
	case "bgm":
		err = runBgm(os.Args[2:])
	case "about":
		runAbout()
	case "update":
		err = runUpdate()
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
	fmt.Printf("  %s build [--web] [--compat] [--portmaster]  Build dist/<project>.love; --web → dist/www; --portmaster → PortMaster zip\n", bin)
	fmt.Printf("  %s serve [--port N]  Serve dist/www with the headers required for SharedArrayBuffer (default port 8000)\n", bin)
	fmt.Printf("  %s run               Launch the game in the current directory with love/love2d\n", bin)
	fmt.Printf("  %s watch             Watch .lua files and assets/ and auto-restart the game on changes\n", bin)
	fmt.Printf("  %s refresh [--yes]    Add missing template files; prompt to replace existing ones (--yes skips prompts)\n", bin)
	fmt.Printf("  %s gfx [file]         Open sprite editor (file resolved under assets/gfx/)\n", bin)
	fmt.Printf("  %s map [file]         Open map editor\n", bin)
	fmt.Printf("  %s bgm new <name>     Create bgm/<name>.html from BeepBox and open in browser\n", bin)
	fmt.Printf("  %s bgm edit <name>    Open existing bgm/<name>.html in browser\n", bin)
	fmt.Printf("  %s about             Show license and third-party attribution\n", bin)
	fmt.Printf("  %s clean             Remove dist directory\n", bin)
	fmt.Printf("  %s update            Check for a newer release on GitHub and replace this binary\n", bin)
	fmt.Printf("  %s version           Print version\n", bin)
}
