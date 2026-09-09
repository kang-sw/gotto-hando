package syntax

import (
	"strconv"
	"strings"

	"github.com/kang-sw/gotto-hando/internal/ir"
)

// buildOp turns a validated command + modifiers + payload into an ir.Op.
// It parses the per-command payload sub-grammar (COMMANDS, help.txt:299-448)
// and reads modifier values, returning a Diagnostic on any error.
func buildOp(line int, src string, spec cmdSpec, m modSet, payload string, payloadCol int) (*ir.Op, *ir.Diagnostic) {
	op := &ir.Op{Line: line, Src: src, Kind: spec.kind}

	// Per-line delay override d= (STATE MACHINE precedence; help.txt:210).
	if v, has := m.kvs["d"]; has {
		ms, msg := parseDurMS(v)
		if msg != "" {
			return nil, syntaxDiag(line, m.col["kv:d"], src, msg)
		}
		op.DelayMS = &ms
	}

	trimmed := strings.TrimSpace(payload)

	// Frame resolution for coordinate commands and cap (help.txt:259-261).
	frame, disp := "desktop", 0
	if spec.coordFrame {
		f, dsp, d := resolveFrame(line, src, m)
		if d != nil {
			return nil, d
		}
		frame, disp = f, dsp
	}

	switch spec.payload {
	case plNone:
		if trimmed != "" {
			return nil, syntaxDiag(line, payloadCol, src, spec.name+" takes no payload")
		}
		return finishNone(line, src, spec, m, op)

	case plKeys:
		return buildKeys(line, src, spec, m, trimmed, payloadCol, op)

	case plText:
		return buildText(line, src, spec, m, payload, payloadCol, op)

	case plQclip:
		op.FromFile = m.flags['f']
		if op.FromFile {
			// qclip[f]<path> saves the clipboard to a LOCAL file
			// (help.txt:353-354). The path is a local concern the wire IR
			// omits (help.txt:669-671, "to_file" only), but the future
			// local writer needs it, so keep it on the non-serialized
			// FilePath field.
			op.FilePath = trimmed
		}
		return op, nil

	case plRclip:
		if trimmed == "" {
			return nil, syntaxDiag(line, payloadCol, src, "rclip requires a file path")
		}
		if m.words["img"] && m.words["txt"] {
			return nil, syntaxDiag(line, payloadCol, src, "rclip img and txt modifiers are mutually exclusive")
		}
		op.Path, op.RClipType = trimmed, "auto"
		if m.words["img"] {
			op.RClipType = "image"
		}
		if m.words["txt"] {
			op.RClipType = "text"
		}
		return op, nil

	case plPoint:
		if trimmed == "" {
			return nil, syntaxDiag(line, payloadCol, src, spec.name+" requires x,y")
		}
		p, msg := parsePoint(trimmed, frame, disp)
		if msg != "" {
			return nil, syntaxDiag(line, payloadCol, src, msg)
		}
		op.Point, op.HasPoint = p, true
		msv, d := durVal(line, src, m, "ms", 0)
		if d != nil {
			return nil, d
		}
		op.DurationMS = msv
		return op, nil

	case plOptPoint:
		if trimmed != "" {
			p, msg := parsePoint(trimmed, frame, disp)
			if msg != "" {
				return nil, syntaxDiag(line, payloadCol, src, msg)
			}
			op.Point, op.HasPoint = p, true
		}
		btn, d := buttonVal(line, src, m)
		if d != nil {
			return nil, d
		}
		op.Button = btn
		op.Mods = orderedMods(m)
		if spec.kind == ir.KindClick {
			n, d := intVal(line, src, m, "n", 1)
			if d != nil {
				return nil, d
			}
			op.Count = n
			g, d := durVal(line, src, m, "ms", 60)
			if d != nil {
				return nil, d
			}
			op.GapMS = g
		}
		return op, nil

	case plPoints:
		if trimmed == "" {
			return nil, syntaxDiag(line, payloadCol, src, "drag requires at least one point")
		}
		pts, msg := parsePoints(trimmed, frame, disp)
		if msg != "" {
			return nil, syntaxDiag(line, payloadCol, src, msg)
		}
		op.Points = pts
		btn, d := buttonVal(line, src, m)
		if d != nil {
			return nil, d
		}
		if _, has := m.kvs["b"]; !has {
			btn = "left"
		}
		op.Button = btn
		op.Mods = orderedMods(m)
		dur, d := durVal(line, src, m, "ms", 500)
		if d != nil {
			return nil, d
		}
		op.DurationMS = dur
		steps, d := intVal(line, src, m, "steps", 20)
		if d != nil {
			return nil, d
		}
		op.Count = steps
		return op, nil

	case plScroll:
		return buildScroll(line, src, m, trimmed, payloadCol, op)

	case plSelector:
		if trimmed == "" {
			return nil, syntaxDiag(line, payloadCol, src, "win requires a selector")
		}
		op.Selector = parseSelector(trimmed, m.flags['r'])
		_, hasWait := m.kvs["wait"]
		op.HasWait = hasWait
		w, d := durVal(line, src, m, "wait", 0)
		if d != nil {
			return nil, d
		}
		op.WaitMS = w
		return op, nil

	case plOptSelector:
		if trimmed != "" {
			op.Selector = parseSelector(trimmed, m.flags['r'])
		} else {
			op.Selector = ir.Selector{Kind: "title", Value: "", Regex: m.flags['r']}
		}
		return op, nil

	case plTarget:
		if trimmed == "" {
			return nil, syntaxDiag(line, payloadCol, src, "open requires an app or path")
		}
		op.Target = trimmed
		_, hasWait := m.kvs["wait"]
		op.HasWait = hasWait
		w, d := durVal(line, src, m, "wait", 0)
		if d != nil {
			return nil, d
		}
		op.WaitMS = w
		return op, nil

	case plExec:
		return buildExec(line, src, m, trimmed, payloadCol, op)

	case plCap:
		return buildCap(line, src, m, frame, disp, trimmed, op)

	case plSleep:
		if trimmed == "" {
			return nil, syntaxDiag(line, payloadCol, src, "sleep requires a duration")
		}
		ms, msg := parseDurMS(trimmed)
		if msg != "" {
			return nil, syntaxDiag(line, payloadCol, src, msg)
		}
		op.SleepMS = ms
		return op, nil
	}
	return op, nil
}

