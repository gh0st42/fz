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
	source, cleanup, err := getTemplateFS()
	if err != nil {
		return err
	}
	defer cleanup()

	return walkTemplates(source, func(relPath string) error {
		target := filepath.Join(root, filepath.FromSlash(relPath))
		if filepath.Base(target) == ".keep" {
			return os.MkdirAll(filepath.Dir(target), 0o755)
		}
		raw, err := fs.ReadFile(source, relPath)
		if err != nil {
			return err
		}
		contents, err := applyTemplate(relPath, raw, info)
		if err != nil {
			return fmt.Errorf("render %s: %w", relPath, err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return writeIfNotExists(target, contents, 0o644)
	})
}

// getTemplateFS resolves the template source in priority order:
//  1. FZ_TEMPLATE env var pointing to a local directory
//  2. FZ_TEMPLATE env var containing a git URL (cloned into a temp dir)
//  3. Embedded templates bundled in the binary
//
// The returned cleanup function must be called when the FS is no longer needed.
func getTemplateFS() (fs.FS, func(), error) {
	src := os.Getenv("FZ_TEMPLATE")
	if src == "" {
		sub, err := fs.Sub(templatesFS, "templates")
		return sub, func() {}, err
	}

	// Local directory
	if info, err := os.Stat(src); err == nil && info.IsDir() {
		return os.DirFS(src), func() {}, nil
	}

	// Git URL — clone into a temp dir
	if _, err := exec.LookPath("git"); err != nil {
		return nil, func() {}, fmt.Errorf("FZ_TEMPLATE=%q looks like a URL but git is not in PATH", src)
	}
	tmpDir, err := os.MkdirTemp("", "fz-template-*")
	if err != nil {
		return nil, func() {}, err
	}
	cleanup := func() { os.RemoveAll(tmpDir) }

	fmt.Fprintf(os.Stderr, "Cloning template from %s ...\n", src)
	cmd := exec.Command("git", "clone", "--depth=1", src, tmpDir)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		cleanup()
		return nil, func() {}, fmt.Errorf("git clone %s: %w", src, err)
	}
	return os.DirFS(tmpDir), cleanup, nil
}

// walkTemplates calls fn for every file in source, passing the slash-separated
// path relative to the FS root.
func walkTemplates(source fs.FS, fn func(relPath string) error) error {
	return fs.WalkDir(source, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		return fn(path)
	})
}

// applyTemplate renders src through Go text/template for the two files that
// carry project metadata (main.lua, conf.lua). All other files pass through
// verbatim so third-party libraries are never modified.
func applyTemplate(relPath string, src []byte, info projectInfo) ([]byte, error) {
	base := filepath.Base(relPath)
	if base != "main.lua" && base != "conf.lua" {
		return src, nil
	}
	return renderTemplate(src, info)
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
