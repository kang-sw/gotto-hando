package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/output"
	"github.com/kang-sw/gotto-hando/internal/syntax"
)

// parseInlined parses and inline-resolves lines exactly like
// cmd/gotto-hando's parseAndValidate does (minus the final ir.Validate
// call, irrelevant to Rewrite), so Rewrite sees the same op.Text/FromFile
// shape a real CLI invocation would build.
func parseInlined(t *testing.T, lines []string) *ir.Sequence {
	t.Helper()
	seq, diags := syntax.Parse(lines, ir.DefaultState())
	diags = append(diags, syntax.Inline(seq)...)
	if len(diags) > 0 {
		t.Fatalf("parseInlined: unexpected diagnostics: %+v", diags)
	}
	return seq
}

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "payload.txt")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestRewriteTextCommands asserts txt[f]/paste[f]/clip[f] rewrite to their
// f-less form with the file's content escaped inline, mods otherwise
// preserved and reordered exactly as written (help-remote.txt HOW IT
// WORKS step 1).
func TestRewriteTextCommands(t *testing.T) {
	path := writeFile(t, "back\\slash\nnew\tline")
	lines := []string{
		"txt[f]" + path,
		"paste[f,ms=30]" + path,
		"clip[f]" + path,
	}
	seq := parseInlined(t, lines)
	rewritten, qclips, diags := Rewrite(lines, seq)
	if len(diags) != 0 {
		t.Fatalf("diags = %+v, want none", diags)
	}
	if len(qclips) != 0 {
		t.Fatalf("qclips = %+v, want none", qclips)
	}
	want := []string{
		`txt[]back\\slash\nnew\tline`,
		`paste[ms=30]back\\slash\nnew\tline`,
		`clip[]back\\slash\nnew\tline`,
	}
	for i, w := range want {
		if rewritten[i] != w {
			t.Errorf("rewritten[%d] = %q, want %q", i, rewritten[i], w)
		}
	}
}

// TestRewriteQClip asserts qclip[f]<path> becomes qclip[] with the write-
// back path recorded, keyed by line number.
func TestRewriteQClip(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "clip-out.txt")
	lines := []string{"qclip[f]" + outPath}
	seq := parseInlined(t, lines)
	rewritten, qclips, diags := Rewrite(lines, seq)
	if len(diags) != 0 {
		t.Fatalf("diags = %+v, want none", diags)
	}
	if rewritten[0] != "qclip[]" {
		t.Fatalf("rewritten[0] = %q, want %q", rewritten[0], "qclip[]")
	}
	if len(qclips) != 1 || qclips[0].Line != 1 || qclips[0].FilePath != outPath {
		t.Fatalf("qclips = %+v, want [{Line:1 FilePath:%q}]", qclips, outPath)
	}
}

// TestRewritePassesThroughNonFileLines asserts every line without an [f]
// payload is sent verbatim, byte-identical to what the caller wrote.
func TestRewritePassesThroughNonFileLines(t *testing.T) {
	lines := []string{"qinfo", "k[]a", "txt[]literal", "cap", "rclip[img]/target/shot.bmp"}
	seq := parseInlined(t, lines)
	rewritten, qclips, diags := Rewrite(lines, seq)
	if len(diags) != 0 {
		t.Fatalf("diags = %+v, want none", diags)
	}
	if len(qclips) != 0 {
		t.Fatalf("qclips = %+v, want none", qclips)
	}
	for i, l := range lines {
		if rewritten[i] != l {
			t.Errorf("rewritten[%d] = %q, want verbatim %q", i, rewritten[i], l)
		}
	}
	remoteSeq, remoteDiags := syntax.Parse(rewritten, ir.DefaultState())
	if len(remoteDiags) != 0 {
		t.Fatalf("remote parse diagnostics = %+v", remoteDiags)
	}
	op := remoteSeq.Ops[len(remoteSeq.Ops)-1]
	if op.Kind != ir.KindRClip || op.Path != "/target/shot.bmp" || op.RClipType != "image" {
		t.Fatalf("remote rclip op = %+v", op)
	}
}

// TestRewriteOversizedLineDiagnostic asserts a rewritten line that exceeds
// the 64 KiB line limit (LIMITS) is reported as an ir.Diagnostic keyed on
// the ORIGINAL src, not sent, per help-remote.txt step 1 ("E_VALIDATE,
// exit 2, nothing sent").
func TestRewriteOversizedLineDiagnostic(t *testing.T) {
	// A file of backslashes well under the 64 KiB [f]-file cap (so
	// syntax.Inline accepts it), but whose escaped form (each '\' becomes
	// "\\", doubling in size) plus the "txt[]" prefix exceeds
	// ir.MaxLineBytes once rewritten - the scenario Rewrite's own
	// post-rewrite re-check exists for.
	big := strings.Repeat(`\`, ir.MaxLineBytes/2+10)
	path := writeFile(t, big)
	lines := []string{"txt[f]" + path}
	seq := parseInlined(t, lines)
	rewritten, qclips, diags := Rewrite(lines, seq)
	if len(qclips) != 0 {
		t.Fatalf("qclips = %+v, want none", qclips)
	}
	if len(diags) != 1 {
		t.Fatalf("diags = %+v, want exactly one", diags)
	}
	d := diags[0]
	if d.Line != 1 {
		t.Errorf("diag.Line = %d, want 1", d.Line)
	}
	if d.Code != output.EValidate {
		t.Errorf("diag.Code = %s, want %s", d.Code, output.EValidate)
	}
	if d.Src != lines[0] {
		t.Errorf("diag.Src = %q, want original src %q", d.Src, lines[0])
	}
	// The line must be left as originally written (unrewritten) since the
	// caller must not send it.
	if rewritten[0] != lines[0] {
		t.Errorf("rewritten[0] = %q, want unmodified original %q", rewritten[0], lines[0])
	}
}