func finishNone(line int, src string, spec cmdSpec, m modSet, op *ir.Op) (*ir.Op, *ir.Diagnostic) {
	switch spec.kind {
	case ir.KindButtonUp:
		btn, d := buttonVal(line, src, m)
		if d != nil {
			return nil, d
		}
		op.Button = btn
	case ir.KindSet:
		if v, has := m.kvs["delay"]; has {
			ms, msg := parseDurMS(v)
			if msg != "" {
				return nil, syntaxDiag(line, m.col["kv:delay"], src, msg)
			}
			op.SetDelay = &ms
		}
		if v, has := m.kvs["txtms"]; has {
			ms, msg := parseDurMS(v)
			if msg != "" {
				return nil, syntaxDiag(line, m.col["kv:txtms"], src, msg)
			}
			op.SetTxtms = &ms
		}
		if v, has := m.kvs["keyms"]; has {
			ms, msg := parseDurMS(v)
			if msg != "" {
				return nil, syntaxDiag(line, m.col["kv:keyms"], src, msg)
			}
			op.SetKeyms = &ms
		}
	}
	return op, nil
}

func buildKeys(line int, src string, spec cmdSpec, m modSet, trimmed string, payloadCol int, op *ir.Op) (*ir.Op, *ir.Diagnostic) {
	if trimmed == "" {
		return nil, syntaxDiag(line, payloadCol, src, spec.name+" requires key names")
	}
	groups := strings.Fields(trimmed)
	var chords [][]string
	for _, g := range groups {
		keyTokens := strings.Split(g, "+")
		var chord []string
		for _, kt := range keyTokens {
			if kt == "" {
				return nil, syntaxDiag(line, payloadCol, src, "empty key name")
			}
			sym, ok := canonKey(kt)
			if !ok {
				return nil, syntaxDiag(line, payloadCol, src, "unknown key name "+strconv.Quote(kt))
			}
			chord = append(chord, sym)
		}
		if spec.kind == ir.KindKey {
			chords = append(chords, chord)
		} else {
			// kd/ku: flatten to one single-key chord per held key.
			for _, s := range chord {
				chords = append(chords, []string{s})
			}
		}
	}
	op.Keys = chords
	if spec.kind == ir.KindKey {
		op.Mods = orderedMods(m)
		n, d := intVal(line, src, m, "n", 1)
		if d != nil {
			return nil, d
		}
		op.Repeat = n
		g, d := durVal(line, src, m, "ms", 30)
		if d != nil {
			return nil, d
		}
		op.GapMS = g
	}
	return op, nil
}

