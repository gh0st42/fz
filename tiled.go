package main

import (
	"encoding/json"
	"os"
	"strings"
)

type tiledProperty struct {
	Name  string          `json:"name"`
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

type tiledTileData struct {
	ID         int             `json:"id"`
	Properties []tiledProperty `json:"properties,omitempty"`
}

type tiledTilesetJSON struct {
	Columns      int             `json:"columns"`
	Image        string          `json:"image"`
	ImageHeight  int             `json:"imageheight"`
	ImageWidth   int             `json:"imagewidth"`
	Margin       int             `json:"margin"`
	Name         string          `json:"name"`
	Spacing      int             `json:"spacing"`
	TileCount    int             `json:"tilecount"`
	TileHeight   int             `json:"tileheight"`
	TileWidth    int             `json:"tilewidth"`
	Tiles        []tiledTileData `json:"tiles,omitempty"`
	Type         string          `json:"type"`
	Version      string          `json:"version"`
	TiledVersion string          `json:"tiledversion"`
}

type tiledTilesetRefJSON struct {
	FirstGID int    `json:"firstgid"`
	Source   string `json:"source"`
}

// mapObject represents a Tiled rect object (or best-effort other types).
// Unknown fields in loaded TMJ are discarded on re-save.
type mapObject struct {
	ID         int             `json:"id"`
	Name       string          `json:"name"`
	ObjType    string          `json:"type,omitempty"`
	X          float64         `json:"x"`
	Y          float64         `json:"y"`
	Width      float64         `json:"width"`
	Height     float64         `json:"height"`
	Rotation   float64         `json:"rotation,omitempty"`
	Visible    bool            `json:"visible"`
	Ellipse    bool            `json:"ellipse,omitempty"`
	Point      bool            `json:"point,omitempty"`
	Properties []tiledProperty `json:"properties,omitempty"`
}

type tiledLayerJSON struct {
	Data      []uint32     `json:"data,omitempty"`
	DrawOrder string       `json:"draworder,omitempty"`
	Height    int          `json:"height,omitempty"`
	ID        int          `json:"id"`
	Name      string       `json:"name"`
	Objects   *[]mapObject `json:"objects,omitempty"`
	Opacity   float64      `json:"opacity"`
	Type      string       `json:"type"`
	Visible   bool         `json:"visible"`
	Width     int          `json:"width,omitempty"`
	X         int          `json:"x"`
	Y         int          `json:"y"`
}

type tiledMapJSON struct {
	CompressionLevel int                   `json:"compressionlevel"`
	Height           int                   `json:"height"`
	Infinite         bool                  `json:"infinite"`
	Layers           []tiledLayerJSON      `json:"layers"`
	NextLayerID      int                   `json:"nextlayerid"`
	NextObjectID     int                   `json:"nextobjectid"`
	Orientation      string                `json:"orientation"`
	RenderOrder      string                `json:"renderorder"`
	TiledVersion     string                `json:"tiledversion"`
	TileHeight       int                   `json:"tileheight"`
	TileWidth        int                   `json:"tilewidth"`
	Tilesets         []tiledTilesetRefJSON `json:"tilesets"`
	Type             string                `json:"type"`
	Version          string                `json:"version"`
	Width            int                   `json:"width"`
}

func loadTSJFile(path string) (*tiledTilesetJSON, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ts tiledTilesetJSON
	if err := json.Unmarshal(raw, &ts); err != nil {
		return nil, err
	}
	return &ts, nil
}

func saveTSJFile(path string, ts tiledTilesetJSON) error {
	raw, err := json.MarshalIndent(ts, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

// reformatTMJTileData rewrites tile layer "data" arrays so each map row sits on
// one JSON line instead of one element per line, making the file human-readable.
func reformatTMJTileData(src []byte, mapW int) []byte {
	if mapW <= 0 {
		return src
	}
	lines := strings.Split(string(src), "\n")
	out := make([]string, 0, len(lines))
	inData := false
	var dataIndent string
	var vals []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inData {
			out = append(out, line)
			if strings.HasSuffix(trimmed, `"data": [`) {
				inData = true
				lineContent := strings.TrimLeft(line, " \t")
				dataIndent = line[:len(line)-len(lineContent)] + "  "
				vals = nil
			}
		} else {
			if trimmed == "]" || trimmed == "]," {
				for start := 0; start < len(vals); start += mapW {
					end := start + mapW
					if end > len(vals) {
						end = len(vals)
					}
					rowStr := dataIndent + strings.Join(vals[start:end], ", ")
					if end < len(vals) {
						rowStr += ","
					}
					out = append(out, rowStr)
				}
				out = append(out, line)
				inData = false
				vals = nil
			} else {
				v := strings.TrimRight(trimmed, ", ")
				if v != "" {
					vals = append(vals, v)
				}
			}
		}
	}
	return []byte(strings.Join(out, "\n"))
}
