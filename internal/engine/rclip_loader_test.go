package engine

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

func TestLoadRClipNormalizesAdvertisedImageFormatsAndOverrides(t *testing.T) {
	dir := t.TempDir()
	im := image.NewRGBA(image.Rect(0, 0, 2, 3))
	im.Set(1, 2, color.RGBA{R: 20, G: 30, B: 40, A: 255})
	formats := []struct {
		name  string
		write func(*bytes.Buffer) error
	}{
		{"png", func(b *bytes.Buffer) error { return png.Encode(b, im) }},
		{"jpg", func(b *bytes.Buffer) error { return jpeg.Encode(b, im, nil) }},
		{"gif", func(b *bytes.Buffer) error { return gif.Encode(b, im, nil) }},
		{"bmp", func(b *bytes.Buffer) error { return bmp.Encode(b, im) }},
		{"tiff", func(b *bytes.Buffer) error { return tiff.Encode(b, im, nil) }},
	}
	for _, tc := range formats {
		t.Run(tc.name, func(t *testing.T) {
			var data bytes.Buffer
			if err := tc.write(&data); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "input."+tc.name)
			if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := loadRClip(path, "auto")
			if err != nil {
				t.Fatal(err)
			}
			if got.Type != "image" || got.Bytes != data.Len() || got.Image.Width != 2 || got.Image.Height != 3 || len(got.Image.PNG) == 0 || len(got.Image.TIFF) == 0 || len(got.Image.RGBA) != 24 {
				t.Fatalf("loaded = %+v", got)
			}
		})
	}
	textPath := filepath.Join(dir, "note.unknown")
	if err := os.WriteFile(textPath, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := loadRClip(textPath, "auto"); err != nil || got.Type != "text" || got.Text != "hello" {
		t.Fatalf("auto text = %+v, %v", got, err)
	}
	if got, err := loadRClip(textPath, "text"); err != nil || got.Type != "text" {
		t.Fatalf("txt override = %+v, %v", got, err)
	}
	if _, err := loadRClip(textPath, "image"); err == nil {
		t.Fatal("img override accepted text")
	}
}

func TestLoadRClipRejectsRuntimeFailures(t *testing.T) {
	dir := t.TempDir()
	if _, err := loadRClip(filepath.Join(dir, "missing.txt"), "auto"); err == nil {
		t.Error("missing file accepted")
	}
	bad := filepath.Join(dir, "bad.txt")
	if err := os.WriteFile(bad, []byte{0xff}, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRClip(bad, "auto"); err == nil {
		t.Error("invalid UTF-8 accepted")
	}
	if _, err := loadRClip(bad, "image"); err == nil {
		t.Error("malformed forced image accepted")
	}
	big := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(big, make([]byte, 64*1024*1024+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRClip(big, "auto"); err == nil {
		t.Error("oversize file accepted")
	}
	// A PNG signature plus IHDR is sufficient for DecodeConfig; the loader
	// must reject its dimensions before allocating or decoding pixel data.
	huge := filepath.Join(dir, "huge.png")
	if err := os.WriteFile(huge, []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82, 0, 1, 0, 0, 0, 1, 0, 1, 8, 6, 0, 0, 0}, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRClip(huge, "auto"); err == nil {
		t.Error("over-pixel image accepted")
	}
}
