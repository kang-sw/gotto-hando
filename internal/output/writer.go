package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// KV is one ordered JSON field. Per-line JSONL objects carry
// command-specific fields in the order the spec examples show them
// (help.txt:603-608), which a plain map[string]any (whose keys
// encoding/json sorts alphabetically) cannot reproduce.
type KV struct {
	Key string
	Val any
}

// Result is one input line's already-computed outcome; a future engine
// (Phase 2) builds these, this package only formats them (OUTPUT,
// help.txt:543-594; JSONL, help.txt:595-610).
type Result struct {
	Line int
	// Status is "ok", "err", "skip" or "warn".
	Status string
	// Cmd is the bare command name (win, k, cap, ...).
	Cmd string

	// err only: OUTPUT's "<n> err <source>: <message> (<E_CODE>)"
	// (help.txt:567) and JSONL's separate "src"/"code"/"msg" fields
	// (help.txt:607-608). Src falls back to Cmd in plain mode when empty.
	Src     string
	ErrMsg  string
	ErrCode ErrorCode

	// Detail is plain-only: pre-formatted trailing text for a non-err
	// line (e.g. `id=2314 app=Blender matched=1 0,25 1440x875
	// "Untitled"`).
	Detail string
	// Extra is plain-only: already-formatted, already-indented
	// continuation lines for multi-line results (qwin/exec/...,
	// help.txt:576-587).
	Extra []string

	// AlwaysShow keeps an "ok" line visible under -q: query results and
	// capture lines are never omitted, only other ok lines are
	// (help.txt:65-66).
	AlwaysShow bool

	// JSON holds jsonl-only command-specific fields, in spec order.
	// Ignored (along with TMS) when Status is "err" - err objects never
	// carry t_ms in the JSONL examples (help.txt:607-608).
	JSON []KV
	TMS  int64
}

// Done is the run summary (OUTPUT "done" line, help.txt:568-569; JSONL
// "done" event, help.txt:598-600).
type Done struct {
	OK           int
	Err          int
	Skip         int
	ElapsedMS    int64
	HeldReleased int
	// StateUnknown appends " state=unknown" (plain) / "state":"unknown"
	// (jsonl); it is only ever set together with exit 5 (help.txt:569).
	StateUnknown bool
}

// WriteResult formats one line's Result in plain or JSONL form.
func WriteResult(w io.Writer, jsonl bool, quiet bool, r Result) error {
	if jsonl {
		return writeResultJSON(w, r)
	}
	return writeResultPlain(w, quiet, r)
}

func writeResultPlain(w io.Writer, quiet bool, r Result) error {
	if quiet && r.Status == "ok" && !r.AlwaysShow {
		return nil
	}
	var err error
	if r.Status == "err" {
		src := r.Src
		if src == "" {
			src = r.Cmd
		}
		_, err = fmt.Fprintf(w, "%d err %s: %s (%s)\n", r.Line, src, r.ErrMsg, r.ErrCode)
	} else {
		line := fmt.Sprintf("%d %s %s", r.Line, r.Status, r.Cmd)
		if r.Detail != "" {
			line += " " + r.Detail
		}
		_, err = fmt.Fprintln(w, line)
	}
	if err != nil {
		return err
	}
	for _, e := range r.Extra {
		if _, err = fmt.Fprintln(w, e); err != nil {
			return err
		}
	}
	return nil
}

func writeResultJSON(w io.Writer, r Result) error {
	pairs := []KV{{"line", r.Line}, {"status", r.Status}, {"cmd", r.Cmd}}
	if r.Status == "err" {
		pairs = append(pairs, KV{"src", r.Src}, KV{"code", string(r.ErrCode)}, KV{"msg", r.ErrMsg})
	} else {
		pairs = append(pairs, r.JSON...)
		pairs = append(pairs, KV{"t_ms", r.TMS})
	}
	return writeJSONObject(w, pairs)
}

// WriteDone formats the run summary in plain or JSONL form.
func WriteDone(w io.Writer, jsonl bool, d Done) error {
	if jsonl {
		pairs := []KV{
			{"event", "done"}, {"ok", d.OK}, {"err", d.Err}, {"skip", d.Skip},
			{"elapsed_ms", d.ElapsedMS}, {"held_released", d.HeldReleased},
		}
		if d.StateUnknown {
			pairs = append(pairs, KV{"state", "unknown"})
		}
		return writeJSONObject(w, pairs)
	}
	line := fmt.Sprintf("done ok=%d err=%d skip=%d elapsed=%dms held_released=%d",
		d.OK, d.Err, d.Skip, d.ElapsedMS, d.HeldReleased)
	if d.StateUnknown {
		line += " state=unknown"
	}
	_, err := fmt.Fprintln(w, line)
	return err
}

