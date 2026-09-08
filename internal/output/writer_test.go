package output

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func loadGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	// %OS% is a portable placeholder for the JSONL "target":{"os":...}
	// field, which is always this process's own runtime.GOOS (Phase 1
	// never connects to a real target).
	return strings.ReplaceAll(string(b), "%OS%", runtime.GOOS)
}

// TestPlainQuickStartTranscript reproduces the QUICK START transcript
// (help.txt:117-133) through WriteResult/WriteDone.
func TestPlainQuickStartTranscript(t *testing.T) {
	var buf bytes.Buffer
	mustWrite(t, WriteStart(&buf, false, "local", "/tmp/gh/"))
	results := []Result{
		{Line: 1, Status: "ok", Cmd: "win", Detail: `id=771 app=Safari matched=1 0,25 1440x875 "Start Page"`},
		{Line: 2, Status: "ok", Cmd: "k"},
		{Line: 3, Status: "ok", Cmd: "txt", Detail: "chars=12"},
		{Line: 4, Status: "ok", Cmd: "sleep"},
		{Line: 5, Status: "ok", Cmd: "cap", Detail: "/tmp/gh/0000-cap-20260907T131501.502Z.png 1440x900 origin=0,0 scale=1"},
	}
	for _, r := range results {
		mustWrite(t, WriteResult(&buf, false, false, r))
	}
	mustWrite(t, WriteDone(&buf, false, Done{OK: 5, Err: 0, Skip: 0, ElapsedMS: 1610, HeldReleased: 0}))

	want := loadGolden(t, "plain_quickstart.golden")
	if got := buf.String(); got != want {
		t.Fatalf("mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestPlainOutputSectionTranscript reproduces the OUTPUT section
// transcript (help.txt:548-560): multi-line qwin/exec results, an err
// line and a skip line.
func TestPlainOutputSectionTranscript(t *testing.T) {
	var buf bytes.Buffer
	mustWrite(t, WriteStart(&buf, false, "local", "/tmp/gh/"))
	results := []Result{
		{Line: 1, Status: "ok", Cmd: "win", Detail: `id=2314 app=Blender matched=1 0,25 1440x875 "Untitled - Blender"`},
		{Line: 2, Status: "ok", Cmd: "k"},
		{Line: 3, Status: "ok", Cmd: "txt", Detail: "chars=5"},
		{Line: 4, Status: "ok", Cmd: "cap", Detail: "/tmp/gh/0000-cap-20260907T131502.114Z.png 1440x900 origin=0,0 scale=1"},
		{Line: 5, Status: "ok", Cmd: "qwin", AlwaysShow: true, Extra: []string{
			"  2314\t4120\tBlender\t0,25 1440x875\t*\tUntitled - Blender",
			"  2290\t612\tTerminal\t1440,25 1120x875\t\tzsh - 120x40",
		}},
		{Line: 6, Status: "ok", Cmd: "exec", Detail: "exit=0 ms=41 stdout=34B stderr=0B", Extra: []string{
			"  1\tC:\\Users\\me\\scenes\\untitled.blend",
		}},
		{Line: 7, Status: "err", Cmd: "c", Src: "c[w]1500,300", ErrMsg: "coordinates outside current window", ErrCode: EBounds},
		{Line: 8, Status: "skip", Cmd: "cap"},
	}
	for _, r := range results {
		mustWrite(t, WriteResult(&buf, false, false, r))
	}
	mustWrite(t, WriteDone(&buf, false, Done{OK: 6, Err: 1, Skip: 1, ElapsedMS: 1875, HeldReleased: 0}))

	want := loadGolden(t, "plain_output_section.golden")
	if got := buf.String(); got != want {
		t.Fatalf("mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestPlainQuietOmitsOkExceptQueriesAndCaptures asserts -q omits plain
// "ok" lines but keeps err/warn lines and AlwaysShow (query/capture)
// lines visible (help.txt:65-66).
func TestPlainQuietOmitsOkExceptQueriesAndCaptures(t *testing.T) {
	var buf bytes.Buffer
	results := []Result{
		{Line: 1, Status: "ok", Cmd: "k"},                                           // omitted
		{Line: 2, Status: "ok", Cmd: "qwin", AlwaysShow: true, Detail: "matched=0"}, // kept (query)
		{Line: 3, Status: "warn", Cmd: "win", Detail: "matched=2"},                  // kept
		{Line: 4, Status: "err", Cmd: "c", ErrMsg: "boom", ErrCode: EBounds},        // kept
		{Line: 5, Status: "skip", Cmd: "cap"},                                       // kept
	}
	for _, r := range results {
		mustWrite(t, WriteResult(&buf, false, true, r))
	}
	got := buf.String()
	if strings.Contains(got, "1 ok k") {
		t.Fatalf("quiet mode did not omit a plain ok line:\n%s", got)
	}
	for _, want := range []string{"2 ok qwin matched=0", "3 warn win matched=2", "4 err c: boom (E_BOUNDS)", "5 skip cap"} {
		if !strings.Contains(got, want) {
			t.Fatalf("quiet mode output missing %q:\n%s", want, got)
		}
	}
}

// TestJSONLTranscript exercises the JSONL start/result/done sequence
// (help.txt:595-610) including the field-order distinction between ok
// lines (command-specific fields then t_ms) and err lines (src/code/msg,
// no t_ms).
func TestJSONLTranscript(t *testing.T) {
	var buf bytes.Buffer
	mustWrite(t, WriteStart(&buf, true, "local", "/tmp/gh/"))
	results := []Result{
		{
			Line: 4, Status: "ok", Cmd: "cap", TMS: 312,
			JSON: []KV{
				{"path", "/tmp/gh/0000-cap.png"}, {"w", 1440}, {"h", 875},
				{"origin", []int{0, 25}}, {"scale", 1},
			},
		},
		{
			Line: 6, Status: "ok", Cmd: "exec", TMS: 41,
			JSON: []KV{
				{"exit", 0}, {"stdout", "...\n"}, {"stderr", ""}, {"truncated", false},
			},
		},
		{
			Line: 7, Status: "err", Cmd: "c", Src: "c[w]1500,300",
			ErrCode: EBounds, ErrMsg: "coordinates outside current window",
		},
	}
	for _, r := range results {
		mustWrite(t, WriteResult(&buf, true, false, r))
	}
	mustWrite(t, WriteDone(&buf, true, Done{OK: 2, Err: 1, Skip: 0, ElapsedMS: 500, HeldReleased: 0}))

	want := loadGolden(t, "jsonl_basic.golden")
	if got := buf.String(); got != want {
		t.Fatalf("mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestWriteAbortPlain asserts the abort asymmetry: plain mode writes
// nothing to stdout at all (not even the normally-first "out" line) and
// exactly one line to the writer passed as stderr (help.txt:543-575).
func TestWriteAbortPlain(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := WriteAbort(&stderr, false, "local", "/tmp/gh/", EValidate, "platform backend not implemented, nothing ran"); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout should stay empty on a plain abort, got %q", stdout.String())
	}
	want := loadGolden(t, "plain_abort.golden")
	if got := stderr.String(); got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

// TestWriteAbortJSONL asserts JSONL mode writes "start" then "abort" (no
// "done") to the writer passed as stdout (help.txt:611-614).
func TestWriteAbortJSONL(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteAbort(&buf, true, "local", "/tmp/gh/", EValidate, "platform backend not implemented, nothing ran"); err != nil {
		t.Fatal(err)
	}
	want := loadGolden(t, "jsonl_abort.golden")
	if got := buf.String(); got != want {
		t.Fatalf("mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestAbortExit asserts the ticket's abort-exit mapping: E_VALIDATE -> 2,
// E_CONNECT -> 3, every other code -> 4.
func TestAbortExit(t *testing.T) {
	cases := []struct {
		code ErrorCode
		want int
	}{
		{EValidate, 2},
		{EConnect, 3},
		{EPermission, 4},
		{ESession, 4},
		{EBounds, 4},
		{ENoWindow, 4},
		{EInput, 4},
		{ECapture, 4},
		{EClipboard, 4},
		{EExec, 4},
		{ETimeout, 4},
		{EUnknown, 4},
		{ESyntax, 4},
	}
	for _, c := range cases {
		if got := AbortExit(c.code); got != c.want {
			t.Errorf("AbortExit(%s) = %d, want %d", c.code, got, c.want)
		}
	}
}

// TestDefaultOutDir covers the --out / $GOTTO_HANDO_OUT / computed-default
// precedence (help.txt:588-590).
func TestDefaultOutDir(t *testing.T) {
	if got := DefaultOutDir("/explicit/", "local", "run1"); got != "/explicit/" {
		t.Fatalf("explicit --out not honored: %q", got)
	}

	t.Setenv("GOTTO_HANDO_OUT", "/from/env/")
	if got := DefaultOutDir("", "local", "run1"); got != "/from/env/" {
		t.Fatalf("$GOTTO_HANDO_OUT not honored: %q", got)
	}

	t.Setenv("GOTTO_HANDO_OUT", "")
	t.Setenv("TMPDIR", "/tmp")
	want := filepath.Join("/tmp", "gotto-hando", "local", "run1") + string(filepath.Separator)
	if got := DefaultOutDir("", "local", "run1"); got != want {
		t.Fatalf("computed default = %q, want %q", got, want)
	}
}

func mustWrite(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
