package remote

import (
	"strings"

	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// textCmdName maps the IR kinds that carry a [f]-able text payload to
// their source command word.
func textCmdName(k ir.Kind) string {
	switch k {
	case ir.KindText:
		return "txt"
	case ir.KindClipboard:
		return "clip"
	case ir.KindPaste:
		return "paste"
	}
	return ""
}

// modsWithoutF extracts src's "[...]" modifier list with the bare "f"
// flag token removed, by direct string surgery on the original source
// line (op.Src) rather than reconstructing it from IR fields - the IR
// does not retain "was ms= explicitly given" for text ops, only the
// resolved value, so re-deriving the bracket text from the parsed struct
// cannot round-trip an explicit-vs-default modifier faithfully. The
// command's own src always has exactly one "[...]" span (SYNTAX grammar),
// so the first '[' / first ']' pair is authoritative.
func modsWithoutF(src string) []string {
	open := strings.IndexByte(src, '[')
	closeIdx := strings.IndexByte(src, ']')
	if open < 0 || closeIdx < 0 || closeIdx < open {
		return nil
	}
	inner := src[open+1 : closeIdx]
	if inner == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(inner, ",") {
		if p == "f" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// QClipWriteback is one qclip[f]<path> line whose clipboard text must be
// written to a local file after the run (help-remote.txt HOW IT WORKS
// step 1: "qclip[f]<path> becomes qclip[] and its result is written to
// the local <path> afterwards").
type QClipWriteback struct {
	Line     int
	FilePath string
}

// Rewrite builds the sequence text to send over ssh stdin
// (help-remote.txt HOW IT WORKS step 1): every line passes through
// unmodified except txt[f]/paste[f]/clip[f] (rebuilt with the [f] file's
// already-inlined op.Text re-escaped into TEXT ESCAPES form, the f flag
// dropped) and qclip[f]<path> (rebuilt as qclip[], its write-back path
// recorded in qclips keyed by line number). Each rewritten line is
// re-checked against the 64 KiB line limit (ir.MaxLineBytes); a violation
// is returned as an ir.Diagnostic keyed on the ORIGINAL op.Src, per
// help-remote.txt step 1's "E_VALIDATE, exit 2, nothing sent" - the
// caller must not spawn ssh when diags is non-empty.
//
// lines must be the same source lines seq was parsed from (same 1-based
// line numbering via op.Line); seq must already have run through
// syntax.Inline so FromFile ops carry their file contents in op.Text.
func Rewrite(lines []string, seq *ir.Sequence) (rewritten []string, qclips []QClipWriteback, diags []ir.Diagnostic) {
	rewritten = append([]string(nil), lines...)
	for i := range seq.Ops {
		op := &seq.Ops[i]
		if !op.FromFile {
			continue
		}
		var line string
		switch op.Kind {
		case ir.KindText, ir.KindClipboard, ir.KindPaste:
			cmd := textCmdName(op.Kind)
			mods := modsWithoutF(op.Src)
			line = cmd + "[" + strings.Join(mods, ",") + "]" + escapeText(op.Text)
		case ir.KindQueryClip:
			mods := modsWithoutF(op.Src)
			line = "qclip[" + strings.Join(mods, ",") + "]"
		default:
			continue
		}
		if len(line) > ir.MaxLineBytes {
			diags = append(diags, ir.Diagnostic{
				Line: op.Line, Col: 1, Code: output.EValidate,
				Msg: "rewritten line exceeds 64 KiB (LIMITS)", Src: op.Src,
			})
			continue
		}
		rewritten[op.Line-1] = line
		if op.Kind == ir.KindQueryClip {
			qclips = append(qclips, QClipWriteback{Line: op.Line, FilePath: op.FilePath})
		}
	}
	return rewritten, qclips, diags
}
