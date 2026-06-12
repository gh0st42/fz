package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func runUpdate() error {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", ghOwner, ghRepo)

	req, _ := http.NewRequest("GET", apiURL, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "fz/"+version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch release info: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API returned %s", resp.Status)
	}

	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return fmt.Errorf("parse release info: %w", err)
	}

	latest := strings.TrimPrefix(rel.TagName, "v")
	if !semverNewer(latest, version) {
		fmt.Printf("Already up to date (%s).\n", version)
		return nil
	}
	fmt.Printf("New version available: %s → %s\n", version, latest)

	// Asset name matches the release workflow output pattern.
	wantName := fmt.Sprintf("fz-%s-%s-%s", rel.TagName, runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		wantName += ".exe"
	}

	var downloadURL string
	for _, a := range rel.Assets {
		if a.Name == wantName {
			downloadURL = a.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		return fmt.Errorf("no release asset for %s/%s (expected %q)", runtime.GOOS, runtime.GOARCH, wantName)
	}

	// Resolve the real binary path (follow symlinks so we replace the file, not the link).
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate binary: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("resolve binary path: %w", err)
	}
	exeDir := filepath.Dir(exePath)

	// Download into the same directory so the rename is atomic (same filesystem).
	tmp, err := os.CreateTemp(exeDir, "fz-update-*")
	if err != nil {
		return fmt.Errorf("create temp file (check write permission on %s): %w", exeDir, err)
	}
	tmpPath := tmp.Name()
	success := false
	defer func() {
		tmp.Close()
		if !success {
			os.Remove(tmpPath)
		}
	}()

	fmt.Printf("Downloading %s ...\n", wantName)
	dlReq, _ := http.NewRequest("GET", downloadURL, nil)
	dlReq.Header.Set("User-Agent", "fz/"+version)
	dlResp, err := http.DefaultClient.Do(dlReq)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer dlResp.Body.Close()
	if dlResp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %s", dlResp.Status)
	}

	if _, err := io.Copy(tmp, dlResp.Body); err != nil {
		return fmt.Errorf("write download: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("flush download: %w", err)
	}

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}

	// On Windows, the running executable cannot be overwritten directly.
	// Rename it out of the way first; the process continues from its open handle.
	if runtime.GOOS == "windows" {
		oldPath := exePath + ".old"
		_ = os.Remove(oldPath)
		if err := os.Rename(exePath, oldPath); err != nil {
			return fmt.Errorf("move current binary aside: %w", err)
		}
		fmt.Printf("Previous binary saved as %s.old (safe to delete)\n", filepath.Base(exePath))
	}

	if err := os.Rename(tmpPath, exePath); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}
	success = true

	fmt.Printf("Updated to %s.\n", latest)
	return nil
}

// semverNewer returns true when candidate is strictly newer than current.
// Both strings should be bare semver without a leading "v".
func semverNewer(candidate, current string) bool {
	ca := parseSemver(candidate)
	cb := parseSemver(current)
	n := len(ca)
	if len(cb) > n {
		n = len(cb)
	}
	for i := 0; i < n; i++ {
		a, b := 0, 0
		if i < len(ca) {
			a = ca[i]
		}
		if i < len(cb) {
			b = cb[i]
		}
		if a > b {
			return true
		}
		if a < b {
			return false
		}
	}
	return false
}

func parseSemver(s string) []int {
	parts := strings.Split(s, ".")
	out := make([]int, len(parts))
	for i, p := range parts {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}
