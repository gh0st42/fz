package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// runGame launches the game in the current directory using the love or love2d
// binary found on PATH. It exits with a clear error when neither is available.
func runGame() error {
	loveBin, err := findLoveBinary()
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	cmd := exec.Command(loveBin, cwd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Propagate the game's own exit code as a plain message so the
			// top-level handler doesn't double-print "error: exit status N".
			return fmt.Errorf("love exited with status %d", exitErr.ExitCode())
		}
		return err
	}

	return nil
}

func findLoveBinary() (string, error) {
	for _, name := range []string{"love", "love2d"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf(
		"neither 'love' nor 'love2d' found in PATH\n" +
			"Install Love2D from https://love2d.org and make sure it is on your PATH",
	)
}
