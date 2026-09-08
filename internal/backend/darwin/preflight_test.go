//go:build darwin

package darwin

import (
	"context"
	"errors"
	"testing"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// fakeSession/fakePerm/fakeKeys back the Preflight test seam
// (probes.go's sessionProbe/permissionProbe/keyStateProbe interfaces).
type fakeSession struct{ state string }

func (f fakeSession) State() string { return f.state }

type fakePerm struct{ accessibility, screen bool }

func (f fakePerm) Accessibility() bool   { return f.accessibility }
func (f fakePerm) ScreenRecording() bool { return f.screen }

type fakeKeys struct{ keyHeld, buttonHeld bool }

func (f fakeKeys) AnyKeyHeld(codes []uint16) bool { return f.keyHeld }
func (f fakeKeys) AnyButtonHeld() bool            { return f.buttonHeld }

// newTestBackend builds a Backend with fake probes wired in and real FFI
// initialized (Preflight's coordinate-bounds check unconditionally calls
// activeDisplays(), which goes through the real CGGetActiveDisplayList
// binding - dlopen succeeds regardless of GUI session lock state, so this
// is safe to call from a locked/remote test session).
func newTestBackend(t *testing.T, session string, accessibility bool, keyHeld, buttonHeld bool) *Backend {
	t.Helper()
	if err := initFFI(); err != nil {
		t.Fatalf("initFFI: %v", err)
	}
	return &Backend{
		session: fakeSession{state: session},
		perm:    fakePerm{accessibility: accessibility},
		keys:    fakeKeys{keyHeld: keyHeld, buttonHeld: buttonHeld},
	}
}

func seqOf(ops ...ir.Op) *ir.Sequence {
	return &ir.Sequence{V: 1, Ops: ops}
}

func preflightCode(t *testing.T, err error) output.ErrorCode {
	t.Helper()
	if err == nil {
		t.Fatal("Preflight returned nil, want a *backend.PreflightError")
	}
	var pfErr *backend.PreflightError
	if !errors.As(err, &pfErr) {
		t.Fatalf("Preflight error %v is not a *backend.PreflightError", err)
	}
	return pfErr.Code
}

// (a) session locked + only qinfo/qdisp/qmouse/sleep/set lines -> passes,
// even though Accessibility is missing and a key is physically held: the
// exemption list means checks 2/3 never run either.
func TestPreflightQueryOnlyPassesWhenLocked(t *testing.T) {
	b := newTestBackend(t, "locked", false, true, true)
	seq := seqOf(
		ir.Op{Kind: ir.KindQueryInfo},
		ir.Op{Kind: ir.KindQueryDisp},
		ir.Op{Kind: ir.KindQueryMouse},
		ir.Op{Kind: ir.KindSleep},
		ir.Op{Kind: ir.KindSet},
	)
	if err := b.Preflight(context.Background(), seq); err != nil {
		t.Fatalf("Preflight() = %v, want nil (query-only sequence must never fail on session/permission/held-key)", err)
	}
}

// (b) session locked + a `k` line -> E_SESSION (check 1 fires before
// check 2, even though Accessibility is also missing here).
func TestPreflightLockedSessionWithKeyOp(t *testing.T) {
	b := newTestBackend(t, "locked", false, false, false)
	seq := seqOf(ir.Op{Kind: ir.KindKey, Keys: [][]string{{"a"}}})
	code := preflightCode(t, b.Preflight(context.Background(), seq))
	if code != output.ESession {
		t.Errorf("code = %s, want %s", code, output.ESession)
	}
}

// (c) session ok, Accessibility missing + a `k` line -> E_PERMISSION.
func TestPreflightMissingAccessibility(t *testing.T) {
	b := newTestBackend(t, "active", false, false, false)
	seq := seqOf(ir.Op{Kind: ir.KindKey, Keys: [][]string{{"a"}}})
	code := preflightCode(t, b.Preflight(context.Background(), seq))
	if code != output.EPermission {
		t.Errorf("code = %s, want %s", code, output.EPermission)
	}
}

// (d) a held key reported by the fake keyStateProbe + a `k` line ->
// E_INPUT (check 3, after session/permission both pass).
func TestPreflightHeldKeyBlocks(t *testing.T) {
	b := newTestBackend(t, "active", true, true, false)
	seq := seqOf(ir.Op{Kind: ir.KindKey, Keys: [][]string{{"a"}}})
	code := preflightCode(t, b.Preflight(context.Background(), seq))
	if code != output.EInput {
		t.Errorf("code = %s, want %s", code, output.EInput)
	}
}

// A held mouse button (not a key) must trip the same check 3.
func TestPreflightHeldButtonBlocks(t *testing.T) {
	b := newTestBackend(t, "active", true, false, true)
	seq := seqOf(ir.Op{Kind: ir.KindKey, Keys: [][]string{{"a"}}})
	code := preflightCode(t, b.Preflight(context.Background(), seq))
	if code != output.EInput {
		t.Errorf("code = %s, want %s", code, output.EInput)
	}
}

// bridge session state is equivalent to active for check 1.
func TestPreflightBridgeSessionPasses(t *testing.T) {
	b := newTestBackend(t, "bridge", true, false, false)
	seq := seqOf(ir.Op{Kind: ir.KindKey, Keys: [][]string{{"a"}}})
	if err := b.Preflight(context.Background(), seq); err != nil {
		t.Fatalf("Preflight() = %v, want nil (bridge session is accepted like active)", err)
	}
}

// check 5: a key name unsupported on darwin (volup) surfaces E_INPUT with
// an unsupported-key message, only reached once checks 1-4 pass.
func TestPreflightUnsupportedKeyName(t *testing.T) {
	b := newTestBackend(t, "active", true, false, false)
	seq := seqOf(ir.Op{Kind: ir.KindKey, Keys: [][]string{{"volup"}}})
	code := preflightCode(t, b.Preflight(context.Background(), seq))
	if code != output.EInput {
		t.Errorf("code = %s, want %s", code, output.EInput)
	}
}
