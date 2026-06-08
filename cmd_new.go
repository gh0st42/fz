package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"unicode"
)

type projectInfo struct {
	Title    string
	Identity string
	Author   string
}

func runNew(args []string) error {
	if len(args) != 1 {
		return errors.New("new requires exactly one argument: project name")
	}

	projectDir := args[0]
	if projectDir == "" || projectDir == "." {
		return errors.New("invalid project name")
	}

	if _, err := os.Stat(projectDir); err == nil {
		return fmt.Errorf("directory %q already exists", projectDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	info, err := gatherProjectInfo(projectDir)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return err
	}

	if err := initializeProject(projectDir, info); err != nil {
		return err
	}

	gitInit(projectDir)

	fmt.Printf("Created Love2D project in %s\n", projectDir)
	return nil
}

func runInit() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	info, err := gatherProjectInfo(filepath.Base(cwd))
	if err != nil {
		return err
	}

	if err := initializeProject(".", info); err != nil {
		return err
	}

	gitInit(cwd)

	fmt.Println("Initialized Love2D project in current directory")
	return nil
}

func gatherProjectInfo(defaultTitle string) (projectInfo, error) {
	r := bufio.NewReader(os.Stdin)

	title, err := promptString(r, "Game title", defaultTitle)
	if err != nil {
		return projectInfo{}, err
	}

	author, err := promptString(r, "Author", gitConfigUser())
	if err != nil {
		return projectInfo{}, err
	}

	return projectInfo{
		Title:    title,
		Identity: toIdentity(title),
		Author:   author,
	}, nil
}

func initializeProject(root string, info projectInfo) error {
	files := []struct {
		template string
		target   string
	}{
		{template: "templates/main.lua", target: "main.lua"},
		{template: "templates/conf.lua", target: "conf.lua"},
		{template: "templates/.gitignore", target: ".gitignore"},
		{template: "templates/.luarc.json", target: ".luarc.json"},
	}

	for _, file := range files {
		raw, err := templatesFS.ReadFile(file.template)
		if err != nil {
			return err
		}

		contents, err := renderTemplate(raw, info)
		if err != nil {
			return fmt.Errorf("render %s: %w", file.template, err)
		}

		targetPath := filepath.Join(root, file.target)
		if err := writeIfNotExists(targetPath, contents, 0o644); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
		return err
	}

	return nil
}

func renderTemplate(src []byte, info projectInfo) ([]byte, error) {
	tmpl, err := template.New("").Parse(string(src))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, info); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gitInit(dir string) {
	if _, err := exec.LookPath("git"); err != nil {
		return
	}
	cmd := exec.Command("git", "init", dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}

func promptString(r *bufio.Reader, label, defaultVal string) (string, error) {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultVal, nil
	}
	return line, nil
}

func gitConfigUser() string {
	out, err := exec.Command("git", "config", "user.name").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// toIdentity converts a title to a lowercase identifier with underscores.
func toIdentity(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if unicode.IsSpace(r) || r == '-' {
			b.WriteRune('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func writeIfNotExists(path string, contents []byte, mode fs.FileMode) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("file %q already exists", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.WriteFile(path, contents, mode)
}
