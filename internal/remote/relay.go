package remote

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kang-sw/gotto-hando/internal/engine"
	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// Relay rewrites the remote process's per-line JSONL objects into the
// same shape a genuine local run would print: capture frames decoded
// from --inline-captures base64 into local files under OutDir (the same
// <out>/<NNNN>-<label>-<ts>.png convention CapturePath/WriteCaptureFile
// already implement), qclip[f] write-back performed as a side effect, and
// err "src" restored from the local parse by line number (so an inlined
// [f] line shows as the caller wrote it, e.g. "paste[f]./cmd.py").
type Relay struct {
	// OutDir is the LOCAL capture directory (output.DefaultOutDir),
	// already resolved by the caller before the first result arrives.
	OutDir string
	// LocalSeq is the wrapper's own local parse of the (pre-rewrite)
	// sequence - the source of truth for cap label/explicit-path and for
	// every err line's original src.
	LocalSeq *ir.Sequence
	// QClipPaths maps a qclip[f]-rewritten line number to the local file
	// its clipboard text must be written to.
	QClipPaths map[int]string

	capSeq int
}

// opByLine finds LocalSeq's Op for a given 1-based line number. O(n), fine
// at LIMITS scale (<=1000 ops per run).
func (rl *Relay) opByLine(line int) *ir.Op {
	for i := range rl.LocalSeq.Ops {
		if rl.LocalSeq.Ops[i].Line == line {
			return &rl.LocalSeq.Ops[i]
		}
	}
	return nil
}

// lineHead is the minimal set of fields every per-line JSONL object
// carries (help.txt JSONL :601-608).
type lineHead struct {
	Line   int    `json:"line"`
	Status string `json:"status"`
	Cmd    string `json:"cmd"`
	Src    string `json:"src"`
	Code   string `json:"code"`
	Msg    string `json:"msg"`
	TMS    int64  `json:"t_ms"`
}

// Process handles one decoded per-line wire object: it performs any local
// side effect (capture file write, qclip[f] write-back) and returns the
// printable Result. rawForJSONL is the same bytes as raw when the caller
// may print them verbatim for --jsonl (every command whose fields need no
// local rewrite - the common case, guaranteeing byte-for-byte parity with
// a genuine local run since both sides use output.WriteResult); it is nil
// for cap and err lines, whose JSONL bytes must be rebuilt with local
// path/src values.
func (rl *Relay) Process(raw []byte) (res output.Result, rawForJSONL []byte, err error) {
	var h lineHead
	if err := json.Unmarshal(raw, &h); err != nil {
		return output.Result{}, nil, err
	}

	if h.Status == "err" {
		localSrc := h.Src
		if op := rl.opByLine(h.Line); op != nil {
			localSrc = op.Src
		}
		return output.Result{
			Line: h.Line, Status: "err", Cmd: h.Cmd, Src: localSrc,
			ErrCode: output.ErrorCode(h.Code), ErrMsg: h.Msg,
		}, nil, nil
	}

	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)

	if h.Cmd == "cap" {
		res, err := rl.processCapture(h, fields)
		return res, nil, err
	}

	if path, has := rl.QClipPaths[h.Line]; has {
		var v struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(raw, &v) == nil {
			if err := WriteQClipFile(path, v.Text); err != nil {
				return output.Result{}, nil, err
			}
		}
	}

	res = output.Result{Line: h.Line, Status: h.Status, Cmd: h.Cmd, TMS: h.TMS}
	if detail, extra, alwaysShow, ok := engine.ResultDetailFromJSON(h.Cmd, fields); ok {
		res.Detail, res.Extra, res.AlwaysShow = detail, extra, alwaysShow
	}
	return res, raw, nil
}

// wireCaptureFrame is one --inline-captures frame's wire shape
// (help.txt --inline-captures :72-77, JSONL capture-burst "frames").
type wireCaptureFrame struct {
	Data        string `json:"data"`
	Fmt         string `json:"fmt"`
	ScheduledMS int64  `json:"scheduled_ms"`
	ActualMS    int64  `json:"actual_ms"`
	SlipMS      int64  `json:"slip_ms"`
}

// wireCapture is a cap result's wire shape with --inline-captures.
type wireCapture struct {
	Data   string             `json:"data"`
	Fmt    string             `json:"fmt"`
	W      int                `json:"w"`
	H      int                `json:"h"`
	Origin [2]int             `json:"origin"`
	Scale  float64            `json:"scale"`
	Frames []wireCaptureFrame `json:"frames"`
}