func buildText(line int, src string, spec cmdSpec, m modSet, payload string, payloadCol int, op *ir.Op) (*ir.Op, *ir.Diagnostic) {
	settleDefault := 0
	if spec.kind == ir.KindPaste {
		settleDefault = 50
	}
	if m.flags['f'] {
		op.FromFile = true
		op.FilePath = strings.TrimSpace(payload)
		if op.FilePath == "" {
			return nil, syntaxDiag(line, payloadCol, src, spec.name+"[f] requires a file path")
		}
	} else {
		text, rel, msg := applyEscapes(payload)
		if msg != "" {
			return nil, syntaxDiag(line, payloadCol+rel, src, msg)
		}
		op.Text, op.HasText = text, true
	}
	if spec.kind == ir.KindText || spec.kind == ir.KindPaste {
		v, d := durVal(line, src, m, "ms", settleDefault)
		if d != nil {
			return nil, d
		}
		op.IntervalMS = v
	}
	return op, nil
}

func buildScroll(line int, src string, m modSet, trimmed string, payloadCol int, op *ir.Op) (*ir.Op, *ir.Diagnostic) {
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return nil, syntaxDiag(line, payloadCol, src, "scroll requires a direction")
	}
	switch fields[0] {
	case "up", "down", "left", "right":
		op.ScrollDir = fields[0]
	default:
		return nil, syntaxDiag(line, payloadCol, src, "scroll direction must be up|down|left|right")
	}
	op.Ticks = 3
	if len(fields) >= 2 {
		n, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, syntaxDiag(line, payloadCol, src, "scroll ticks must be an integer")
		}
		op.Ticks = n
	}
	if len(fields) > 2 {
		return nil, syntaxDiag(line, payloadCol, src, "scroll takes only a direction and optional ticks")
	}
	by := "line"
	if v, has := m.kvs["by"]; has {
		if v != "line" && v != "page" {
			return nil, syntaxDiag(line, m.col["kv:by"], src, "by must be line or page")
		}
		by = v
	}
	op.ScrollBy = by
	return op, nil
}

func buildExec(line int, src string, m modSet, trimmed string, payloadCol int, op *ir.Op) (*ir.Op, *ir.Diagnostic) {
	if trimmed == "" {
		return nil, syntaxDiag(line, payloadCol, src, "exec requires a command")
	}
	op.Shell = m.words["shell"]
	op.Noerr = m.words["noerr"]
	if op.Shell {
		op.Cmd = trimmed
	} else {
		argv, msg := splitArgv(trimmed)
		if msg != "" {
			return nil, syntaxDiag(line, payloadCol, src, msg)
		}
		op.Argv = argv
	}
	t, d := durVal(line, src, m, "timeout", 10000)
	if d != nil {
		return nil, d
	}
	op.TimeoutMS = t
	return op, nil
}

