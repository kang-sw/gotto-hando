package syntax

import (
	"math"
	"strconv"
	"strings"

	"github.com/kang-sw/gotto-hando/internal/ir"
)

// parseFinite parses a COORDINATES number: an integer or decimal
// (help.txt:247). Unlike strconv.ParseFloat it rejects NaN, Inf and
// scientific notation, which are outside the grammar and which --check
// number-format validation must reject (matching DURATIONS, which already
// rejects NaN/Inf). ok is false on a malformed value.
func parseFinite(s string) (float64, bool) {
	t := strings.TrimSpace(s)
	if strings.ContainsAny(t, "eEpPxX") {
		// scientific/hex float notation (e.g. 1e3, 0x1p4): outside the
		// integers-or-decimals grammar.
		return 0, false
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// parseCoord parses one COORDINATES coord (help.txt:200, :245-268):
// [ "-" ] number [ "%" ]. It returns the value, whether a percent suffix
// was present, and a message on failure.
func parseCoord(s string) (val float64, pct bool, msg string) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, false, "empty coordinate"
	}
	if strings.HasSuffix(t, "%") {
		pct = true
		t = t[:len(t)-1]
	}
	f, ok := parseFinite(t)
	if !ok {
		return 0, false, "invalid coordinate " + strconv.Quote(s)
	}
	return f, pct, ""
}

// parsePoint parses an "x,y" point in the given frame. relr reports whether
// the frame is the pointer (r flag): N% cannot combine with r
// (help.txt:261).
func parsePoint(s, frame string, disp int) (ir.Point, string) {
	parts := strings.Split(strings.TrimSpace(s), ",")
	if len(parts) != 2 {
		return ir.Point{}, "point must be x,y"
	}
	x, xpct, msg := parseCoord(parts[0])
	if msg != "" {
		return ir.Point{}, msg
	}
	y, ypct, msg := parseCoord(parts[1])
	if msg != "" {
		return ir.Point{}, msg
	}
	if frame == "pointer" && (xpct || ypct) {
		return ir.Point{}, "N% cannot be combined with r"
	}
	return ir.Point{Frame: frame, Disp: disp, X: x, Y: y, XPct: xpct, YPct: ypct}, ""
}

// parsePoints parses a space-separated polyline (help.txt:201, :334).
func parsePoints(s, frame string, disp int) ([]ir.Point, string) {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) == 0 {
		return nil, "drag needs at least one point"
	}
	pts := make([]ir.Point, 0, len(fields))
	for _, f := range fields {
		p, msg := parsePoint(f, frame, disp)
		if msg != "" {
			return nil, msg
		}
		pts = append(pts, p)
	}
	return pts, ""
}

// parseRect parses a cap rect=x:y:w:h value (help.txt:202, :406-407): x,y
// are coords (percent/negative allowed), w,h are plain numbers.
func parseRect(s string) (*ir.Rect, string) {
	parts := strings.Split(s, ":")
	if len(parts) != 4 {
		return nil, "rect must be x:y:w:h"
	}
	x, xpct, msg := parseCoord(parts[0])
	if msg != "" {
		return nil, msg
	}
	y, ypct, msg := parseCoord(parts[1])
	if msg != "" {
		return nil, msg
	}
	w, ok := parseFinite(parts[2])
	if !ok {
		return nil, "invalid rect width"
	}
	h, ok := parseFinite(parts[3])
	if !ok {
		return nil, "invalid rect height"
	}
	return &ir.Rect{X: x, Y: y, W: w, H: h, XPct: xpct, YPct: ypct}, ""
}
