package main

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	reTitleConf  = regexp.MustCompile(`t\.window\.title\s*=\s*"([^"]*)"`)
	reIdentConf  = regexp.MustCompile(`t\.identity\s*=\s*"([^"]*)"`)
	reAuthorMain = regexp.MustCompile(`^--[^b]*by\s+(.+)$`)
)

func runRefresh() error {
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

	return walkTemplates(source, func(relPath string) error {
		targetPath := filepath.Join(cwd, filepath.FromSlash(relPath))

		if filepath.Base(targetPath) == ".keep" {
			return os.MkdirAll(filepath.Dir(targetPath), 0o755)
		}

		_, statErr := os.Stat(targetPath)
		missing := os.IsNotExist(statErr)
		if statErr != nil && !missing {
			return statErr
		}

		if missing {
			base := filepath.Base(relPath)
			if base == "main.lua" || base == "conf.lua" {
				if err := resolveInfo(); err != nil {
					return err
				}
			}
			raw, err := fs.ReadFile(source, relPath)
			if err != nil {
				return err
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
			fmt.Printf("Added %s\n", relPath)
			return nil
		}

		fmt.Printf("Replace %s? [y/N]: ", relPath)
		line, err := r.ReadString('\n')
		if err != nil && line == "" {
			fmt.Println()
			fmt.Printf("Kept %s\n", relPath)
			return nil
		}
		if !strings.EqualFold(strings.TrimSpace(line), "y") {
			fmt.Printf("Kept %s\n", relPath)
			return nil
		}

		base := filepath.Base(relPath)
		if base == "main.lua" || base == "conf.lua" {
			if err := resolveInfo(); err != nil {
				return err
			}
		}
		raw, err := fs.ReadFile(source, relPath)
		if err != nil {
			return err
		}
		contents, err := applyTemplate(relPath, raw, info)
		if err != nil {
			return fmt.Errorf("render %s: %w", relPath, err)
		}
		if err := os.WriteFile(targetPath, contents, 0o644); err != nil {
			return err
		}
		fmt.Printf("Replaced %s\n", relPath)
		return nil
	})
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
