//go:build windows

package windows

import (
	"testing"

	"github.com/kang-sw/gotto-hando/internal/backend"
)

// TestBuildExecArgvShell locks buildExecArgv's shell routing
// (help-windows.txt "exec ON WINDOWS": `cmd /C <payload>`, always cmd):
// %ComSpec% /C <payload>, falling back to cmd.exe when ComSpec is unset;
// non-shell runs Argv directly.
func TestBuildExecArgvShell(t *testing.T) {
	t.Run("uses %ComSpec%", func(t *testing.T) {
		t.Setenv("ComSpec", `C:\Windows\system32\cmd.exe`)
		got := buildExecArgv(backend.ExecReq{Shell: true, Cmd: "dir"})
		want := []string{`C:\Windows\system32\cmd.exe`, "/C", "dir"}
		if !strSliceEqual(got, want) {
			t.Errorf("argv = %v, want %v", got, want)
		}
	})
	t.Run("falls back to cmd.exe when ComSpec is empty", func(t *testing.T) {
		t.Setenv("ComSpec", "")
		got := buildExecArgv(backend.ExecReq{Shell: true, Cmd: "dir"})
		want := []string{"cmd.exe", "/C", "dir"}
		if !strSliceEqual(got, want) {
			t.Errorf("argv = %v, want %v", got, want)
		}
	})
	t.Run("non-shell uses Argv directly", func(t *testing.T) {
		got := buildExecArgv(backend.ExecReq{Argv: []string{"blender.exe", "--version"}})
		want := []string{"blender.exe", "--version"}
		if !strSliceEqual(got, want) {
			t.Errorf("argv = %v, want %v", got, want)
		}
	})
}

func strSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestCappedWriterTruncation mirrors darwin's exec_test.go coverage of the
// duplicated cappedWriter: writes past the byte budget are silently dropped
// and Truncated is set, without the Writer ever failing.
func TestCappedWriterTruncation(t *testing.T) {
	w := &cappedWriter{limit: 8}
	n, err := w.Write([]byte("0123456789"))
	if err != nil {
		t.Fatalf("Write() err = %v, want nil", err)
	}
	if n != 10 {
		t.Errorf("Write() n = %d, want 10 (a capped writer never fails or short-reports)", n)
	}
	if w.buf.String() != "01234567" {
		t.Errorf("buf = %q, want %q", w.buf.String(), "01234567")
	}
	if !w.truncated {
		t.Errorf("truncated = false, want true")
	}
}

// TestDecodeCodePageCP949 feeds decodeCodePage a CP949 (Korean) byte
// fixture with one undecodable byte in the middle, asserting the surrounding
// text decodes correctly and the bad byte becomes exactly one U+FFFD
// (help-windows.txt "exec ON WINDOWS": "Undecodable bytes become U+FFFD").
// 0x68 0x69 is ASCII "hi"; 0x80 is an unassigned CP949 lead byte (invalid
// standalone); 0xB0 0xA1 is CP949 for U+AC00 ("가"). This proves the
// short-window fallback correctly re-syncs to a valid double-byte character
// immediately after a single bad byte, rather than shredding it further.
func TestDecodeCodePageCP949(t *testing.T) {
	const cp949 = 949
	b := []byte{0x68, 0x69, 0x80, 0xB0, 0xA1}
	got := decodeCodePage(b, cp949)
	want := "hi\uFFFD\uAC00"
	if got != want {
		t.Fatalf("decodeCodePage(%x, cp949) = %q, want %q", b, got, want)
	}
}

// TestDecodeCodePageCleanBufferFastPath proves a fully valid buffer decodes
// via the single-call fast path (no U+FFFD introduced) - the common case
// help-windows.txt describes for a Korean-locale `exec[shell]dir`.
func TestDecodeCodePageCleanBufferFastPath(t *testing.T) {
	const cp949 = 949
	b := []byte{0xB0, 0xA1, 0x20, 0x68, 0x69} // "가 hi"
	got := decodeCodePage(b, cp949)
	want := "\uAC00 hi"
	if got != want {
		t.Fatalf("decodeCodePage(%x, cp949) = %q, want %q", b, got, want)
	}
}

// TestDecodeCodePageEmpty proves an empty buffer decodes to "" without
// touching MultiByteToWideChar (which rejects a zero-length input).
func TestDecodeCodePageEmpty(t *testing.T) {
	if got := decodeCodePage(nil, 949); got != "" {
		t.Fatalf("decodeCodePage(nil, 949) = %q, want \"\"", got)
	}
}
