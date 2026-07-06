package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

//go:embed 3pp/beepbox/beepbox_offline.html
var beepboxHTML []byte

func runBgm(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: fz bgm <new|edit> <songname>")
	}

	sub := args[0]
	name := strings.TrimSuffix(args[1], ".html")
	bgmDir := "bgm"
	songPath := filepath.Join(bgmDir, name+".html")

	switch sub {
	case "new":
		if err := os.MkdirAll(bgmDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(songPath, beepboxHTML, 0o644); err != nil {
			return err
		}
		fmt.Printf("Created %s\n", songPath)
		return openInBrowser(songPath)
	case "edit":
		if _, err := os.Stat(songPath); err != nil {
			return fmt.Errorf("song not found: %s", songPath)
		}
		return openInBrowser(songPath)
	default:
		return fmt.Errorf("unknown bgm subcommand %q — use new or edit", sub)
	}
}

func openInBrowser(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", abs)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", abs)
	default:
		cmd = exec.Command("xdg-open", abs)
	}
	return cmd.Start()
}
