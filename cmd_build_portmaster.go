package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func runBuildPortmaster(cwd, projectName, lovePath string) error {
	info, _ := readExistingInfo(cwd)

	// Derive a safe PortMaster directory name from the Love2D identity.
	// Fall back to the directory name if conf.lua hasn't been created yet.
	pmName := strings.ToLower(strings.ReplaceAll(info.Identity, " ", "_"))
	if pmName == "" {
		pmName = strings.ToLower(strings.ReplaceAll(projectName, " ", "_"))
	}
	if info.Title == "" {
		info.Title = projectName
	}
	if info.Author == "" {
		info.Author = toolAuthor
	}

	zipPath := filepath.Join(cwd, "dist", projectName+".portmaster.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)

	// ports/<name>.sh — launcher script
	if err := zipWrite(zw, "ports/"+pmName+".sh", []byte(portmasterScript(pmName))); err != nil {
		zw.Close()
		return err
	}

	// ports/<name>/port.json — metadata
	portJSON, err := portmasterJSON(info.Title, info.Author, pmName)
	if err != nil {
		zw.Close()
		return err
	}
	if err := zipWrite(zw, "ports/"+pmName+"/port.json", portJSON); err != nil {
		zw.Close()
		return err
	}

	// ports/<name>/<name>.love — the game archive
	loveData, err := os.ReadFile(lovePath)
	if err != nil {
		zw.Close()
		return err
	}
	if err := zipWrite(zw, "ports/"+pmName+"/"+pmName+".love", loveData); err != nil {
		zw.Close()
		return err
	}

	if err := zw.Close(); err != nil {
		return err
	}

	fmt.Printf("Built %s (%s)\n", zipPath, fileSize(zipPath))
	fmt.Println("Install: copy the zip to your handheld and extract it into the PortMaster ports folder.")
	return nil
}

func zipWrite(zw *zip.Writer, name string, data []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func portmasterScript(gameName string) string {
	return fmt.Sprintf(`#!/bin/bash

XDG_DATA_HOME=${XDG_DATA_HOME:-$HOME/.local/share}

if [ -d "/opt/system/Tools/PortMaster/" ]; then
  controlfolder="/opt/system/Tools/PortMaster"
elif [ -d "/opt/tools/PortMaster/" ]; then
  controlfolder="/opt/tools/PortMaster"
elif [ -d "$XDG_DATA_HOME/PortMaster/" ]; then
  controlfolder="$XDG_DATA_HOME/PortMaster"
else
  controlfolder="/roms/ports/PortMaster"
fi

source $controlfolder/control.txt
get_controls

GAMEDIR="/roms/ports/%s"
cd $GAMEDIR

$ESUDO chmod 666 /dev/uinput

$GPTOKEYB "love" -c "./gamecontrollerdb.txt" &
PM_DATA=$GAMEDIR $controlfolder/pm_runtime love-11.5.aarch64.squashfs "love" --fused %s.love
$ESUDO kill -9 $(id -u) 0
`, gameName, gameName)
}

func portmasterJSON(title, author, gameName string) ([]byte, error) {
	doc := map[string]any{
		"name":    title,
		"desc":    "",
		"genres":  []string{},
		"porter":  author,
		"image":   map[string]any{},
		"exec":    gameName + ".sh",
		"rtr":     false,
		"runtime": "love-11.5.aarch64.squashfs",
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
