package engine_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/backend/dryrun"
	"github.com/kang-sw/gotto-hando/internal/engine"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// TestExecOKFormat locks exec's ok-path Detail/Extra/JSON (help.txt OUTPUT
// :591-598, JSONL :614-616): the "exit=.. ms=.. stdout=..B stderr=..B"
// header, one "  <1|2>\t<text>" line per output line (stdout before
// stderr), and the exit/stdout/stderr/truncated JSONL fields.
func TestExecOKFormat(t *testing.T) {
	seq := parse(t, "exec[]true")
	be := &dryrun.Backend{ExecResult: backend.ExecResult{
		Exit: 0, Stdout: "line1\nline2\n", Stderr: "warn1\n",
	}}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	if len(sum.Results) != 1 || sum.Results[0].Status != "ok" {
		t.Fatalf("Results = %+v, want a single ok result", sum.Results)
	}
	r := sum.Results[0]

	wantDetail := "exit=0 ms=0 stdout=12B stderr=6B"
	if r.Detail != wantDetail {
		t.Errorf("Detail = %q, want %q", r.Detail, wantDetail)
	}
	wantExtra := []string{"  1\tline1", "  1\tline2", "  2\twarn1"}
	if !reflect.DeepEqual(r.Extra, wantExtra) {
		t.Errorf("Extra = %#v, want %#v", r.Extra, wantExtra)
	}
	wantJSON := []output.KV{
		{Key: "exit", Val: 0}, {Key: "stdout", Val: "line1\nline2\n"},
		{Key: "stderr", Val: "warn1\n"}, {Key: "truncated", Val: false},
	}
	if !reflect.DeepEqual(r.JSON, wantJSON) {
		t.Errorf("JSON = %#v, want %#v", r.JSON, wantJSON)
	}
	if !r.AlwaysShow {
		t.Error("AlwaysShow = false, want true")
	}
}

// TestExecErrExitOutputSurvives is the regression for the fail-closure
// value-capture footgun (plan Codebase Findings): a non-zero exit without
// noerr is an err result, but its Detail/Extra/JSON must still carry the
// captured output (help.txt: "Output lines also follow err results
// (non-zero exit, timeout)"). Routing this through the passed-in fail
// closure instead of setting res fields directly would silently drop it,
// since fail closes over execute()'s own res, not doExec's by-value copy.
func TestExecErrExitOutputSurvives(t *testing.T) {
	seq := parse(t, "exec[]false")
	be := &dryrun.Backend{ExecResult: backend.ExecResult{
		Exit: 1, Stdout: "", Stderr: "boom\n",
	}}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	if len(sum.Results) != 1 {
		t.Fatalf("Results = %+v, want one result", sum.Results)
	}
	r := sum.Results[0]
	if r.Status != "err" || r.ErrCode != output.EExec {
		t.Fatalf("status=%q code=%q, want err/E_EXEC", r.Status, r.ErrCode)
	}
	wantExtra := []string{"  2\tboom"}
	if !reflect.DeepEqual(r.Extra, wantExtra) {
		t.Errorf("Extra = %#v, want %#v (err result must still carry output lines)", r.Extra, wantExtra)
	}
	if len(r.JSON) == 0 {
		t.Error("JSON is empty, want exec's command-specific fields to survive the err path (dropped only by writeResultJSON's err branch, not by doExec)")
	}
}

// TestExecErrTimeoutOutputSurvives is the timeout twin of the above: a
// TimedOut result is also err, and must also keep its Extra output lines.
func TestExecErrTimeoutOutputSurvives(t *testing.T) {
	seq := parse(t, "exec[timeout=1s]sleep 5")
	be := &dryrun.Backend{ExecResult: backend.ExecResult{
		TimedOut: true, Stdout: "partial\n",
	}}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	if len(sum.Results) != 1 {
		t.Fatalf("Results = %+v, want one result", sum.Results)
	}
	r := sum.Results[0]
	if r.Status != "err" || r.ErrCode != output.ETimeout {
		t.Fatalf("status=%q code=%q, want err/E_TIMEOUT", r.Status, r.ErrCode)
	}
	wantExtra := []string{"  1\tpartial"}
	if !reflect.DeepEqual(r.Extra, wantExtra) {
		t.Errorf("Extra = %#v, want %#v (timeout err result must still carry output lines)", r.Extra, wantExtra)
	}
}

// TestExecNoerrSoftenedStillFormats: a noerr-softened non-zero exit is ok,
// and still carries the exec Detail/Extra/JSON formatting (not just a bare
// "ok" line).
func TestExecNoerrSoftenedStillFormats(t *testing.T) {
	seq := parse(t, "exec[noerr]grep -q x f")
	be := &dryrun.Backend{ExecResult: backend.ExecResult{Exit: 1, Stdout: "no match\n"}}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	if len(sum.Results) != 1 || sum.Results[0].Status != "ok" {
		t.Fatalf("Results = %+v, want a single ok result", sum.Results)
	}
	r := sum.Results[0]
	wantDetail := "exit=1 ms=0 stdout=9B stderr=0B"
	if r.Detail != wantDetail {
		t.Errorf("Detail = %q, want %q", r.Detail, wantDetail)
	}
	wantExtra := []string{"  1\tno match"}
	if !reflect.DeepEqual(r.Extra, wantExtra) {
		t.Errorf("Extra = %#v, want %#v", r.Extra, wantExtra)
	}
}

// TestExecSpawnFailureIsGenericExec: a backend Exec error (spawn failure)
// is a plain E_EXEC err with no output-formatting attempted (no r was ever
// returned to format).
func TestExecSpawnFailureIsGenericExec(t *testing.T) {
	seq := parse(t, "exec[]true")
	spawnErr := errors.New("spawn failure")
	be := &dryrun.Backend{FailOn: func(call string) error {
		if strings.HasPrefix(call, "Exec ") {
			return spawnErr
		}
		return nil
	}}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})
	if len(sum.Results) != 1 {
		t.Fatalf("Results = %+v, want one result", sum.Results)
	}
	r := sum.Results[0]
	if r.Status != "err" || r.ErrCode != output.EExec {
		t.Fatalf("status=%q code=%q, want err/E_EXEC", r.Status, r.ErrCode)
	}
}
