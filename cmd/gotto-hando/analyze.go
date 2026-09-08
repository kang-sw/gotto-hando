package main

import (
	"fmt"
	"io"
	"sort"

	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/output"
	"github.com/kang-sw/gotto-hando/internal/syntax"
)

// analyze implements the --check and --ir paths (EXECUTION steps 1-2,
// help.txt:464-471): parse -> inline [f] files -> static validate, stopping
// before connect. The parse/validation failure contract (the normative
// help.txt EXECUTION/OUTPUT edit that ships with this code) is:
//
//   - on failure: stdout stays EMPTY in both plain and --jsonl; every error
//     is written to stderr as "<CODE> line <n> col <c>: <message>" followed
//     by the source line indented two spaces; ALL errors are reported before
//     exit; exit 2. CODE is the actual code (E_SYNTAX for parse, E_VALIDATE
//     for validation), not a hardcoded prefix.
//   - --check success: stdout "ok <n> lines" (n = source line count,
//     including blank/comment lines), exit 0.
//   - --ir success: stdout the IR JSON document, exit 0.
func analyze(opts parsedOptions, lines []string, stdout, stderr io.Writer) int {
	defaults := ir.DefaultState()
	if opts.Delay != "" {
		if ms, ok := syntax.ParseDurationMS(opts.Delay); ok {
			defaults.DelayMS = ms
		}
	}

	seq, diags := syntax.Parse(lines, defaults)
	diags = append(diags, syntax.Inline(seq)...)
	diags = append(diags, ir.Validate(seq, len(lines))...)

	if len(diags) > 0 {
		sort.SliceStable(diags, func(i, j int) bool {
			if diags[i].Line != diags[j].Line {
				return diags[i].Line < diags[j].Line
			}
			return diags[i].Col < diags[j].Col
		})
		for _, d := range diags {
			fmt.Fprintf(stderr, "%s line %d col %d: %s\n", d.Code, d.Line, d.Col, d.Msg)
			if d.Src != "" {
				fmt.Fprintf(stderr, "  %s\n", d.Src)
			}
		}
		return output.ExitValidation
	}

	if opts.IR {
		b, err := ir.Marshal(seq)
		if err != nil {
			fmt.Fprintf(stderr, "abort: %s (%s)\n", err, output.EUnknown)
			return output.ExitPreflight
		}
		_, _ = stdout.Write(b)
		return output.ExitOK
	}

	// --check success.
	fmt.Fprintf(stdout, "ok %d lines\n", len(lines))
	return output.ExitOK
}
