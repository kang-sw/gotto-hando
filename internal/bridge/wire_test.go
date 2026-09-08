package bridge_test

import (
	"testing"

	"github.com/kang-sw/gotto-hando/internal/bridge"
	"github.com/kang-sw/gotto-hando/internal/ir"
)

// TestEncodeRequestIsSingleLine asserts EncodeRequest's output has no
// embedded newline - the newline-delimited framing (wire.go) depends on
// this, unlike ir.Marshal's own pretty-printed output.
func TestEncodeRequestIsSingleLine(t *testing.T) {
	seq := parseSeq(t, "qinfo")
	body, err := bridge.EncodeRequest(seq, bridge.RunEnvelope{DeadlineMS: 5000, KeepGoing: true})
	if err != nil {
		t.Fatalf("EncodeRequest: %v", err)
	}
	for _, b := range body {
		if b == '\n' {
			t.Fatalf("EncodeRequest output contains an embedded newline: %s", body)
		}
	}

	// A round trip through ir.Unmarshal (what Session.Handle actually
	// calls) must recover the same sequence, and the spliced "run" object
	// must be present and correctly typed.
	got, err := ir.Unmarshal(body)
	if err != nil {
		t.Fatalf("ir.Unmarshal(EncodeRequest(...)): %v", err)
	}
	if got.V != seq.V || len(got.Ops) != len(seq.Ops) {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, seq)
	}
}