// processCapture decodes one cap line's inline base64 frame(s), writes
// each to a local file via CapturePath/WriteCaptureFile (the same
// convention a genuine local run uses), and rebuilds the Result with
// local paths instead of inline data - both for plain Detail/Extra and
// for the --jsonl object's field shape/order (path,w,h,origin,scale,
// [frames],t_ms - matching internal/engine's own captureJSON order
// exactly since both are hand-written against the same help.txt example).
func (rl *Relay) processCapture(h lineHead, fields map[string]json.RawMessage) (output.Result, error) {
	var w wireCapture
	if err := decodeInto(fields, &w); err != nil {
		return output.Result{}, err
	}

	label, explicit := "cap", ""
	if op := rl.opByLine(h.Line); op != nil {
		if op.Label != "" {
			label = op.Label
		}
		explicit = op.FilePath
	}

	frames := w.Frames
	if len(frames) == 0 {
		frames = []wireCaptureFrame{{Data: w.Data, Fmt: w.Fmt}}
	}
	n := len(frames)
	ts := time.Now().UTC().Format(engine.CaptureTimestampLayout)
	seq := rl.capSeq
	rl.capSeq++

	paths := make([]string, n)
	for i, fr := range frames {
		data, err := base64.StdEncoding.DecodeString(fr.Data)
		if err != nil {
			return output.Result{}, fmt.Errorf("decode inline capture frame %d: %w", i, err)
		}
		path := engine.CapturePath(rl.OutDir, explicit, label, ts, seq, i, n)
		if err := engine.WriteCaptureFile(path, data); err != nil {
			return output.Result{}, err
		}
		paths[i] = path
	}

	detail := captureField(paths[0], w.W, w.H, w.Origin, w.Scale)
	jsonPairs := []output.KV{
		{Key: "path", Val: paths[0]},
		{Key: "w", Val: w.W}, {Key: "h", Val: w.H},
		{Key: "origin", Val: []int{w.Origin[0], w.Origin[1]}},
		{Key: "scale", Val: w.Scale},
	}
	var extra []string
	if n > 1 {
		frameArr := make([]map[string]any, 0, n)
		for i, fr := range frames {
			if i > 0 {
				extra = append(extra, "  "+captureField(paths[i], w.W, w.H, w.Origin, w.Scale))
			}
			frameArr = append(frameArr, map[string]any{
				"path": paths[i], "scheduled_ms": fr.ScheduledMS,
				"actual_ms": fr.ActualMS, "slip_ms": fr.SlipMS,
			})
		}
		jsonPairs = append(jsonPairs, output.KV{Key: "frames", Val: frameArr})
	}

	return output.Result{
		Line: h.Line, Status: "ok", Cmd: "cap", AlwaysShow: true,
		Detail: detail, Extra: extra, JSON: jsonPairs, TMS: h.TMS,
	}, nil
}

// captureField renders one frame's plain OUTPUT fields, matching
// internal/engine's own captureField exactly (help.txt OUTPUT cap
// :587).
func captureField(path string, w, h int, origin [2]int, scale float64) string {
	return fmt.Sprintf("%s %dx%d origin=%d,%d scale=%g", path, w, h, origin[0], origin[1], scale)
}

// decodeInto re-marshals a field map back to JSON and decodes it into
// dst - a convenience mirroring internal/engine.ResultDetailFromJSON's own
// decodeFields helper.
func decodeInto(fields map[string]json.RawMessage, dst any) error {
	b, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

// WriteQClipFile writes one qclip[f]<path> result's clipboard text to a
// local file (help-remote.txt HOW IT WORKS step 1), creating the parent
// directory on first use like WriteCaptureFile does.
func WriteQClipFile(path, text string) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

// SynthesizeDone builds the connection-loss "done" object (ERROR POLICY
// "Connection loss", help.txt:531-535): the totals accumulated from every
// per-line object actually relayed, held_released=0 (the wrapper cannot
// know what the far side released), elapsed since startedAt, state=unknown.
func SynthesizeDone(ok, errN, skip int, startedAt time.Time) output.Done {
	return output.Done{
		OK: ok, Err: errN, Skip: skip, HeldReleased: 0,
		ElapsedMS: time.Since(startedAt).Milliseconds(), StateUnknown: true,
	}
}
