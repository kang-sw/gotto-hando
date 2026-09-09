package engine

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/ir"
	_ "golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

// rclipLoaded preserves the source-byte count while carrying the normalized
// representation needed by either platform clipboard implementation.
type rclipLoaded struct {
	Type  string
	Bytes int
	Text  string
	Image backend.ClipboardImage
}

func loadRClip(path, requested string) (rclipLoaded, error) {
	info, err := os.Stat(path)
	if err != nil {
		return rclipLoaded{}, fmt.Errorf("cannot stat rclip file: %w", err)
	}
	if info.Size() > ir.MaxRClipBytes {
		return rclipLoaded{}, fmt.Errorf("rclip file exceeds 64 MiB")
	}
	if !info.Mode().IsRegular() {
		return rclipLoaded{}, fmt.Errorf("rclip path is not a regular file")
	}
	f, err := openRClip(path)
	if err != nil {
		return rclipLoaded{}, fmt.Errorf("cannot read rclip file: %w", err)
	}
	defer f.Close()
	// Check the opened descriptor too: the pathname may be replaced between
	// Stat and Open, and a FIFO/device could otherwise block the bridge.
	openedInfo, err := f.Stat()
	if err != nil {
		return rclipLoaded{}, fmt.Errorf("cannot stat opened rclip file: %w", err)
	}
	if !openedInfo.Mode().IsRegular() {
		return rclipLoaded{}, fmt.Errorf("rclip path is not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(ir.MaxRClipBytes)+1))
	if err != nil {
		return rclipLoaded{}, fmt.Errorf("cannot read rclip file: %w", err)
	}
	if len(data) > ir.MaxRClipBytes {
		return rclipLoaded{}, fmt.Errorf("rclip file exceeds 64 MiB")
	}
	typ := requested
	if typ == "" {
		typ = "auto"
	}
	if typ == "auto" {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".tif", ".tiff":
			typ = "image"
		default:
			typ = "text"
		}
	}
	if typ == "text" {
		if !utf8.Valid(data) {
			return rclipLoaded{}, fmt.Errorf("rclip text is not valid UTF-8")
		}
		return rclipLoaded{Type: typ, Bytes: len(data), Text: string(data)}, nil
	}
	if typ != "image" {
		return rclipLoaded{}, fmt.Errorf("invalid rclip type %q", typ)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return rclipLoaded{}, fmt.Errorf("cannot decode rclip image: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > ir.MaxRClipPixels/config.Height {
		return rclipLoaded{}, fmt.Errorf("rclip image dimensions exceed 64 MiPixels")
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return rclipLoaded{}, fmt.Errorf("cannot decode rclip image: %w", err)
	}
	rgba := image.NewRGBA(image.Rect(0, 0, config.Width, config.Height))
	draw.Draw(rgba, rgba.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	var pngData, tiffData bytes.Buffer
	if err := png.Encode(&pngData, rgba); err != nil {
		return rclipLoaded{}, fmt.Errorf("cannot encode rclip PNG: %w", err)
	}
	if err := tiff.Encode(&tiffData, rgba, nil); err != nil {
		return rclipLoaded{}, fmt.Errorf("cannot encode rclip TIFF: %w", err)
	}
	// The RGBA image is tightly packed by image.NewRGBA (stride == width*4).
	pixels := append([]byte(nil), rgba.Pix...)
	return rclipLoaded{Type: typ, Bytes: len(data), Image: backend.ClipboardImage{
		PNG: pngData.Bytes(), TIFF: tiffData.Bytes(), RGBA: pixels,
		Width: config.Width, Height: config.Height,
	}}, nil
}
