package engine

import (
	"fmt"
	"strings"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// formatQueryInfo builds qinfo's plain Detail line (help.txt:431-434,
// help-macos.txt CHECK example :12-14):
//
//	os=darwin osver=14.5 arch=arm64 ver=1.2.3 primary=cmd desktop=0,0
//	2560x1440 displays=2 session=active perms=accessibility:ok,screen:ok
func formatQueryInfo(info backend.Info) string {
	return fmt.Sprintf("os=%s osver=%s arch=%s ver=%s primary=%s desktop=%d,%d %dx%d displays=%d session=%s perms=%s",
		info.OS, info.OSVer, info.Arch, info.Ver, info.Primary,
		info.DesktopX, info.DesktopY, info.DesktopW, info.DesktopH,
		info.Displays, info.Session, info.Perms)
}

// queryInfoJSON is qinfo's --jsonl command-specific fields: flat KV pairs
// mirroring the plain fields (help.txt JSONL, :601-610, does not show a
// verbatim example for qinfo - this shape is a spec-consistent inference).
func queryInfoJSON(info backend.Info) []output.KV {
	return []output.KV{
		{Key: "os", Val: info.OS}, {Key: "osver", Val: info.OSVer},
		{Key: "arch", Val: info.Arch}, {Key: "ver", Val: info.Ver},
		{Key: "primary", Val: info.Primary},
		{Key: "desktop_x", Val: info.DesktopX}, {Key: "desktop_y", Val: info.DesktopY},
		{Key: "desktop_w", Val: info.DesktopW}, {Key: "desktop_h", Val: info.DesktopH},
		{Key: "displays", Val: info.Displays},
		{Key: "session", Val: info.Session}, {Key: "perms", Val: info.Perms},
	}
}

// formatQueryDisp builds qdisp's plain Extra lines (help.txt:435-436,
// :582): one two-space-indented, TAB-separated line per display -
// "<idx>\t<x>,<y> <w>x<h>\tscale=<f>\t[primary]" - even for a single
// display (OUTPUT "Multi-line results" convention, same mechanism as
// qwin's example).
func formatQueryDisp(info backend.Info) []string {
	lines := make([]string, 0, len(info.DisplayList))
	for i, d := range info.DisplayList {
		primary := ""
		if d.Primary {
			primary = "primary"
		}
		lines = append(lines, fmt.Sprintf("  %d\t%d,%d %dx%d\tscale=%g\t%s",
			i, d.X, d.Y, d.W, d.H, d.Scale, primary))
	}
	return lines
}

// queryDispJSON is qdisp's --jsonl command-specific field: a "displays"
// array (spec-consistent inference, same caveat as queryInfoJSON).
func queryDispJSON(info backend.Info) []output.KV {
	arr := make([]map[string]any, 0, len(info.DisplayList))
	for i, d := range info.DisplayList {
		arr = append(arr, map[string]any{
			"idx": i, "x": d.X, "y": d.Y, "w": d.W, "h": d.H,
			"scale": d.Scale, "primary": d.Primary,
		})
	}
	return []output.KV{{Key: "displays", Val: arr}}
}

// windowFlags builds a qwin/win flags field (help.txt OUTPUT :578-579:
// "* focused, min minimized, hidden"). The three are independent; focused
// is "*", minimized "min", hidden "hidden", joined by spaces, empty when
// none apply (the plain example's Terminal row has an empty flags field).
func windowFlags(w backend.Window) string {
	var f []string
	if w.Focused {
		f = append(f, "*")
	}
	if w.Minimized {
		f = append(f, "min")
	}
	if w.Hidden {
		f = append(f, "hidden")
	}
	return strings.Join(f, " ")
}

// formatQueryWindows builds qwin's plain Extra lines (help.txt OUTPUT
// :576-578): one two-space-indented, TAB-separated line per window -
// "<id>\t<pid>\t<app>\t<x>,<y> <w>x<h>\t<flags>\t<title>". An empty list
// yields no lines (qwin tolerates zero matches, help.txt :296-297).
func formatQueryWindows(wins []backend.Window) []string {
	lines := make([]string, 0, len(wins))
	for _, w := range wins {
		lines = append(lines, fmt.Sprintf("  %d\t%d\t%s\t%d,%d %dx%d\t%s\t%s",
			w.ID, w.PID, w.App, w.X, w.Y, w.W, w.H, windowFlags(w), w.Title))
	}
	return lines
}

// queryWindowsJSON is qwin's --jsonl command-specific field: a "windows"
// array (spec-consistent inference, same caveat as queryInfoJSON; help.txt
// JSONL shows no verbatim qwin example).
func queryWindowsJSON(wins []backend.Window) []output.KV {
	arr := make([]map[string]any, 0, len(wins))
	for _, w := range wins {
		arr = append(arr, map[string]any{
			"id": w.ID, "pid": w.PID, "app": w.App, "title": w.Title,
			"x": w.X, "y": w.Y, "w": w.W, "h": w.H,
			"focused": w.Focused, "minimized": w.Minimized, "hidden": w.Hidden,
		})
	}
	return []output.KV{{Key: "windows", Val: arr}}
}

// formatFocusDetail builds win's plain Detail line (help.txt OUTPUT :600):
// "id=<n> app=<s> matched=<n> <x>,<y> <w>x<h> <quoted title>". Factored out
// of doFocus (run.go) so ResultDetailFromJSON (wire_result.go) can reuse
// the identical format for the bridge forwarder's plain-mode
// reconstruction of a win line received over the JSONL-only bridge wire
// (260908-feat-remote-ssh Phase 0).
func formatFocusDetail(w backend.Window, matched int) string {
	return fmt.Sprintf("id=%d app=%s matched=%d %d,%d %dx%d %q",
		w.ID, w.App, matched, w.X, w.Y, w.W, w.H, w.Title)
}

// focusJSON is win's --jsonl command-specific fields (spec-consistent
// inference; help.txt JSONL shows no verbatim win example): the focused
// window's identity/geometry plus matched=N.
func focusJSON(w backend.Window, matched int) []output.KV {
	return []output.KV{
		{Key: "id", Val: w.ID}, {Key: "pid", Val: w.PID},
		{Key: "app", Val: w.App}, {Key: "title", Val: w.Title},
		{Key: "matched", Val: matched},
		{Key: "x", Val: w.X}, {Key: "y", Val: w.Y},
		{Key: "w", Val: w.W}, {Key: "h", Val: w.H},
	}
}

// formatQueryMouse builds qmouse's plain Extra line (help.txt:445-446,
// :583): "  x,y" (two-space indent, no TAB - a single field).
func formatQueryMouse(p backend.Point) []string {
	return []string{fmt.Sprintf("  %.0f,%.0f", p.X, p.Y)}
}

// queryMouseJSON is qmouse's --jsonl command-specific fields (spec-
// consistent inference, same caveat as queryInfoJSON).
func queryMouseJSON(p backend.Point) []output.KV {
	return []output.KV{{Key: "x", Val: p.X}, {Key: "y", Val: p.Y}}
}
