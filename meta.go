package main

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
)

const pngMetaKey = "fz_meta"

// tileMeta is serialised as JSON into a PNG tEXt chunk with key "fz_meta".
type tileMeta struct {
	TileSize int              `json:"tile_size"`
	Flags    map[string]uint8 `json:"flags,omitempty"` // tile-id string → 8-bit mask
}

// pngChunk holds one parsed PNG chunk.
type pngChunk struct {
	typ string
	raw []byte // full encoded bytes: length(4)+type(4)+data(N)+crc(4)
	dat []byte // data slice into raw
}

func makePNGChunk(typ string, data []byte) []byte {
	out := make([]byte, 12+len(data))
	binary.BigEndian.PutUint32(out[0:], uint32(len(data)))
	copy(out[4:], typ)
	copy(out[8:], data)
	binary.BigEndian.PutUint32(out[8+len(data):], crc32.Checksum(out[4:8+len(data)], crc32.IEEETable))
	return out
}

func parsePNGChunks(buf []byte) ([]pngChunk, error) {
	if len(buf) < 8 || string(buf[:8]) != "\x89PNG\r\n\x1a\n" {
		return nil, fmt.Errorf("not a PNG file")
	}
	var chunks []pngChunk
	pos := 8
	for pos+12 <= len(buf) {
		n := int(binary.BigEndian.Uint32(buf[pos:]))
		if pos+12+n > len(buf) {
			break
		}
		c := pngChunk{
			typ: string(buf[pos+4 : pos+8]),
			raw: buf[pos : pos+12+n],
			dat: buf[pos+8 : pos+8+n],
		}
		chunks = append(chunks, c)
		pos += 12 + n
		if c.typ == "IEND" {
			break
		}
	}
	return chunks, nil
}

// isOurTextChunk returns true when c is the tEXt chunk with our metadata key.
func isOurTextChunk(c pngChunk) bool {
	if c.typ != "tEXt" {
		return false
	}
	for i, b := range c.dat {
		if b == 0 {
			return string(c.dat[:i]) == pngMetaKey
		}
	}
	return false
}

func readPNGMeta(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	chunks, err := parsePNGChunks(raw)
	if err != nil {
		return "", err
	}
	for _, c := range chunks {
		if !isOurTextChunk(c) {
			continue
		}
		for i, b := range c.dat {
			if b == 0 {
				return string(c.dat[i+1:]), nil
			}
		}
	}
	return "", nil
}

func writePNGMeta(path, jsonStr string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	chunks, err := parsePNGChunks(raw)
	if err != nil {
		return err
	}
	out := []byte("\x89PNG\r\n\x1a\n")
	for _, c := range chunks {
		if isOurTextChunk(c) {
			continue // drop old metadata chunk
		}
		if c.typ == "IEND" {
			// Insert fresh metadata chunk before IEND.
			cd := append([]byte(pngMetaKey+"\x00"), []byte(jsonStr)...)
			out = append(out, makePNGChunk("tEXt", cd)...)
		}
		out = append(out, c.raw...)
	}
	return os.WriteFile(path, out, 0o644)
}
