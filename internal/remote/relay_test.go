package remote

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kang-sw/gotto-hando/internal/output"
)

// TestRelayProcessErrRestoresLocalSrc asserts an "err" line's Src is
// restored from the local parse by line number, so an inlined [f] line
// reports the src the caller actually wrote (e.g. "paste[f]./cmd.py"),
// not the rewritten/inlined text the remote saw (help-remote.txt HOW IT
// WORKS step 4).
func TestRelayProcessErrRestoresLocalSrc(t *testing.T) {
	path := writeFile(t, "hello")
	origSrc := "paste[f]" + path
	seq := parseInlined(t, []string{origSrc})

	rl := &Relay{LocalSeq: seq}
	wire := `{"line":1,"status":"err","cmd":"paste","src":"paste[]hello","code":"E_CLIPBOARD","msg":"clipboard set failed","t_ms":3}`
	res, rawForJSONL, err := rl.Process([]byte(wire))
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}
	if rawForJSONL != nil {
		t.Errorf("rawForJSONL = %q, want nil for an err line", rawForJSONL)
	}
	if res.Status != "err" || res.Cmd != "paste" {
		t.Fatalf("res = %+v", res)
	}
	if res.Src != origSrc {
		t.Errorf("res.Src = %q, want original %q", res.Src, origSrc)
	}
	if res.ErrCode != output.EClipboard || res.ErrMsg != "clipboard set failed" {
		t.Errorf("res = %+v, want code=%s msg=%q", res, output.EClipboard, "clipboard set failed")
	}
}

// TestRelayProcessErrFallsBackToWireSrcWhenNoLocalOp asserts a missing
// local op (should not happen in practice, but Process must not panic)
// falls back to the wire's own src.
func TestRelayProcessErrFallsBackToWireSrcWhenNoLocalOp(t *testing.T) {
	seq := parseInlined(t, []string{"qinfo"})
	rl := &Relay{LocalSeq: seq}
	wire := `{"line":99,"status":"err","cmd":"k","src":"k[]a","code":"E_INPUT","msg":"boom","t_ms":1}`
	res, _, err := rl.Process([]byte(wire))
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}
	if res.Src != "k[]a" {
		t.Errorf("res.Src = %q, want wire src %q", res.Src, "k[]a")
	}
}

// TestRelayProcessQClipWriteBack asserts a qclip line whose local line
// number is in QClipPaths writes the clipboard "text" field to the local
// file as a side effect, and still passes the raw bytes through unchanged
// for --jsonl (help-remote.txt HOW IT WORKS step 1/4).
func TestRelayProcessQClipWriteBack(t *testing.T) {
	seq := parseInlined(t, []string{"qclip[]"})
	outPath := filepath.Join(t.TempDir(), "out.txt")
	rl := &Relay{LocalSeq: seq, QClipPaths: map[int]string{1: outPath}}
	wire := `{"line":1,"status":"ok","cmd":"qclip","text":"clipboard contents","t_ms":2}`
	res, rawForJSONL, err := rl.Process([]byte(wire))
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}
	if string(rawForJSONL) != wire {
		t.Errorf("rawForJSONL = %q, want verbatim %q", rawForJSONL, wire)
	}
	if res.Status != "ok" || res.Cmd != "qclip" {
		t.Fatalf("res = %+v", res)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read written qclip file: %v", err)
	}
	if string(got) != "clipboard contents" {
		t.Errorf("qclip file content = %q, want %q", got, "clipboard contents")
	}
}

