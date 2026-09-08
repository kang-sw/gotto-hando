package ir_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/syntax"
)

// exampleDoc is the == IR JSON == document from assets/help.txt (:630-660),
// copied verbatim. The golden test parses the same nine source lines and
// asserts the --ir output is structurally identical (field values and
// presence), independent of key order and float formatting (1 vs 1.0).
const exampleDoc = `
{
  "v": 1,
  "ops": [
    {"line": 1, "src": "win[]Blender", "op": "focus",
     "selector": {"kind": "title", "value": "Blender", "regex": false},
     "wait_ms": 0},
    {"line": 2, "src": "k[c]v", "op": "key", "mods": ["ctrl"],
     "keys": [["v"]], "repeat": 1, "gap_ms": 30},
    {"line": 3, "src": "txt[ms=66]hello, world!", "op": "text",
     "text": "hello, world!", "interval_ms": 66},
    {"line": 4, "src": "m[]1024,133", "op": "move",
     "point": {"frame": "desktop", "x": 1024, "y": 133},
     "duration_ms": 0},
    {"line": 5, "src": "c[n=2,w]50%,50%", "op": "click",
     "button": "left", "count": 2, "gap_ms": 60, "mods": [],
     "point": {"frame": "window", "x": 50, "y": 50,
               "x_pct": true, "y_pct": true}},
    {"line": 6, "src": "cap[n=2,ms=100,label=after]", "op": "capture",
     "frame": "desktop", "scale": 1.0, "format": "png",
     "count": 2, "interval_ms": 100, "label": "after"},
    {"line": 7, "src": "exec[]git status", "op": "exec",
     "argv": ["git", "status"], "shell": false, "noerr": false,
     "timeout_ms": 10000},
    {"line": 8, "src": "exec[shell]dir /b", "op": "exec",
     "shell": true, "cmd": "dir /b", "noerr": false,
     "timeout_ms": 10000},
    {"line": 9, "src": "open[]Blender", "op": "open",
     "target": "Blender", "wait_ms": 0}
  ],
  "defaults": {"delay_ms": 100, "text_interval_ms": 0, "key_gap_ms": 30}
}`

var exampleLines = []string{
	"win[]Blender",
	"k[c]v",
	"txt[ms=66]hello, world!",
	"m[]1024,133",
	"c[n=2,w]50%,50%",
	"cap[n=2,ms=100,label=after]",
	"exec[]git status",
	"exec[shell]dir /b",
	"open[]Blender",
}

func TestMarshalMatchesHelpExample(t *testing.T) {
	seq, diags := syntax.Parse(exampleLines, ir.Defaults{DelayMS: 100, TextIntervalMS: 0, KeyGapMS: 30})
	if len(diags) != 0 {
		t.Fatalf("unexpected parse diagnostics: %+v", diags)
	}
	if d := ir.Validate(seq, len(exampleLines)); len(d) != 0 {
		t.Fatalf("unexpected validation diagnostics: %+v", d)
	}
	got, err := ir.Marshal(seq)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var gotV, wantV any
	if err := json.Unmarshal(got, &gotV); err != nil {
		t.Fatalf("unmarshal got: %v\n%s", err, got)
	}
	if err := json.Unmarshal([]byte(exampleDoc), &wantV); err != nil {
		t.Fatalf("unmarshal example: %v", err)
	}
	if !reflect.DeepEqual(gotV, wantV) {
		t.Fatalf("IR JSON mismatch\n got: %s\nwant: %s", got, exampleDoc)
	}
}
