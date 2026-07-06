package main

import (
	"bufio"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type distPattern struct {
	negate   bool
	dirOnly  bool
	anchored bool // leading / → anchored to project root
	pattern  string
}

func loadDistignore(rootDir string) ([]distPattern, error) {
	f, err := os.Open(filepath.Join(rootDir, ".distignore"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []distPattern
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		s := strings.TrimRight(sc.Text(), " \t\r")
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		p := distPattern{}
		if strings.HasPrefix(s, "!") {
			p.negate = true
			s = s[1:]
		}
		if strings.HasSuffix(s, "/") {
			p.dirOnly = true
			s = strings.TrimSuffix(s, "/")
		}
		if strings.HasPrefix(s, "/") {
			p.anchored = true
			s = s[1:]
		}
		p.pattern = s
		out = append(out, p)
	}
	return out, sc.Err()
}

// distignoreMatch reports whether relPath should be excluded by the rule set.
// relPath must use forward slashes and be relative to the project root.
// isDir must be true when relPath refers to a directory.
// Rules are evaluated in order; the last matching rule wins (same as gitignore).
func distignoreMatch(patterns []distPattern, relPath string, isDir bool) bool {
	ignored := false
	for _, p := range patterns {
		if p.dirOnly && !isDir {
			continue
		}
		if distPatternMatch(p, relPath) {
			ignored = !p.negate
		}
	}
	return ignored
}

func distPatternMatch(p distPattern, relPath string) bool {
	pat := p.pattern
	// Anchored or slash-containing patterns match the full relative path.
	if p.anchored || strings.Contains(pat, "/") {
		return globMatch(pat, relPath)
	}
	// Patterns with ** imply a full-path match.
	if strings.Contains(pat, "**") {
		return globMatch(pat, relPath)
	}
	// Plain patterns (no slash, no **) match against the base name only,
	// so "*.lua" excludes every .lua file anywhere in the tree.
	base := path.Base(relPath)
	ok, _ := path.Match(pat, base)
	return ok
}

// globMatch matches a gitignore-style glob against a forward-slash path.
// Supports * (any sequence except /), ? (one char except /), and ** (any depth).
func globMatch(pattern, name string) bool {
	return dsMatch(strings.Split(pattern, "/"), 0, strings.Split(name, "/"), 0)
}

func dsMatch(pp []string, pi int, np []string, ni int) bool {
	for pi < len(pp) {
		seg := pp[pi]
		if seg == "**" {
			// ** consumes zero or more complete path segments.
			for i := ni; i <= len(np); i++ {
				if dsMatch(pp, pi+1, np, i) {
					return true
				}
			}
			return false
		}
		if ni >= len(np) {
			return false
		}
		ok, _ := path.Match(seg, np[ni])
		if !ok {
			return false
		}
		pi++
		ni++
	}
	return ni == len(np)
}
