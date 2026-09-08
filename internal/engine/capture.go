package engine

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// captureTimestampLayout is the UTC timestamp in a capture file name
// (help.txt OUTPUT "Capture paths" example
// 0000-cap-20260907T131502.114Z.png, :601-606).
const captureTimestampLayout = "20060102T150405.000Z"

// encodePNG encodes a backend.Image's raw pixels to PNG bytes (ticket
// Decision: the backend returns raw pixels, the GOOS-agnostic engine does
// the PNG encoding via image/png). Pixels are tightly-packed RGBA
// (4 bytes/pixel, stride W*4) - every backend converts to that shape so
// this encoder stays platform-independent.
func encodePNG(img backend.Image) ([]byte, error) {
	need := img.W * img.H * 4
	if img.W <= 0 || img.H <= 0 || len(img.Pixels) < need {
		return nil, fmt.Errorf("capture image is malformed: %dx%d needs %d pixel bytes, got %d",
			img.W, img.H, need, len(img.Pixels))
	}
	m := &image.RGBA{
		Pix:    img.Pixels[:need],
		Stride: img.W * 4,
		Rect:   image.Rect(0, 0, img.W, img.H),
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, m); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// capturePath builds one frame's output path (help.txt OUTPUT "Capture
// paths" :601-606): <out>/<NNNN>-<label>-<UTC timestamp>.png, with a
// -00/-01/... frame suffix when the burst has more than one frame. An
// explicit cap payload path overrides <out>/name (the frame suffix is then
// spliced before its extension); the payload wins per help.txt cap
// "A cap payload overrides the path for that capture" (:594).
func capturePath(outDir, explicit, label, ts string, seq, index, n int) string {
	if explicit != "" {
		if n > 1 {
			return withFrameSuffix(explicit, index)
		}
		return explicit
	}
	name := fmt.Sprintf("%04d-%s-%s", seq, label, ts)
	if n > 1 {
		name += fmt.Sprintf("-%02d", index)
	}
	name += ".png"
	return filepath.Join(outDir, name)
}

// withFrameSuffix splices -NN before path's extension (an explicit-path
// burst).
func withFrameSuffix(path string, index int) string {
	ext := filepath.Ext(path)
	return strings.TrimSuffix(path, ext) + fmt.Sprintf("-%02d", index) + ext
}

// writeCaptureFile writes one PNG to disk, creating the capture directory
// on first use (Phase 1 never created it; the local run path now must).
func writeCaptureFile(path string, data []byte) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0o644)
}

// capturedFrame is one taken frame: its geometry, its written path (empty
// with --inline-captures) and its encoded PNG bytes (kept for inline
// base64).
type capturedFrame struct {
	img  backend.Image
	path string
	data []byte
}

// doCapture runs a cap command: it drives the frame burst on absolute
// deadlines (runFrameSeries), encodes each frame to PNG, and either writes
// it to the capture directory or (with --inline-captures) keeps the bytes
// for base64 in --jsonl. The backend only produces raw pixels; encoding,
// path building and file writing are all here so windows Phase 2 reuses
// them unchanged.
func (st *engineState) doCapture(ctx context.Context, op *ir.Op, res output.Result, fail func(output.ErrorCode, string) output.Result) output.Result {
	req := backend.CaptureReq{
		Frame: op.Frame, Display: op.Disp, Rect: op.Rect,
		Scale: op.Scale, ScaleNative: op.ScaleNative, Format: op.Format,
	}
	n := op.CapCount
	if n < 1 {
		n = 1
	}
	label := op.Label
	if label == "" {
		label = "cap"
	}
	ts := time.Now().UTC().Format(captureTimestampLayout)
	seq := st.capSeq
	st.capSeq++
	explicit := op.FilePath

	frames := make([]capturedFrame, 0, n)
	shoot := func(i int) error {
		img, err := st.be.Capture(ctx, req)
		if err != nil {
			return err
		}
		data, err := encodePNG(img)
		if err != nil {
			return err
		}
		fr := capturedFrame{img: img, data: data}
		if !st.inlineCaptures {
			fr.path = capturePath(st.outDir, explicit, label, ts, seq, i, n)
			if err := writeCaptureFile(fr.path, data); err != nil {
				return err
			}
		}
		frames = append(frames, fr)
		return nil
	}

	timings, err := runFrameSeries(ctx, n, op.CapInterval, time.Now(), realClock{}, shoot)
	if err != nil {
		// cap[w] with no current window is E_NOWINDOW (like c[w]/m[w]/drag[w],
		// help.txt COORDINATES :254); a region that resolved outside its
		// frame is E_BOUNDS (the backend wraps backend.ErrBounds); every
		// other capture failure is E_CAPTURE (help.txt COORDINATES :262-264,
		// ERROR CODES).
		if errors.Is(err, backend.ErrNoWindow) {
			return fail(output.ENoWindow, err.Error())
		}
		if errors.Is(err, backend.ErrBounds) {
			return fail(output.EBounds, err.Error())
		}
		return fail(output.ECapture, err.Error())
	}

	res.AlwaysShow = true
	res.Detail, res.Extra = st.captureDetail(frames)
	res.JSON = st.captureJSON(frames, timings)
	return res
}