// TestRelayProcessPassthroughLine asserts an ordinary (non-cap, non-err,
// no qclip write-back) line is returned byte-identical for --jsonl and
// its plain Detail/Extra are rebuilt via engine.ResultDetailFromJSON.
func TestRelayProcessPassthroughLine(t *testing.T) {
	seq := parseInlined(t, []string{"qinfo"})
	rl := &Relay{LocalSeq: seq}
	wire := `{"line":1,"status":"ok","cmd":"qinfo","os":"darwin","osver":"14.0","arch":"arm64","ver":"0.1.0","primary":"cmd","desktop_x":0,"desktop_y":0,"desktop_w":1920,"desktop_h":1080,"displays":1,"session":"bridge","perms":"accessibility:ok,screen:ok","t_ms":4}`
	res, rawForJSONL, err := rl.Process([]byte(wire))
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}
	if string(rawForJSONL) != wire {
		t.Errorf("rawForJSONL = %q, want verbatim %q", rawForJSONL, wire)
	}
	if !res.AlwaysShow {
		t.Error("res.AlwaysShow = false, want true for qinfo")
	}
	if res.Detail == "" {
		t.Error("res.Detail is empty, want a formatted qinfo line")
	}
}

// TestRelayProcessCaptureDecodesInlineFrame asserts a "cap" line's
// --inline-captures base64 payload is decoded and written to a local file
// under OutDir via the same CapturePath/WriteCaptureFile convention a
// genuine local run uses, with rawForJSONL nil (the caller must rebuild
// the JSONL object itself with the local path).
func TestRelayProcessCaptureDecodesInlineFrame(t *testing.T) {
	seq := parseInlined(t, []string{"cap"})
	outDir := t.TempDir()
	rl := &Relay{LocalSeq: seq, OutDir: outDir}

	raw := []byte("not-really-a-png-but-bytes-are-bytes")
	b64 := base64.StdEncoding.EncodeToString(raw)
	wire, err := json.Marshal(map[string]any{
		"line": 1, "status": "ok", "cmd": "cap",
		"data": b64, "fmt": "png", "w": 100, "h": 50,
		"origin": []int{10, 20}, "scale": 1.0, "t_ms": 7,
	})
	if err != nil {
		t.Fatal(err)
	}

	res, rawForJSONL, perr := rl.Process(wire)
	if perr != nil {
		t.Fatalf("Process error: %v", perr)
	}
	if rawForJSONL != nil {
		t.Errorf("rawForJSONL = %q, want nil for a cap line", rawForJSONL)
	}
	if res.Status != "ok" || res.Cmd != "cap" || !res.AlwaysShow {
		t.Fatalf("res = %+v", res)
	}

	// The JSON pairs' first entry must be "path", pointing at a file
	// under outDir that actually contains the decoded bytes.
	if len(res.JSON) == 0 || res.JSON[0].Key != "path" {
		t.Fatalf("res.JSON = %+v, want first key \"path\"", res.JSON)
	}
	path, ok := res.JSON[0].Val.(string)
	if !ok {
		t.Fatalf("res.JSON[0].Val = %v (%T), want string", res.JSON[0].Val, res.JSON[0].Val)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read decoded capture file %s: %v", path, err)
	}
	if string(got) != string(raw) {
		t.Errorf("capture file content = %q, want %q", got, raw)
	}
	if filepath.Dir(path) != outDir {
		t.Errorf("capture file dir = %q, want %q", filepath.Dir(path), outDir)
	}
	if res.Detail == "" {
		t.Error("res.Detail is empty, want a formatted cap line")
	}
}

// TestSynthesizeDone asserts the connection-loss done object carries the
// accumulated totals, held_released=0 and StateUnknown=true (ERROR POLICY
// "Connection loss").
func TestSynthesizeDone(t *testing.T) {
	d := SynthesizeDone(3, 1, 2, time.Now().Add(-50*time.Millisecond))
	if d.OK != 3 || d.Err != 1 || d.Skip != 2 {
		t.Errorf("d = %+v, want OK=3 Err=1 Skip=2", d)
	}
	if d.HeldReleased != 0 {
		t.Errorf("d.HeldReleased = %d, want 0", d.HeldReleased)
	}
	if !d.StateUnknown {
		t.Error("d.StateUnknown = false, want true")
	}
	if d.ElapsedMS < 0 {
		t.Errorf("d.ElapsedMS = %d, want >= 0", d.ElapsedMS)
	}
}
