package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

func runWatch() error {
	loveBin, err := findLoveBinary()
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating watcher: %w", err)
	}
	defer watcher.Close()

	if err := watchDirsRecursive(watcher, cwd); err != nil {
		return err
	}

	var (
		mu    sync.Mutex
		proc  *os.Process
		timer *time.Timer
	)

	startGame := func() {
		mu.Lock()
		defer mu.Unlock()

		if proc != nil {
			_ = proc.Kill()
			proc = nil
		}

		cmd := exec.Command(loveBin, cwd)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin

		if err := cmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "watch: failed to start game: %v\n", err)
			return
		}
		proc = cmd.Process
		go cmd.Wait() //nolint:errcheck
	}

	scheduleRestart := func(name string) {
		mu.Lock()
		defer mu.Unlock()
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(200*time.Millisecond, startGame)
	}

	fmt.Println("watch: starting game…")
	startGame()

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Has(fsnotify.Create) {
				if info, statErr := os.Stat(event.Name); statErr == nil && info.IsDir() {
					_ = watcher.Add(event.Name)
				}
			}
			if isWatchedPath(event.Name, cwd) {
				rel, _ := filepath.Rel(cwd, event.Name)
				fmt.Printf("watch: %s changed, restarting…\n", rel)
				scheduleRestart(event.Name)
			}
		case watchErr, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			fmt.Fprintf(os.Stderr, "watch: %v\n", watchErr)
		}
	}
}

// isWatchedPath returns true for .lua files anywhere and all files under assets/.
func isWatchedPath(path, cwd string) bool {
	if strings.HasSuffix(path, ".lua") {
		return true
	}
	assetsDir := filepath.Join(cwd, "assets") + string(filepath.Separator)
	return strings.HasPrefix(path, assetsDir)
}

func watchDirsRecursive(watcher *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		if name != "." && strings.HasPrefix(name, ".") {
			return filepath.SkipDir
		}
		if name == "dist" {
			return filepath.SkipDir
		}
		return watcher.Add(path)
	})
}
