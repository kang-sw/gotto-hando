//go:build windows

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/kang-sw/gotto-hando/internal/backend/dryrun"
	"github.com/kang-sw/gotto-hando/internal/bridge"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// withBridgeSession overrides the package-level dialBridge seam
// (dispatch_windows.go) to hand forwardToBridge one end of a net.Pipe()
// whose other end is served by an in-process bridge.Session over be - no
// real named pipe or GUI session needed (260908-feat-remote-ssh Phase 0).
// The original dialBridge is restored via t.Cleanup.
func withBridgeSession(t *testing.T, be *dryrun.Backend) {
	t.Helper()
	orig := dialBridge
	t.Cleanup(func() { dialBridge = orig })
	dialBridge = func(name string) (io.ReadWriteCloser, error) {
		client, server := net.Pipe()
		sess := &bridge.Session{Backend: be}
		go sess.Handle(context.Background(), server)
		return client, nil
	}
}

func testAbort(stderr *bytes.Buffer) func(output.ErrorCode, string) int {
	return func(code output.ErrorCode, msg string) int {
		stderr.WriteString("abort: " + msg + " (" + string(code) + ")\n")
		return output.AbortExit(code)
	}
}

// TestForwardToBridgePlainRelaysQueryResult exercises forwardToBridge end
// to end over net.Pipe(): a plain-mode "local" run of a single qclip line
// is forwarded to an in-process bridge.Session backed by dryrun.Backend,
// and the reconstructed Detail (via engine.ResultDetailFromJSON) must
// match what a real local run would have printed.
func TestForwardToBridgePlainRelaysQueryResult(t *testing.T) {
	withBridgeSession(t, &dryrun.Backend{Clipboard: "hello-bridge"})

	opts := parsedOptions{Dest: "local"}
	seq, diags := parseAndValidate(opts, []string{"qclip"})
	if len(diags) > 0 {
		t.Fatalf("parseAndValidate diags: %+v", diags)
	}

	var stdout, stderr bytes.Buffer
	code := forwardToBridge(opts, seq, &stdout, &stderr, testAbort(&stderr))
	if code != output.ExitOK {
		t.Fatalf("exit = %d, want 0 (stderr=%q)", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "hello-bridge") {
		t.Errorf("stdout = %q, want it to contain the reconstructed qclip detail %q", out, "hello-bridge")
	}
	if !strings.HasPrefix(out, "out ") {
		t.Errorf("stdout = %q, want it to start with the local forwarder's own start line", out)
	}
	if !strings.Contains(out, "done ok=1") {
		t.Errorf("stdout = %q, want a done ok=1 line", out)
	}
}

// TestForwardToBridgeJSONLRelaysVerbatim exercises the --jsonl path: the
// bridge's own per-line result/done objects (internal/output.WriteResult/
// WriteDone - the exact functions the local run path already uses) are
// forwarded byte-for-byte.
func TestForwardToBridgeJSONLRelaysVerbatim(t *testing.T) {
	withBridgeSession(t, &dryrun.Backend{Clipboard: "hello-jsonl"})

	opts := parsedOptions{Dest: "local", JSONL: true}
	seq, diags := parseAndValidate(opts, []string{"qclip"})
	if len(diags) > 0 {
		t.Fatalf("parseAndValidate diags: %+v", diags)
	}

	var stdout, stderr bytes.Buffer
	code := forwardToBridge(opts, seq, &stdout, &stderr, testAbort(&stderr))
	if code != output.ExitOK {
		t.Fatalf("exit = %d, want 0 (stderr=%q)", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"event":"start"`) {
		t.Errorf("stdout = %q, want a start event", out)
	}
	if !strings.Contains(out, `"text":"hello-jsonl"`) {
		t.Errorf("stdout = %q, want the forwarded qclip text field", out)
	}
	if !strings.Contains(out, `"event":"done"`) {
		t.Errorf("stdout = %q, want a done event", out)
	}
}

// TestForwardToBridgeDialFailureAborts covers the "no bridge reachable"
// path (help-windows.txt :72-73): forwardToBridge must abort E_SESSION /
// exit 4 without ever printing a start line, exactly like a Preflight
// abort on the direct local path.
func TestForwardToBridgeDialFailureAborts(t *testing.T) {
	orig := dialBridge
	t.Cleanup(func() { dialBridge = orig })
	dialBridge = func(name string) (io.ReadWriteCloser, error) {
		return nil, errors.New("no bridge listening")
	}

	opts := parsedOptions{Dest: "local"}
	seq, diags := parseAndValidate(opts, []string{"qclip"})
	if len(diags) > 0 {
		t.Fatalf("parseAndValidate diags: %+v", diags)
	}

	var stdout, stderr bytes.Buffer
	code := forwardToBridge(opts, seq, &stdout, &stderr, testAbort(&stderr))
	if code != output.ExitPreflight {
		t.Fatalf("exit = %d, want %d (stderr=%q)", code, output.ExitPreflight, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty (no start line on a dial failure)", stdout.String())
	}
	if !strings.Contains(stderr.String(), "E_SESSION") {
		t.Errorf("stderr = %q, want E_SESSION", stderr.String())
	}
}
