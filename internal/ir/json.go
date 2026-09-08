package ir

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// Marshal renders the sequence as the == IR JSON == document (help.txt
// :630-679). Keys are emitted in the example's order (a plain map would
// sort them); the output is indented for readability. Field presence
// follows the example: point pct fields appear only when true, mods is
// always an array, capture/qclip carry no path, exec emits argv xor cmd.
func Marshal(seq *Sequence) ([]byte, error) {
	root := obj(
		kv{"v", num(seq.V)},
		kv{"ops", opsArray(seq.Ops)},
		kv{"defaults", obj(
			kv{"delay_ms", num(seq.Defaults.DelayMS)},
			kv{"text_interval_ms", num(seq.Defaults.TextIntervalMS)},
			kv{"key_gap_ms", num(seq.Defaults.KeyGapMS)},
		)},
	)
	var compact bytes.Buffer
	if err := encode(&compact, root); err != nil {
		return nil, err
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, compact.Bytes(), "", "  "); err != nil {
		return nil, err
	}
	pretty.WriteByte('\n')
	return pretty.Bytes(), nil
}

func opsArray(ops []Op) node {
	items := make([]node, 0, len(ops))
	for i := range ops {
		items = append(items, opNode(&ops[i]))
	}
	return arr(items...)
}

// opNode builds the ordered object for one op. Common fields (line, src,
// op) come first, then the kind-specific fields in help.txt order.
func opNode(o *Op) node {
	pairs := []kv{
		{"line", num(o.Line)},
		{"src", str(o.Src)},
		{"op", str(string(o.Kind))},
	}
	switch o.Kind {
	case KindFocus:
		pairs = append(pairs,
			kv{"selector", selectorNode(o.Selector)},
			kv{"wait_ms", num(o.WaitMS)},
			kv{"has_wait", boolean(o.HasWait)})
	case KindQueryWindows:
		pairs = append(pairs, kv{"selector", selectorNode(o.Selector)})
	case KindKey:
		pairs = append(pairs,
			kv{"mods", strArr(o.Mods)},
			kv{"keys", keysNode(o.Keys)},
			kv{"repeat", num(o.Repeat)},
			kv{"gap_ms", num(o.GapMS)})
	case KindKeyDown, KindKeyUp:
		pairs = append(pairs, kv{"keys", keysNode(o.Keys)})
	case KindText:
		pairs = append(pairs, kv{"text", str(o.Text)}, kv{"interval_ms", num(o.IntervalMS)})
	case KindClipboard:
		pairs = append(pairs, kv{"text", str(o.Text)})
	case KindPaste:
		pairs = append(pairs, kv{"text", str(o.Text)}, kv{"settle_ms", num(o.IntervalMS)})
	case KindQueryClip:
		pairs = append(pairs, kv{"to_file", boolean(o.FromFile)})
	case KindMove:
		pairs = append(pairs, kv{"point", pointNode(o.Point)}, kv{"duration_ms", num(o.DurationMS)})
	case KindClick:
		pairs = append(pairs,
			kv{"button", str(o.Button)},
			kv{"count", num(o.Count)},
			kv{"gap_ms", num(o.GapMS)},
			kv{"mods", strArr(o.Mods)})
		if o.HasPoint {
			pairs = append(pairs, kv{"point", pointNode(o.Point)})
		}
	case KindButtonDown:
		pairs = append(pairs, kv{"button", str(o.Button)}, kv{"mods", strArr(o.Mods)})
		if o.HasPoint {
			pairs = append(pairs, kv{"point", pointNode(o.Point)})
		}
	case KindButtonUp:
		pairs = append(pairs, kv{"button", str(o.Button)})
	case KindDrag:
		pairs = append(pairs,
			kv{"button", str(o.Button)},
			kv{"points", pointsNode(o.Points)},
			kv{"duration_ms", num(o.DurationMS)},
			kv{"steps", num(o.Count)},
			kv{"mods", strArr(o.Mods)})
	case KindScroll:
		pairs = append(pairs, kv{"dir", str(o.ScrollDir)}, kv{"ticks", num(o.Ticks)}, kv{"by", str(o.ScrollBy)})
	case KindOpen:
		pairs = append(pairs,
			kv{"target", str(o.Target)},
			kv{"wait_ms", num(o.WaitMS)},
			kv{"has_wait", boolean(o.HasWait)})
	case KindExec:
		pairs = append(pairs, kv{"shell", boolean(o.Shell)})
		if o.Shell {
			pairs = append(pairs, kv{"cmd", str(o.Cmd)})
		} else {
			pairs = append(pairs, kv{"argv", strArr(o.Argv)})
		}
		pairs = append(pairs, kv{"noerr", boolean(o.Noerr)}, kv{"timeout_ms", num(o.TimeoutMS)})
	case KindCapture:
		pairs = append(pairs, kv{"frame", str(o.Frame)})
		if o.Frame == "display" {
			pairs = append(pairs, kv{"display", num(o.Disp)})
		}
		if o.Rect != nil {
			pairs = append(pairs, kv{"rect", rectNode(*o.Rect)})
		}
		if o.ScaleNative {
			pairs = append(pairs, kv{"scale", str("native")})
		} else {
			pairs = append(pairs, kv{"scale", fnum(o.Scale)})
		}
		pairs = append(pairs,
			kv{"format", str(o.Format)},
			kv{"count", num(o.CapCount)},
			kv{"interval_ms", num(o.CapInterval)},
			kv{"label", str(o.Label)})
	case KindSleep:
		pairs = append(pairs, kv{"duration_ms", num(o.SleepMS)})
	case KindSet:
		if o.SetDelay != nil {
			pairs = append(pairs, kv{"delay_ms", num(*o.SetDelay)})
		}
		if o.SetTxtms != nil {
			pairs = append(pairs, kv{"text_interval_ms", num(*o.SetTxtms)})
		}
		if o.SetKeyms != nil {
			pairs = append(pairs, kv{"key_gap_ms", num(*o.SetKeyms)})
		}
	case KindQueryInfo, KindQueryDisp, KindQueryMouse:
		// no extra fields
	}
	if o.DelayMS != nil {
		pairs = append(pairs, kv{"line_delay_ms", num(*o.DelayMS)})
	}
	return obj(pairs...)
}