// captureOnError takes the single full-desktop capture --cap-on-error asks
// for after a failed line (help.txt --cap-on-error :58-59). It reuses the
// normal capture pipeline with a synthetic desktop cap op labelled
// "error"; a capture failure surfaces as an ordinary err result rather than
// disturbing the already-failed line.
func (st *engineState) captureOnError(ctx context.Context, line int) output.Result {
	op := &ir.Op{
		Kind: ir.KindCapture, Line: line, Frame: "desktop",
		Scale: 1, Format: "png", Label: "error", CapCount: 1, CapInterval: 100,
	}
	res := output.Result{Line: line, Cmd: kindCmd[ir.KindCapture], Status: "ok"}
	fail := func(code output.ErrorCode, msg string) output.Result {
		res.Status = "err"
		res.ErrCode = code
		res.ErrMsg = msg
		return res
	}
	return st.doCapture(ctx, op, res, fail)
}

// captureField renders one frame's plain OUTPUT fields (help.txt OUTPUT
// cap :587: "<path> <w>x<h> origin=<x>,<y> scale=<f>"). With
// --inline-captures no file exists, so the path field reads "(inline)".
func (st *engineState) captureField(fr capturedFrame) string {
	pathField := fr.path
	if st.inlineCaptures {
		pathField = "(inline)"
	}
	return fmt.Sprintf("%s %dx%d origin=%d,%d scale=%g",
		pathField, fr.img.W, fr.img.H, fr.img.OriginX, fr.img.OriginY, fr.img.Scale)
}

// captureDetail builds cap's plain Detail (first frame) and Extra
// (remaining frames, two-space indented per the OUTPUT multi-line
// convention).
func (st *engineState) captureDetail(frames []capturedFrame) (string, []string) {
	if len(frames) == 0 {
		return "", nil
	}
	detail := st.captureField(frames[0])
	var extra []string
	for _, fr := range frames[1:] {
		extra = append(extra, "  "+st.captureField(fr))
	}
	return detail, extra
}

// captureJSON builds cap's --jsonl command-specific fields. A single frame
// matches help.txt's documented example (:604-605:
// path/w/h/origin/scale); --inline-captures swaps path for data+fmt
// (help.txt --inline-captures :72-77). A burst adds a "frames" array whose
// entries carry each frame's path-or-data and its scheduled/actual/slip
// timing (help.txt JSONL :609-610).
func (st *engineState) captureJSON(frames []capturedFrame, timings []frameResult) []output.KV {
	if len(frames) == 0 {
		return nil
	}
	first := frames[0]
	var pairs []output.KV
	if st.inlineCaptures {
		pairs = append(pairs,
			output.KV{Key: "data", Val: base64.StdEncoding.EncodeToString(first.data)},
			output.KV{Key: "fmt", Val: "png"})
	} else {
		pairs = append(pairs, output.KV{Key: "path", Val: first.path})
	}
	pairs = append(pairs,
		output.KV{Key: "w", Val: first.img.W},
		output.KV{Key: "h", Val: first.img.H},
		output.KV{Key: "origin", Val: []int{first.img.OriginX, first.img.OriginY}},
		output.KV{Key: "scale", Val: first.img.Scale})

	if len(frames) > 1 {
		arr := make([]map[string]any, 0, len(frames))
		for i, fr := range frames {
			entry := map[string]any{
				"scheduled_ms": timings[i].scheduledMS,
				"actual_ms":    timings[i].actualMS,
				"slip_ms":      timings[i].slipMS,
			}
			if st.inlineCaptures {
				entry["data"] = base64.StdEncoding.EncodeToString(fr.data)
				entry["fmt"] = "png"
			} else {
				entry["path"] = fr.path
			}
			arr = append(arr, entry)
		}
		pairs = append(pairs, output.KV{Key: "frames", Val: arr})
	}
	return pairs
}