// targetInfo is the JSONL "start" event's nested "target" object
// (help.txt:596-597); Phase 1 never connects to a real target, so OS is
// always this process's own runtime.GOOS.
type targetInfo struct {
	OS string `json:"os"`
}

// WriteStart writes the OUTPUT "First line" (plain, help.txt:562) or the
// JSONL "start" event (help.txt:596-597). Exported so cmd/gotto-hando's
// local-run path (the first non-abort caller) can print it; WriteAbort
// still calls it internally for the abort case.
func WriteStart(w io.Writer, jsonl bool, dest, outDir string) error {
	if !jsonl {
		_, err := fmt.Fprintf(w, "out %s\n", outDir)
		return err
	}
	return writeJSONObject(w, []KV{
		{"event", "start"}, {"out", outDir}, {"dest", dest},
		{"target", targetInfo{OS: runtime.GOOS}},
	})
}

// WriteAbort implements the OUTPUT "Abort" paragraph and the JSONL
// "abort" event (help.txt:543-575, :611-614). Plain mode writes exactly
// one line to w (the caller passes stderr) and nothing else - not even
// the normally-first "out" line. JSONL mode writes the "start" object
// then the "abort" object to w (the caller passes stdout); there is no
// "done" object either way.
func WriteAbort(w io.Writer, jsonl bool, dest string, outDir string, code ErrorCode, msg string) error {
	if !jsonl {
		return WriteAbortEvent(w, false, code, msg)
	}
	if err := WriteStart(w, true, dest, outDir); err != nil {
		return err
	}
	return WriteAbortEvent(w, true, code, msg)
}

// WriteAbortEvent writes just the abort object/line (plain: "abort: <msg>
// (<code>)"; jsonl: the {"event":"abort",...} object) - the half of
// WriteAbort that does NOT also write "start". Exported for callers that
// print their own "start" first (the bridge's local forwarder,
// 260908-feat-remote-ssh Phase 0: it prints its own start object using
// local-only dest/out values before relaying a bridge-side abort, so
// calling WriteAbort a second time would double-print start). WriteAbort
// itself is WriteStart + WriteAbortEvent in jsonl mode, WriteAbortEvent
// alone in plain mode (plain abort never prints "start" at all).
func WriteAbortEvent(w io.Writer, jsonl bool, code ErrorCode, msg string) error {
	if !jsonl {
		_, err := fmt.Fprintf(w, "abort: %s (%s)\n", msg, code)
		return err
	}
	return writeJSONObject(w, []KV{
		{"event", "abort"}, {"code", string(code)}, {"msg", msg},
	})
}

// DefaultOutDir computes the capture directory string (OUTPUT "Capture
// paths", help.txt:588-590): --out, else $GOTTO_HANDO_OUT, else
// $TMPDIR/gotto-hando/<dest>/<run-id>/. This is pure string construction;
// no filesystem access happens here, and Phase 1 never creates the
// directory.
func DefaultOutDir(explicit, dest, runID string) string {
	if explicit != "" {
		return explicit
	}
	if v := os.Getenv("GOTTO_HANDO_OUT"); v != "" {
		return v
	}
	tmp := os.Getenv("TMPDIR")
	if tmp == "" {
		tmp = os.TempDir()
	}
	dir := filepath.Join(tmp, "gotto-hando", dest, runID)
	if !strings.HasSuffix(dir, string(filepath.Separator)) {
		dir += string(filepath.Separator)
	}
	return dir
}

func writeJSONObject(w io.Writer, pairs []KV) error {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, err := json.Marshal(p.Key)
		if err != nil {
			return err
		}
		b.Write(kb)
		b.WriteByte(':')
		vb, err := json.Marshal(p.Val)
		if err != nil {
			return err
		}
		b.Write(vb)
	}
	b.WriteByte('}')
	b.WriteByte('\n')
	_, err := w.Write(b.Bytes())
	return err
}
