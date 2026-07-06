package main

import (
	"archive/zip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func createLoveArchive(rootDir, archivePath string) error {
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	defer archiveFile.Close()

	zipWriter := zip.NewWriter(archiveFile)
	defer zipWriter.Close()

	patterns, err := loadDistignore(rootDir)
	if err != nil {
		return err
	}

	return filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if path == rootDir {
			return nil
		}

		relPath, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}

		relPath = filepath.Clean(relPath)
		base := d.Name()

		// Skip entire directory trees that should never ship.
		if d.IsDir() {
			switch base {
			case "dist", ".git", "node_modules", ".idea", ".vscode":
				return filepath.SkipDir
			}
			if strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			if distignoreMatch(patterns, filepath.ToSlash(relPath), true) {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip file-level noise regardless of directory.
		switch base {
		case ".DS_Store", "Thumbs.db", "desktop.ini":
			return nil
		}
		// Skip hidden files, editor swap/backup files, and .love archives at root.
		if strings.HasPrefix(base, ".") {
			return nil
		}
		if strings.HasSuffix(base, "~") || strings.HasSuffix(base, ".bak") ||
			strings.HasSuffix(base, ".swp") || strings.HasSuffix(base, ".swo") {
			return nil
		}
		// Don't bundle .love files that happen to live in the project root.
		if filepath.Dir(relPath) == "." && strings.HasSuffix(strings.ToLower(base), ".love") {
			return nil
		}
		if distignoreMatch(patterns, filepath.ToSlash(relPath), false) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		if !info.Mode().IsRegular() {
			return nil
		}

		src, err := os.Open(path)
		if err != nil {
			return err
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			src.Close()
			return err
		}
		header.Name = filepath.ToSlash(relPath)
		header.Method = zip.Deflate

		dst, err := zipWriter.CreateHeader(header)
		if err != nil {
			src.Close()
			return err
		}

		if _, err := io.Copy(dst, src); err != nil {
			src.Close()
			return err
		}

		return src.Close()
	})
}
