package engine

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// formatExecDetail builds exec's plain Detail header (help.txt OUTPUT
// :591-593): "exit=<n> ms=<n> stdout=<n>B stderr=<n>B". ms is the
// engine-timed wall-clock duration of the Exec call, distinct from the
// generic TMS/t_ms field (writeResultPlain never prints TMS; it is a
// doc-specified field this line builds itself).
func formatExecDetail(r backend.ExecResult, ms int64) string {
	return fmt.Sprintf("exit=%d ms=%d stdout=%dB stderr=%dB", r.Exit, ms, len(r.Stdout), len(r.Stderr))
}

// splitOutputLines splits captured process output on "\n", dropping the
// trailing empty element a final newline produces. Empty input yields no
// lines at all.
func splitOutputLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// formatExecOutputLines builds exec's plain Extra lines (help.txt OUTPUT
// :593-598): one "  <1|2>\t<text>" line per output line, src 1 = stdout, 2
// = stderr. All stdout lines render before all stderr lines rather than
// true chronological interleaving across the two streams -
// backend.ExecResult carries two flat strings (not an ordered per-chunk
// log), so this reads help.txt's "in order of appearance, best effort" as
// license for this grouping (plan Escalations: exec stdout/stderr
// interleaving).
func formatExecOutputLines(r backend.ExecResult) []string {
	var lines []string
	for _, l := range splitOutputLines(r.Stdout) {
		lines = append(lines, fmt.Sprintf("  1\t%s", l))
	}
	for _, l := range splitOutputLines(r.Stderr) {
		lines = append(lines, fmt.Sprintf("  2\t%s", l))
	}
	return lines
}

// execJSON is exec's --jsonl command-specific fields, in the help.txt JSONL
// example's field order (:614-616).
func execJSON(r backend.ExecResult) []output.KV {
	return []output.KV{
		{Key: "exit", Val: r.Exit}, {Key: "stdout", Val: r.Stdout},
		{Key: "stderr", Val: r.Stderr}, {Key: "truncated", Val: r.Truncated},
	}
}

// openAppSelector derives a best-effort app window selector from open's
// Target (help.txt open :363-368; plan Escalations "open target -> window
// selector correlation"): a bare name (no "/") is used verbatim as an
// app-kind selector; a path is reduced to its base name with a trailing
// ".app" suffix stripped (/Applications/TextEdit.app -> TextEdit). A
// non-.app file/URL target has no statically-derivable app name, so the
// basename guess is a known, documented limitation for that case (it will
// usually not match the real default-handler app's window).
func openAppSelector(target string) ir.Selector {
	name := target
	if strings.Contains(target, "/") {
		name = filepath.Base(strings.TrimSuffix(strings.TrimRight(target, "/"), ".app"))
	}
	return ir.Selector{Kind: "app", Value: name}
}
