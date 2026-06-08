package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func runBuild(args []string) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	web := fs.Bool("web", false, "also build a web version using love.js (requires Node.js / npx)")
	compat := fs.Bool("compat", false, "use love.js compatibility mode (no SharedArrayBuffer; works on itch.io)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	projectName := filepath.Base(cwd)
	if projectName == "" || projectName == "." || projectName == string(filepath.Separator) {
		projectName = "project"
	}

	distDir := filepath.Join(cwd, "dist")
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		return err
	}

	archivePath := filepath.Join(distDir, projectName+".love")
	if err := createLoveArchive(cwd, archivePath); err != nil {
		return err
	}

	fmt.Printf("Built %s (%s)\n", archivePath, fileSize(archivePath))

	if *web {
		if err := runBuildWeb(cwd, projectName, archivePath, *compat); err != nil {
			return err
		}
	}

	return nil
}

func runBuildWeb(cwd, projectName, lovePath string, compat bool) error {
	if _, err := os.Stat(lovePath); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf(".love file not found at %s", lovePath)
	}

	wwwDir := filepath.Join(cwd, "dist", "www")
	if err := os.MkdirAll(wwwDir, 0o755); err != nil {
		return err
	}

	// Resolve love.js: prefer a globally-installed binary, fall back to npx.
	loveJSArgs, err := lovejsCommand(lovePath, wwwDir, projectName, compat)
	if err != nil {
		return err
	}

	fmt.Printf("Building web version via love.js (%s) ...\n", loveJSArgs[0])

	cmd := exec.Command(loveJSArgs[0], loveJSArgs[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("love.js: %w", err)
	}

	fmt.Printf("Built web version in %s\n", wwwDir)
	fmt.Println("Tip: preview with  fz serve  (sets the headers required for SharedArrayBuffer)")
	fmt.Println("     Use --compat only when deploying to hosts that block those headers (e.g. older itch.io embeds)")
	return nil
}

func fileSize(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "?"
	}
	n := info.Size()
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// lovejsCommand returns the argv slice to invoke love.js, choosing between a
// globally-installed binary and npx. Returns an error when neither Node.js nor
// a global love.js install is available.
func lovejsCommand(lovePath, wwwDir, title string, compat bool) ([]string, error) {
	// love.js [options] <input> <output>
	buildArgs := func(bin string, prefix []string) []string {
		args := append(prefix, "-t", title)
		if compat {
			args = append(args, "-c")
		}
		args = append(args, lovePath, wwwDir)
		_ = bin
		return args
	}

	// 1. Globally-installed love.js binary.
	if path, err := exec.LookPath("love.js"); err == nil {
		return buildArgs(path, []string{path}), nil
	}

	// 2. npx (ships with Node.js ≥ 5.2).
	if npx, err := exec.LookPath("npx"); err == nil {
		// --yes auto-confirms package download without an interactive prompt.
		return buildArgs(npx, []string{npx, "--yes", "love.js"}), nil
	}

	return nil, fmt.Errorf(
		"love.js not found\n" +
			"Install Node.js (https://nodejs.org) and then run:\n" +
			"  npm install -g love.js",
	)
}
