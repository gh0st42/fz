package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	reTitleConf  = regexp.MustCompile(`t\.window\.title\s*=\s*"([^"]*)"`)
	reIdentConf  = regexp.MustCompile(`t\.identity\s*=\s*"([^"]*)"`)
	reAuthorMain = regexp.MustCompile(`^--[^b]*by\s+(.+)$`)
)

func runRefresh(args []string) error {
	fs2 := flag.NewFlagSet("refresh", flag.ContinueOnError)
	yes := fs2.Bool("yes", false, "replace all existing files without prompting")
	if err := fs2.Parse(args); err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	source, cleanup, err := getTemplateFS()
	if err != nil {
		return err
	}
	defer cleanup()

	// Try to derive project info from existing files without prompting.
	info, needPrompt := readExistingInfo(cwd)

	r := bufio.NewReader(os.Stdin)
	infoResolved := false

	resolveInfo := func() error {
		if infoResolved {
			return nil
		}
		if needPrompt {
			info, err = gatherProjectInfo(filepath.Base(cwd))
			if err != nil {
				return err
			}
		}
		infoResolved = true
		return nil
	}

	diffBin, _ := exec.LookPath("diff")

	return walkTemplates(source, func(relPath string) error {
		targetPath := filepath.Join(cwd, filepath.FromSlash(relPath))

		if filepath.Base(targetPath) == ".keep" {
			return os.MkdirAll(filepath.Dir(targetPath), 0o755)
		}

		existingStat, statErr := os.Stat(targetPath)
		missing := os.IsNotExist(statErr)
		if statErr != nil && !missing {
			return statErr
		}

		// Read and render the template content now (needed for size comparison and diff).
		raw, err := fs.ReadFile(source, relPath)
		if err != nil {
			return err
		}

		if missing {
			base := filepath.Base(relPath)
			if base == "main.lua" || base == "conf.lua" {
				if err := resolveInfo(); err != nil {
					return err
				}
			}
			contents, err := applyTemplate(relPath, raw, info)
			if err != nil {
				return fmt.Errorf("render %s: %w", relPath, err)
			}
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(targetPath, contents, 0o644); err != nil {
				return err
			}
			fmt.Printf("Added   %s\n", relPath)
			return nil
		}

		// File exists — render template so we can show size delta.
		base := filepath.Base(relPath)
		if base == "main.lua" || base == "conf.lua" {
			if err := resolveInfo(); err != nil {
				return err
			}
		}
		contents, err := applyTemplate(relPath, raw, info)
		if err != nil {
			return fmt.Errorf("render %s: %w", relPath, err)
		}

		existingSize := existingStat.Size()
		newSize := int64(len(contents))
		sizeDelta := newSize - existingSize
		sizeInfo := fmt.Sprintf("%s → %s", formatSize(existingSize), formatSize(newSize))
		if sizeDelta > 0 {
			sizeInfo += fmt.Sprintf(" (+%s)", formatSize(sizeDelta))
		} else if sizeDelta < 0 {
			sizeInfo += fmt.Sprintf(" (-%s)", formatSize(-sizeDelta))
		} else {
			sizeInfo += " (same size)"
		}

		if *yes {
			if err := os.WriteFile(targetPath, contents, 0o644); err != nil {
				return err
			}
			fmt.Printf("Replaced %s  [%s]\n", relPath, sizeInfo)
			return nil
		}

		// Interactive prompt — loop until user gives a clear answer.
		diffHint := ""
		if diffBin != "" {
			diffHint = "/d"
		}
		for {
			fmt.Printf("Replace %s  [%s]? [y/N%s]: ", relPath, sizeInfo, diffHint)
			line, readErr := r.ReadString('\n')
			if readErr != nil && line == "" {
				fmt.Println()
				fmt.Printf("Kept    %s\n", relPath)
				return nil
			}
			answer := strings.ToLower(strings.TrimSpace(line))

			switch answer {
			case "y", "yes":
				if err := os.WriteFile(targetPath, contents, 0o644); err != nil {
					return err
				}
				fmt.Printf("Replaced %s\n", relPath)
				return nil
			case "d", "diff":
				if diffBin == "" {
					fmt.Println("diff not found in PATH")
					continue
				}
				if err := runDiff(diffBin, targetPath, relPath, contents); err != nil {
					fmt.Fprintf(os.Stderr, "diff: %v\n", err)
				}
				// Ask again after showing the diff.
				continue
			default:
				fmt.Printf("Kept    %s\n", relPath)
				return nil
			}
		}
	})
}

// runDiff writes the new content to a temp file and runs diff against the existing file.
func runDiff(diffBin, existingPath, relPath string, newContents []byte) error {
	tmp, err := os.CreateTemp("", "fz-refresh-*-"+filepath.Base(relPath))
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(newContents); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	cmd := exec.Command(diffBin, "--color=auto", "-u",
		"--label", relPath+" (current)",
		"--label", relPath+" (template)",
		existingPath, tmp.Name())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// diff exits 1 when files differ — that's expected, not an error.
	_ = cmd.Run()
	return nil
}

func formatSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

// readExistingInfo tries to extract project info from conf.lua and main.lua
// without prompting. needPrompt is true when essential fields could not be read.
func readExistingInfo(dir string) (info projectInfo, needPrompt bool) {
	confData, err := os.ReadFile(filepath.Join(dir, "conf.lua"))
	if err == nil {
		if m := reTitleConf.FindSubmatch(confData); m != nil {
			info.Title = string(m[1])
		}
		if m := reIdentConf.FindSubmatch(confData); m != nil {
			info.Identity = string(m[1])
		}
	}

	mainData, err := os.ReadFile(filepath.Join(dir, "main.lua"))
	if err == nil {
		for _, line := range strings.SplitN(string(mainData), "\n", 5) {
			if m := reAuthorMain.FindStringSubmatch(line); m != nil {
				info.Author = strings.TrimSpace(m[1])
				break
			}
		}
	}

	needPrompt = info.Title == ""
	return info, needPrompt
}
