//go:build windows

package windows

import (
	"testing"
	"unsafe"
)

// TestShellExecuteInfoWSize pins shellExecuteInfoW's size to the real Win32
// SHELLEXECUTEINFOW struct's 112 bytes on amd64 (cbSize(4)+fMask(4)+
// hwnd(8)+4 LPCWSTR pointers(32)+nShow(4)+4-byte pad+hInstApp(8)+
// lpIDList(8)+lpClass(8)+hkeyClass(8)+dwHotKey(4)+4-byte pad+hIcon(8)+
// hProcess(8) = 112). ShellExecuteExW reads/writes this struct by fixed
// offset; a field reordering that introduces different Go padding than the
// real C ABI would silently corrupt every open() call - same spirit as
// Phase 1's INPUT struct-size assertion in mouse_test.go and this phase's
// bitmapInfoHeader assertion in capture_test.go.
func TestShellExecuteInfoWSize(t *testing.T) {
	const want = 112
	if got := unsafe.Sizeof(shellExecuteInfoW{}); got != want {
		t.Fatalf("sizeof(shellExecuteInfoW) = %d, want %d (the real SHELLEXECUTEINFOW size)", got, want)
	}
}

// TestOpenTargetBasename locks openTargetBasename's split (mirroring
// internal/engine/exec.go's openAppSelector exactly, so b.lastOpenBasename
// compares equal to the sel.Value Windows() receives for the same target):
// a bare word with no "/" is used verbatim; a "/"-containing path is
// reduced to its base name with a trailing ".app" suffix stripped (a
// no-op on a normal Windows path, but kept to stay a faithful mirror).
func TestOpenTargetBasename(t *testing.T) {
	cases := []struct {
		name   string
		target string
		want   string
	}{
		{"bare name", "notepad", "notepad"},
		{"bare name with spaces", "Visual Studio Code", "Visual Studio Code"},
		{"forward-slash path", "C:/tools/notepad.exe", "notepad.exe"},
		{"backslash path stays verbatim (no split)", `C:\tools\notepad.exe`, `C:\tools\notepad.exe`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := openTargetBasename(c.target); got != c.want {
				t.Errorf("openTargetBasename(%q) = %q, want %q", c.target, got, c.want)
			}
		})
	}
}
