// Package dryrun is a test-only Backend that records every call in order
// and returns canned results (plan Phase 2 step 7). It is NEVER selectable
// from the CLI and is never bound to `local`: cmd/gotto-hando does not
// import it. Only internal/engine and its tests do, to exercise the engine
// against the Backend contract (fail-fast, -k, held release, done counts).
package dryrun

import (
	"context"
	"fmt"
	"time"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/ir"
)

// Backend records calls and returns canned results. FailOn, when set, is
// consulted with each recorded call string; a non-nil return makes that
// primitive fail (used to drive fail-fast / -k tests).
type Backend struct {
	Calls []string

	FailOn func(call string) error

	InfoResult     backend.Info
	WindowsResult  []backend.Window
	CaptureResult  backend.Image
	Clipboard      string
	MousePosResult backend.Point
	ExecResult     backend.ExecResult
}

func (b *Backend) record(format string, args ...any) error {
	call := fmt.Sprintf(format, args...)
	b.Calls = append(b.Calls, call)
	if b.FailOn != nil {
		return b.FailOn(call)
	}
	return nil
}

func (b *Backend) Info(context.Context) (backend.Info, error) {
	if err := b.record("Info"); err != nil {
		return backend.Info{}, err
	}
	return b.InfoResult, nil
}

func (b *Backend) Preflight(context.Context, *ir.Sequence) error {
	return b.record("Preflight")
}

func (b *Backend) KeyDown(_ context.Context, key string) error { return b.record("KeyDown %s", key) }
func (b *Backend) KeyUp(_ context.Context, key string) error   { return b.record("KeyUp %s", key) }

func (b *Backend) TypeText(_ context.Context, s string, interval time.Duration) error {
	return b.record("TypeText %q", s)
}

func (b *Backend) MouseMove(_ context.Context, p backend.Point, dur time.Duration) error {
	return b.record("MouseMove %.0f,%.0f", p.X, p.Y)
}

func (b *Backend) ButtonDown(_ context.Context, bt backend.Button) error {
	return b.record("ButtonDown %s", bt)
}
func (b *Backend) ButtonUp(_ context.Context, bt backend.Button) error {
	return b.record("ButtonUp %s", bt)
}

func (b *Backend) Scroll(_ context.Context, dir backend.Dir, ticks int, by backend.ScrollUnit, pageHeightPixels int) error {
	return b.record("Scroll %s %d %s page=%d", dir, ticks, by, pageHeightPixels)
}

func (b *Backend) Windows(_ context.Context, sel ir.Selector) ([]backend.Window, error) {
	if err := b.record("Windows %s:%s", sel.Kind, sel.Value); err != nil {
		return nil, err
	}
	return b.WindowsResult, nil
}

func (b *Backend) Focus(_ context.Context, w backend.Window) error {
	return b.record("Focus %d", w.ID)
}

func (b *Backend) Capture(_ context.Context, req backend.CaptureReq) (backend.Image, error) {
	if err := b.record("Capture %s", req.Frame); err != nil {
		return backend.Image{}, err
	}
	return b.CaptureResult, nil
}

func (b *Backend) ClipboardGet(context.Context) (string, error) {
	if err := b.record("ClipboardGet"); err != nil {
		return "", err
	}
	return b.Clipboard, nil
}

func (b *Backend) ClipboardSet(_ context.Context, s string) error {
	return b.record("ClipboardSet %q", s)
}

func (b *Backend) MousePos(context.Context) (backend.Point, error) {
	if err := b.record("MousePos"); err != nil {
		return backend.Point{}, err
	}
	return b.MousePosResult, nil
}

func (b *Backend) Open(_ context.Context, target string) error {
	return b.record("Open %s", target)
}

func (b *Backend) Exec(_ context.Context, req backend.ExecReq) (backend.ExecResult, error) {
	if err := b.record("Exec %v shell=%v", append(req.Argv, req.Cmd), req.Shell); err != nil {
		return backend.ExecResult{}, err
	}
	return b.ExecResult, nil
}

// Ensure the interface is satisfied.
var _ backend.Backend = (*Backend)(nil)
