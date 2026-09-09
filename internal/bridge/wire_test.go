package bridge_test

import (
	"encoding/json"
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

// TestEncodeRequestPermsShape asserts EncodeRequestPerms's exact wire shape
// (help-remote.txt SESSION BRIDGE :122-129: {"v":1,"request_perms":true})
// and that it round-trips through ir.Unmarshal with no ops - what
// Session.Handle's decodeRequest actually calls. decodeRequest itself is
// unexported, so its full round trip (including the request_perms flag it
// now also returns) is exercised end-to-end instead by session_test.go's
// request-perms Handle tests.
func TestEncodeRequestPermsShape(t *testing.T) {
	body := bridge.EncodeRequestPerms()

	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(EncodeRequestPerms()): %v", err)
	}
	if v, ok := decoded["v"].(float64); !ok || int(v) != ir.SchemaVersion {
		t.Errorf("v = %v, want %d", decoded["v"], ir.SchemaVersion)
	}
	if rp, ok := decoded["request_perms"].(bool); !ok || !rp {
		t.Errorf("request_perms = %v, want true", decoded["request_perms"])
	}
	if len(decoded) != 2 {
		t.Errorf("decoded = %v, want exactly {v, request_perms}", decoded)
	}

	seq, err := ir.Unmarshal(body)
	if err != nil {
		t.Fatalf("ir.Unmarshal(EncodeRequestPerms()): %v", err)
	}
	if seq.V != ir.SchemaVersion {
		t.Errorf("seq.V = %d, want %d", seq.V, ir.SchemaVersion)
	}
	if len(seq.Ops) != 0 {
		t.Errorf("seq.Ops = %+v, want empty (request_perms carries no ops)", seq.Ops)
	}
}