func selectorNode(s Selector) node {
	return obj(kv{"kind", str(s.Kind)}, kv{"value", str(s.Value)}, kv{"regex", boolean(s.Regex)})
}

func pointNode(p Point) node {
	pairs := []kv{{"frame", str(p.Frame)}}
	if p.Frame == "display" {
		pairs = append(pairs, kv{"display", num(p.Disp)})
	}
	pairs = append(pairs, kv{"x", fnum(p.X)}, kv{"y", fnum(p.Y)})
	if p.XPct {
		pairs = append(pairs, kv{"x_pct", boolean(true)})
	}
	if p.YPct {
		pairs = append(pairs, kv{"y_pct", boolean(true)})
	}
	return obj(pairs...)
}

func pointsNode(ps []Point) node {
	items := make([]node, 0, len(ps))
	for _, p := range ps {
		items = append(items, pointNode(p))
	}
	return arr(items...)
}

func rectNode(r Rect) node {
	pairs := []kv{{"x", fnum(r.X)}, {"y", fnum(r.Y)}}
	if r.XPct {
		pairs = append(pairs, kv{"x_pct", boolean(true)})
	}
	if r.YPct {
		pairs = append(pairs, kv{"y_pct", boolean(true)})
	}
	pairs = append(pairs, kv{"w", fnum(r.W)}, kv{"h", fnum(r.H)})
	return obj(pairs...)
}

func keysNode(chords [][]string) node {
	items := make([]node, 0, len(chords))
	for _, c := range chords {
		items = append(items, strArr(c))
	}
	return arr(items...)
}

// --- ordered JSON value model + encoder ---

type node interface{ encodeTo(*bytes.Buffer) error }

type kv struct {
	k string
	v node
}

type objNode struct{ pairs []kv }
type arrNode struct{ items []node }
type rawNode struct{ raw string }

func obj(pairs ...kv) node   { return objNode{pairs} }
func arr(items ...node) node { return arrNode{items} }
func num(i int) node         { return rawNode{strconv.Itoa(i)} }
func boolean(b bool) node    { return rawNode{strconv.FormatBool(b)} }
func fnum(f float64) node    { return rawNode{strconv.FormatFloat(f, 'g', -1, 64)} }
func str(s string) node {
	b, _ := json.Marshal(s)
	return rawNode{string(b)}
}
func strArr(ss []string) node {
	items := make([]node, 0, len(ss))
	for _, s := range ss {
		items = append(items, str(s))
	}
	return arrNode{items}
}

func encode(b *bytes.Buffer, n node) error { return n.encodeTo(b) }

func (n rawNode) encodeTo(b *bytes.Buffer) error { b.WriteString(n.raw); return nil }

func (n objNode) encodeTo(b *bytes.Buffer) error {
	b.WriteByte('{')
	for i, p := range n.pairs {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(p.k)
		b.Write(kb)
		b.WriteByte(':')
		if err := p.v.encodeTo(b); err != nil {
			return err
		}
	}
	b.WriteByte('}')
	return nil
}

func (n arrNode) encodeTo(b *bytes.Buffer) error {
	b.WriteByte('[')
	for i, it := range n.items {
		if i > 0 {
			b.WriteByte(',')
		}
		if err := it.encodeTo(b); err != nil {
			return err
		}
	}
	b.WriteByte(']')
	return nil
}
