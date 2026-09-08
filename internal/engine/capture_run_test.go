package engine_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/backend/dryrun"
	"github.com/kang-sw/gotto-hando/internal/engine"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// synthCapture is a 2x2 opaque RGBA fixture the dryrun backend hands back
// from Capture, so doCapture's encode/write/JSON path runs without any FFI.
func synthCapture() backend.Image {
	pix := make([]byte, 2*2*4)
	for i := range pix {
		pix[i] = byte(i * 7)
	}
	for i := 0; i < 4; i++ {
		pix[i*4+3] = 0xff // opaque alpha per pixel
	}
	return backend.Image{W: 2, H: 2, OriginX: 5, OriginY: 6, Scale: 1, Pixels: pix}
}

func kvVal(t *testing.T, kvs []output.KV, key string) (any, bool) {
	t.Helper()
	for _, kv := range kvs {
		if kv.Key == key {
			return kv.Val, true
		}
	}
	return nil, false
}

// TestCaptureWritesFileAndReportsPath drives `cap` through engine.Run with a
// dryrun backend and a temp OutDir, asserting doCapture wrote a decodable
// PNG at the JSONL `path` and reported the frame geometry (test finding:
// doCapture orchestration was untested).
func TestCaptureWritesFileAndReportsPath(t *testing.T) {
	seq := parse(t, "cap")
	be := &dryrun.Backend{CaptureResult: synthCapture()}
	out := t.TempDir()
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{OutDir: out})

	if len(sum.Results) != 1 || sum.Results[0].Status != "ok" {
		t.Fatalf("Results = %+v, want a single ok result", sum.Results)
	}
	r := sum.Results[0]

	pv, ok := kvVal(t, r.JSON, "path")
	if !ok {
		t.Fatalf("JSON = %+v, want a path field (non-inline capture)", r.JSON)
	}
	path := pv.(string)
	if !strings.HasPrefix(path, out) {
		t.Fatalf("path %q is not under OutDir %q", path, out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("capture file not written: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("written file is not valid PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 2 || b.Dy() != 2 {
		t.Fatalf("PNG size = %dx%d, want 2x2", b.Dx(), b.Dy())
	}
	// w/h/origin/scale reflect the backend image.
	if wv, _ := kvVal(t, r.JSON, "w"); wv != 2 {
		t.Errorf("w = %v, want 2", wv)
	}
	if ov, _ := kvVal(t, r.JSON, "origin"); fmt.Sprint(ov) != "[5 6]" {
		t.Errorf("origin = %v, want [5 6]", ov)
	}
	// Exactly one PNG on disk.
	entries, _ := os.ReadDir(out)
	if len(entries) != 1 {
		t.Fatalf("OutDir has %d files, want 1", len(entries))
	}
}

// TestCaptureInlineEmitsBase64 asserts --inline-captures writes NO file and
// instead emits `data` (base64 PNG) + `fmt` in the JSONL.
func TestCaptureInlineEmitsBase64(t *testing.T) {
	seq := parse(t, "cap")
	be := &dryrun.Backend{CaptureResult: synthCapture()}
	out := t.TempDir()
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{OutDir: out, InlineCaptures: true})

	r := sum.Results[0]
	if r.Status != "ok" {
		t.Fatalf("status = %q, want ok", r.Status)
	}
	if _, ok := kvVal(t, r.JSON, "path"); ok {
		t.Errorf("JSON has a path field, want none with --inline-captures")
	}
	if fv, _ := kvVal(t, r.JSON, "fmt"); fv != "png" {
		t.Errorf("fmt = %v, want png", fv)
	}
	dv, ok := kvVal(t, r.JSON, "data")
	if !ok {
		t.Fatalf("JSON = %+v, want a data field", r.JSON)
	}
	raw, err := base64.StdEncoding.DecodeString(dv.(string))
	if err != nil {
		t.Fatalf("data is not valid base64: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(raw)); err != nil {
		t.Fatalf("inline data is not a valid PNG: %v", err)
	}
	if entries, _ := os.ReadDir(out); len(entries) != 0 {
		t.Fatalf("OutDir has %d files, want 0 with --inline-captures", len(entries))
	}
}

// TestCaptureBurstEmitsFrames asserts a cap[n=2] burst writes two files and
// emits a frames[] array of length 2 with the per-frame timing keys.
func TestCaptureBurstEmitsFrames(t *testing.T) {
	seq := parse(t, "cap[n=2,ms=5]")
	be := &dryrun.Backend{CaptureResult: synthCapture()}
	out := t.TempDir()
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{OutDir: out})

	r := sum.Results[0]
	if r.Status != "ok" {
		t.Fatalf("status = %q, want ok", r.Status)
	}
	fv, ok := kvVal(t, r.JSON, "frames")
	if !ok {
		t.Fatalf("JSON = %+v, want a frames array for n>1", r.JSON)
	}
	frames := fv.([]map[string]any)
	if len(frames) != 2 {
		t.Fatalf("frames len = %d, want 2", len(frames))
	}
	for i, f := range frames {
		for _, k := range []string{"scheduled_ms", "actual_ms", "slip_ms", "path"} {
			if _, has := f[k]; !has {
				t.Errorf("frame %d missing key %q (has %+v)", i, k, f)
			}
		}
	}
	if entries, _ := os.ReadDir(out); len(entries) != 2 {
		t.Fatalf("OutDir has %d files, want 2", len(entries))
	}
}

// TestCaptureErrorMapping asserts doCapture maps a backend Capture error to
// the right code: ErrNoWindow -> E_NOWINDOW (cap[w] no window, consistent
// with c[w]/m[w]), ErrBounds -> E_BOUNDS, any other error -> E_CAPTURE.
func TestCaptureErrorMapping(t *testing.T) {
	cases := []struct {
		name string
		line string
		err  error
		want output.ErrorCode
	}{
		{"no window", "cap[w]", fmt.Errorf("%w: none focused", backend.ErrNoWindow), output.ENoWindow},
		{"out of bounds", "cap", fmt.Errorf("%w: rect off frame", backend.ErrBounds), output.EBounds},
		{"generic failure", "cap", errors.New("screenshot failed"), output.ECapture},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			seq := parse(t, c.line)
			injected := c.err
			be := &dryrun.Backend{CaptureResult: synthCapture(), FailOn: func(call string) error {
				if strings.HasPrefix(call, "Capture") {
					return injected
				}
				return nil
			}}
			sum := engine.Run(context.Background(), be, seq, engine.RunOptions{OutDir: t.TempDir()})
			r := sum.Results[0]
			if r.Status != "err" || r.ErrCode != c.want {
				t.Fatalf("result = status=%q code=%q, want err/%s", r.Status, r.ErrCode, c.want)
			}
		})
	}
}
