package engine

import (
	"encoding/json"

	"github.com/kang-sw/gotto-hando/internal/backend"
)

// ResultDetailFromJSON rebuilds a plain-mode Result's Detail/Extra/
// AlwaysShow from a per-line JSONL object's fields - used by
// cmd/gotto-hando's bridge forwarder (dispatch_windows.go's
// forwardToBridge, 260908-feat-remote-ssh Phase 0) to reconstruct plain
// output from the JSONL-only bridge wire, reusing the same formatting
// functions (formatQueryInfo/formatQueryWindows/formatQueryDisp/
// formatQueryMouse/formatFocusDetail/formatExecDetail/
// formatExecOutputLines) engine.Run's own execute() already uses so both
// paths render identically.
//
// fields is the whole decoded per-line JSON object (line/status/cmd/t_ms
// included; unused keys are ignored by the per-cmd decode below). Only
// the commands whose plain Detail/Extra depend on engine-computed fields
// need an entry here - qinfo, qclip, win (focus), qwin, qdisp, qmouse,
// exec. Every other command kind has no Detail in plain mode today either
// (execute()'s switch never sets one for them), so the caller's generic
// no-Detail fallback is already correct for them; cap is intentionally
// excluded (ticket Out of Scope: no inline-PNG decode / local file
// writing through the bridge this slice).
//
// ok is false when cmd is not one of the above, or a required field is
// missing/malformed - the caller should fall back to its generic case.
func ResultDetailFromJSON(cmd string, fields map[string]json.RawMessage) (detail string, extra []string, alwaysShow bool, ok bool) {
	switch cmd {
	case "qinfo":
		var v struct {
			OS       string `json:"os"`
			OSVer    string `json:"osver"`
			Arch     string `json:"arch"`
			Ver      string `json:"ver"`
			Primary  string `json:"primary"`
			DesktopX int    `json:"desktop_x"`
			DesktopY int    `json:"desktop_y"`
			DesktopW int    `json:"desktop_w"`
			DesktopH int    `json:"desktop_h"`
			Displays int    `json:"displays"`
			Session  string `json:"session"`
			Perms    string `json:"perms"`
		}
		if !decodeFields(fields, &v) {
			return "", nil, false, false
		}
		info := backend.Info{
			OS: v.OS, OSVer: v.OSVer, Arch: v.Arch, Ver: v.Ver, Primary: v.Primary,
			DesktopX: v.DesktopX, DesktopY: v.DesktopY, DesktopW: v.DesktopW, DesktopH: v.DesktopH,
			Displays: v.Displays, Session: v.Session, Perms: v.Perms,
		}
		return formatQueryInfo(info), nil, true, true

	case "qclip":
		var v struct {
			Text string `json:"text"`
		}
		if !decodeFields(fields, &v) {
			return "", nil, false, false
		}
		return v.Text, nil, true, true

	case "win":
		var v struct {
			ID      int    `json:"id"`
			App     string `json:"app"`
			Title   string `json:"title"`
			Matched int    `json:"matched"`
			X, Y    int
			W, H    int
		}
		if !decodeFields(fields, &v) {
			return "", nil, false, false
		}
		w := backend.Window{ID: v.ID, App: v.App, Title: v.Title, X: v.X, Y: v.Y, W: v.W, H: v.H}
		return formatFocusDetail(w, v.Matched), nil, false, true

	case "qwin":
		var v struct {
			Windows []backend.Window `json:"windows"`
		}
		if !decodeFields(fields, &v) {
			return "", nil, false, false
		}
		return "", formatQueryWindows(v.Windows), true, true

	case "qdisp":
		var v struct {
			Displays []backend.DisplayGeom `json:"displays"`
		}
		if !decodeFields(fields, &v) {
			return "", nil, false, false
		}
		return "", formatQueryDisp(backend.Info{DisplayList: v.Displays}), true, true

	case "qmouse":
		var v struct{ X, Y float64 }
		if !decodeFields(fields, &v) {
			return "", nil, false, false
		}
		return "", formatQueryMouse(backend.Point{X: v.X, Y: v.Y}), true, true

	case "exec":
		var v struct {
			Exit      int    `json:"exit"`
			Stdout    string `json:"stdout"`
			Stderr    string `json:"stderr"`
			Truncated bool   `json:"truncated"`
			TMS       int64  `json:"t_ms"`
		}
		if !decodeFields(fields, &v) {
			return "", nil, false, false
		}
		r := backend.ExecResult{Exit: v.Exit, Stdout: v.Stdout, Stderr: v.Stderr, Truncated: v.Truncated}
		// exec always sets AlwaysShow on its non-err path (doExec, run.go);
		// this reconstruction path only ever sees non-err results (the
		// forwarder handles status=="err" via straight src/code/msg
		// passthrough instead), so alwaysShow is unconditionally true here.
		return formatExecDetail(r, v.TMS), formatExecOutputLines(r), true, true

	default:
		return "", nil, false, false
	}
}

// decodeFields re-marshals the field map back to JSON bytes and decodes
// it into dst - a convenience so each case above can use a plain tagged
// struct instead of pulling individual keys out of the map by hand.
func decodeFields(fields map[string]json.RawMessage, dst any) bool {
	b, err := json.Marshal(fields)
	if err != nil {
		return false
	}
	return json.Unmarshal(b, dst) == nil
}