func buildCap(line int, src string, m modSet, frame string, disp int, payload string, op *ir.Op) (*ir.Op, *ir.Diagnostic) {
	op.Frame = frame
	op.Disp = disp
	op.Format = "png"
	op.Scale = 1.0
	op.Label = "cap"
	op.CapCount = 1
	op.CapInterval = 100
	// The optional payload is an explicit LOCAL output path that overrides
	// the automatic <out>/NNNN-label-timestamp.png name (help.txt cap
	// path :413-416, "A cap payload overrides the path"). Like every path
	// it stays in FilePath, which the wire IR never carries (ops.go).
	op.FilePath = payload
	if v, has := m.kvs["rect"]; has {
		r, msg := parseRect(v)
		if msg != "" {
			return nil, syntaxDiag(line, m.col["kv:rect"], src, msg)
		}
		op.Rect = r
	}
	if v, has := m.kvs["scale"]; has {
		if v == "native" {
			op.ScaleNative = true
		} else {
			f, ok := parseFinite(v)
			if !ok {
				return nil, syntaxDiag(line, m.col["kv:scale"], src, "scale must be a number or native")
			}
			op.Scale = f
		}
	}
	if v, has := m.kvs["label"]; has {
		op.Label = v
	}
	n, d := intVal(line, src, m, "n", 1)
	if d != nil {
		return nil, d
	}
	op.CapCount = n
	iv, d := durVal(line, src, m, "ms", 100)
	if d != nil {
		return nil, d
	}
	op.CapInterval = iv
	return op, nil
}

// resolveFrame applies the "at most ONE frame modifier" and picks the IR
// frame (help.txt:259-261). r=pointer, w=window, disp=display N.
func resolveFrame(line int, src string, m modSet) (string, int, *ir.Diagnostic) {
	count := 0
	frame, disp := "desktop", 0
	if m.flags['r'] {
		count++
		frame = "pointer"
	}
	if m.flags['w'] {
		count++
		frame = "window"
	}
	if v, has := m.kvs["disp"]; has {
		count++
		frame = "display"
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return "", 0, syntaxDiag(line, m.col["kv:disp"], src, "disp must be a non-negative integer")
		}
		disp = n
	}
	if count > 1 {
		return "", 0, syntaxDiag(line, 1, src, "at most one frame modifier (r, w, disp=) per command")
	}
	return frame, disp, nil
}

func buttonVal(line int, src string, m modSet) (string, *ir.Diagnostic) {
	v, has := m.kvs["b"]
	if !has {
		return "left", nil
	}
	switch v {
	case "left", "right", "middle":
		return v, nil
	default:
		return "", syntaxDiag(line, m.col["kv:b"], src, "b must be left, right or middle")
	}
}

func intVal(line int, src string, m modSet, key string, def int) (int, *ir.Diagnostic) {
	v, has := m.kvs[key]
	if !has {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, syntaxDiag(line, m.col["kv:"+key], src, key+" must be an integer")
	}
	return n, nil
}

func durVal(line int, src string, m modSet, key string, def int) (int, *ir.Diagnostic) {
	v, has := m.kvs[key]
	if !has {
		return def, nil
	}
	ms, msg := parseDurMS(v)
	if msg != "" {
		return 0, syntaxDiag(line, m.col["kv:"+key], src, msg)
	}
	return ms, nil
}

// splitArgv splits an exec payload on spaces with "..." quoting only, no
// escapes, no expansion (help.txt:373-376).
func splitArgv(s string) ([]string, string) {
	var argv []string
	var cur strings.Builder
	inQuote := false
	hasTok := false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			hasTok = true
		case r == ' ' && !inQuote:
			if hasTok {
				argv = append(argv, cur.String())
				cur.Reset()
				hasTok = false
			}
		default:
			cur.WriteRune(r)
			hasTok = true
		}
	}
	if inQuote {
		return nil, "unbalanced quote in exec command"
	}
	if hasTok {
		argv = append(argv, cur.String())
	}
	if len(argv) == 0 {
		return nil, "exec requires a command"
	}
	return argv, ""
}
