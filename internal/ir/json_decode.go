package ir

import (
	"encoding/json"
	"fmt"
)

// Unmarshal decodes an == IR JSON == document (help.txt :625-679) back into
// a *Sequence. It mirrors opNode (json.go:46-148) field-for-field so that
// Marshal(Unmarshal(Marshal(seq))) == Marshal(seq). Unknown top-level keys
// are ignored (the bridge protocol, ticket 260908-feat-remote-ssh Phase 0,
// passes the whole request body - IR fields plus a "run" object - straight
// through this decoder).
func Unmarshal(data []byte) (*Sequence, error) {
	var root struct {
		V        int               `json:"v"`
		Ops      []json.RawMessage `json:"ops"`
		Defaults struct {
			DelayMS        int `json:"delay_ms"`
			TextIntervalMS int `json:"text_interval_ms"`
			KeyGapMS       int `json:"key_gap_ms"`
		} `json:"defaults"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("ir: decode: %w", err)
	}
	if root.V == 0 {
		return nil, fmt.Errorf("ir: decode: missing or zero \"v\"")
	}

	seq := &Sequence{
		V: root.V,
		Defaults: Defaults{
			DelayMS:        root.Defaults.DelayMS,
			TextIntervalMS: root.Defaults.TextIntervalMS,
			KeyGapMS:       root.Defaults.KeyGapMS,
		},
	}
	seq.Ops = make([]Op, 0, len(root.Ops))
	for _, raw := range root.Ops {
		op, err := decodeOp(raw)
		if err != nil {
			return nil, err
		}
		seq.Ops = append(seq.Ops, op)
	}
	return seq, nil
}

func decodeOp(raw json.RawMessage) (Op, error) {
	var common struct {
		Line        int    `json:"line"`
		Src         string `json:"src"`
		Op          string `json:"op"`
		LineDelayMS *int   `json:"line_delay_ms"`
	}
	if err := json.Unmarshal(raw, &common); err != nil {
		return Op{}, fmt.Errorf("ir: decode op: %w", err)
	}
	o := Op{Line: common.Line, Src: common.Src, DelayMS: common.LineDelayMS}

	switch Kind(common.Op) {
	case KindFocus:
		var f struct {
			Selector jsonSelector `json:"selector"`
			WaitMS   int          `json:"wait_ms"`
			HasWait  bool         `json:"has_wait"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			return Op{}, fmt.Errorf("ir: decode focus: %w", err)
		}
		o.Kind = KindFocus
		o.Selector = f.Selector.toSelector()
		o.WaitMS = f.WaitMS
		o.HasWait = f.HasWait
	case KindQueryWindows:
		var f struct {
			Selector jsonSelector `json:"selector"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			return Op{}, fmt.Errorf("ir: decode query_windows: %w", err)
		}
		o.Kind = KindQueryWindows
		o.Selector = f.Selector.toSelector()
	case KindKey:
		var f struct {
			Mods   []string   `json:"mods"`
			Keys   [][]string `json:"keys"`
			Repeat int        `json:"repeat"`
			GapMS  int        `json:"gap_ms"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			return Op{}, fmt.Errorf("ir: decode key: %w", err)
		}
		o.Kind = KindKey
		o.Mods, o.Keys, o.Repeat, o.GapMS = f.Mods, f.Keys, f.Repeat, f.GapMS
	case KindKeyDown:
		var f struct {
			Keys [][]string `json:"keys"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			return Op{}, fmt.Errorf("ir: decode key_down: %w", err)
		}
		o.Kind = KindKeyDown
		o.Keys = f.Keys
	case KindKeyUp:
		var f struct {
			Keys [][]string `json:"keys"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			return Op{}, fmt.Errorf("ir: decode key_up: %w", err)
		}
		o.Kind = KindKeyUp
		o.Keys = f.Keys
	case KindText:
		var f struct {
			Text       string `json:"text"`
			IntervalMS int    `json:"interval_ms"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			return Op{}, fmt.Errorf("ir: decode text: %w", err)
		}
		o.Kind = KindText
		o.Text, o.IntervalMS = f.Text, f.IntervalMS
	case KindClipboard:
		var f struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			return Op{}, fmt.Errorf("ir: decode clipboard: %w", err)
		}
		o.Kind = KindClipboard
		o.Text = f.Text
	case KindPaste:
		var f struct {
			Text     string `json:"text"`
			SettleMS int    `json:"settle_ms"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			return Op{}, fmt.Errorf("ir: decode paste: %w", err)
		}
		o.Kind = KindPaste
		o.Text, o.IntervalMS = f.Text, f.SettleMS
	case KindQueryClip:
		var f struct {
			ToFile bool `json:"to_file"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			return Op{}, fmt.Errorf("ir: decode query_clipboard: %w", err)
		}
		o.Kind = KindQueryClip
		o.FromFile = f.ToFile
	case KindRClip:
		var f struct {
			Path string `json:"path"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			return Op{}, fmt.Errorf("ir: decode read_clipboard: %w", err)
		}
		o.Kind, o.Path, o.RClipType = KindRClip, f.Path, f.Type
	case KindMove:
		var f struct {
			Point      jsonPoint `json:"point"`
			DurationMS int       `json:"duration_ms"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			return Op{}, fmt.Errorf("ir: decode move: %w", err)
		}
		o.Kind = KindMove
		o.Point = f.Point.toPoint()
		o.DurationMS = f.DurationMS
	case KindClick:
		var f struct {
			Button string     `json:"button"`
			Count  int        `json:"count"`
			GapMS  int        `json:"gap_ms"`
			Mods   []string   `json:"mods"`
			Point  *jsonPoint `json:"point"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			return Op{}, fmt.Errorf("ir: decode click: %w", err)
		}
		o.Kind = KindClick
		o.Button, o.Count, o.GapMS, o.Mods = f.Button, f.Count, f.GapMS, f.Mods
		if f.Point != nil {
			o.HasPoint = true
			o.Point = f.Point.toPoint()
		}
	case KindButtonDown:
		var f struct {
			Button string     `json:"button"`
			Mods   []string   `json:"mods"`
			Point  *jsonPoint `json:"point"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			return Op{}, fmt.Errorf("ir: decode button_down: %w", err)
		}
		o.Kind = KindButtonDown
		o.Button, o.Mods = f.Button, f.Mods
		if f.Point != nil {
			o.HasPoint = true
			o.Point = f.Point.toPoint()
		}
	case KindButtonUp:
		var f struct {
			Button string `json:"button"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			return Op{}, fmt.Errorf("ir: decode button_up: %w", err)
		}
		o.Kind = KindButtonUp
		o.Button = f.Button
	case KindDrag:
		var f struct {
			Button     string      `json:"button"`
			Points     []jsonPoint `json:"points"`
			DurationMS int         `json:"duration_ms"`
			Steps      int         `json:"steps"`
			Mods       []string    `json:"mods"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			return Op{}, fmt.Errorf("ir: decode drag: %w", err)
		}
		o.Kind = KindDrag
		o.Button, o.DurationMS, o.Count, o.Mods = f.Button, f.DurationMS, f.Steps, f.Mods
		o.Points = make([]Point, 0, len(f.Points))
		for _, p := range f.Points {
			o.Points = append(o.Points, p.toPoint())
		}
	case KindScroll:
		var f struct {
			Dir   string `json:"dir"`
			Ticks int    `json:"ticks"`
			By    string `json:"by"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			return Op{}, fmt.Errorf("ir: decode scroll: %w", err)
		}
		o.Kind = KindScroll
		o.ScrollDir, o.Ticks, o.ScrollBy = f.Dir, f.Ticks, f.By
	case KindOpen:
		var f struct {
			Target  string `json:"target"`
			WaitMS  int    `json:"wait_ms"`
			HasWait bool   `json:"has_wait"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			return Op{}, fmt.Errorf("ir: decode open: %w", err)
		}
		o.Kind = KindOpen
		o.Target, o.WaitMS = f.Target, f.WaitMS
		o.HasWait = f.HasWait
	case KindExec:
		var f struct {
			Shell     bool     `json:"shell"`
			Cmd       string   `json:"cmd"`
			Argv      []string `json:"argv"`
			Noerr     bool     `json:"noerr"`
			TimeoutMS int      `json:"timeout_ms"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			return Op{}, fmt.Errorf("ir: decode exec: %w", err)
		}
		o.Kind = KindExec
		o.Shell, o.Cmd, o.Argv, o.Noerr, o.TimeoutMS = f.Shell, f.Cmd, f.Argv, f.Noerr, f.TimeoutMS
	case KindCapture:
		var f struct {
			Frame      string          `json:"frame"`
			Display    int             `json:"display"`
			Rect       *jsonRect       `json:"rect"`
			Scale      json.RawMessage `json:"scale"`
			Format     string          `json:"format"`
			Count      int             `json:"count"`
			IntervalMS int             `json:"interval_ms"`
			Label      string          `json:"label"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			return Op{}, fmt.Errorf("ir: decode capture: %w", err)
		}
		o.Kind = KindCapture
		o.Frame, o.Disp = f.Frame, f.Display
		if f.Rect != nil {
			r := f.Rect.toRect()
			o.Rect = &r
		}
		// scale is either the string "native" (o.ScaleNative) or a JSON
		// number (opNode/fnum) - see json.go:118-122.
		var scaleStr string
		if len(f.Scale) > 0 && f.Scale[0] == '"' {
			if err := json.Unmarshal(f.Scale, &scaleStr); err != nil {
				return Op{}, fmt.Errorf("ir: decode capture scale: %w", err)
			}
		}
		if scaleStr == "native" {
			o.ScaleNative = true
		} else if len(f.Scale) > 0 {
			if err := json.Unmarshal(f.Scale, &o.Scale); err != nil {
				return Op{}, fmt.Errorf("ir: decode capture scale: %w", err)
			}
		}
		o.Format, o.CapCount, o.CapInterval, o.Label = f.Format, f.Count, f.IntervalMS, f.Label
	case KindSleep:
		var f struct {
			DurationMS int `json:"duration_ms"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			return Op{}, fmt.Errorf("ir: decode sleep: %w", err)
		}
		o.Kind = KindSleep
		o.SleepMS = f.DurationMS
	case KindSet:
		var f struct {
			DelayMS        *int `json:"delay_ms"`
			TextIntervalMS *int `json:"text_interval_ms"`
			KeyGapMS       *int `json:"key_gap_ms"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			return Op{}, fmt.Errorf("ir: decode set: %w", err)
		}
		o.Kind = KindSet
		o.SetDelay, o.SetTxtms, o.SetKeyms = f.DelayMS, f.TextIntervalMS, f.KeyGapMS
	case KindQueryInfo:
		o.Kind = KindQueryInfo
	case KindQueryDisp:
		o.Kind = KindQueryDisp
	case KindQueryMouse:
		o.Kind = KindQueryMouse
	default:
		return Op{}, fmt.Errorf("ir: decode op: unknown op %q", common.Op)
	}
	return o, nil
}

// jsonSelector/jsonPoint/jsonRect mirror selectorNode/pointNode/rectNode
// (json.go:149-186) field-for-field for decoding.
type jsonSelector struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Regex bool   `json:"regex"`
}

func (s jsonSelector) toSelector() Selector {
	return Selector{Kind: s.Kind, Value: s.Value, Regex: s.Regex}
}

type jsonPoint struct {
	Frame   string  `json:"frame"`
	Display int     `json:"display"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	XPct    bool    `json:"x_pct"`
	YPct    bool    `json:"y_pct"`
}

func (p jsonPoint) toPoint() Point {
	return Point{Frame: p.Frame, Disp: p.Display, X: p.X, Y: p.Y, XPct: p.XPct, YPct: p.YPct}
}

type jsonRect struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	XPct bool    `json:"x_pct"`
	YPct bool    `json:"y_pct"`
	W    float64 `json:"w"`
	H    float64 `json:"h"`
}

func (r jsonRect) toRect() Rect {
	return Rect{X: r.X, Y: r.Y, W: r.W, H: r.H, XPct: r.XPct, YPct: r.YPct}
}
