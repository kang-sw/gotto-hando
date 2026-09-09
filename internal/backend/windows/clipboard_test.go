//go:build windows

package windows

import (
	"encoding/binary"
	"testing"

	"github.com/kang-sw/gotto-hando/internal/backend"
)

func TestDIBFromRGBAUsesTopDownBGRA(t *testing.T) {
	dib := dibFromRGBA(backend.ClipboardImage{
		Width: 1, Height: 2,
		RGBA: []byte{1, 2, 3, 4, 5, 6, 7, 8},
	})
	const headerBytes = 40
	if len(dib) != headerBytes+8 {
		t.Fatalf("len = %d, want %d", len(dib), headerBytes+8)
	}
	if got := int32(binary.LittleEndian.Uint32(dib[4:])); got != 1 {
		t.Errorf("width = %d, want 1", got)
	}
	if got := int32(binary.LittleEndian.Uint32(dib[8:])); got != -2 {
		t.Errorf("height = %d, want -2 (top-down)", got)
	}
	if got := binary.LittleEndian.Uint16(dib[14:]); got != 1 {
		t.Errorf("planes = %d, want 1", got)
	}
	if got := binary.LittleEndian.Uint16(dib[16:]); got != 32 {
		t.Errorf("bit count = %d, want 32", got)
	}
	if got, want := dib[headerBytes:], []byte{3, 2, 1, 4, 7, 6, 5, 8}; string(got) != string(want) {
		t.Errorf("pixels = %v, want %v", got, want)
	}
}
