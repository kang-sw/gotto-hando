package syntax

import (
	"os"
	"unicode/utf8"

	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// maxFileBytes is the [f] file / line-length limit (LIMITS, help.txt:726).
const maxFileBytes = 64 * 1024

// Inline is the post-parse [f] pass (CONCEPT.md ch.8.1): it reads LOCAL
// files for txt/paste/clip[f] ops on the agent machine and inlines their
// contents into the op's text (help.txt:667). This keeps parser.go OS-free
// (the "parser is pure" Decision). qclip[f] writes a file at run time and is
// not read here. Reading, existence, UTF-8 and the 64 KiB cap are E_VALIDATE
// failures (help.txt:470). Both --check and --ir run this pass.
func Inline(seq *ir.Sequence) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for i := range seq.Ops {
		op := &seq.Ops[i]
		if !op.FromFile {
			continue
		}
		switch op.Kind {
		case ir.KindText, ir.KindClipboard, ir.KindPaste:
			data, err := os.ReadFile(op.FilePath)
			if err != nil {
				diags = append(diags, ir.Diagnostic{
					Line: op.Line, Col: 1, Code: output.EValidate,
					Msg: "cannot read [f] file: " + err.Error(), Src: op.Src,
				})
				continue
			}
			if len(data) > maxFileBytes {
				diags = append(diags, ir.Diagnostic{
					Line: op.Line, Col: 1, Code: output.EValidate,
					Msg: "[f] file exceeds 64 KiB", Src: op.Src,
				})
				continue
			}
			if !utf8.Valid(data) {
				diags = append(diags, ir.Diagnostic{
					Line: op.Line, Col: 1, Code: output.EValidate,
					Msg: "[f] file is not valid UTF-8", Src: op.Src,
				})
				continue
			}
			// Used as-is: no TEXT ESCAPES applied to file contents
			// (help.txt:283-284).
			op.Text = string(data)
			op.HasText = true
		}
	}
	return diags
}
