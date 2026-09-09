//go:build darwin && !dryrun

// This file mirrors dispatch_windows_test.go's withBridgeSession/dialBridge
// seam + net.Pipe() + dryrun.Backend pattern (260908-feat-remote-ssh Phase
// 2), plus request-perms-over-bridge cases the windows file has no
// equivalent for (request_perms is darwin-only). Unlike the windows file -
// which only compile-verifies on this dev host - these tests run natively:
// this dev host is macOS. The build constraint matches dispatch_darwin.go's
// own (`darwin && !dryrun`) rather than a bare `darwin`, so this file is
// correctly excluded (like dispatch_darwin.go itself) from the separate
// `go test -tags dryrun ./cmd/gotto-hando` build, which binds every symbol
// this file references (dialBridge, forwardToBridge, requestPerms) from
// dispatch_dryrun.go instead.
package main

import (
	"bytes"
	"context"
	"encoding/json"
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
// (dispatch_darwin.go) to hand forwardToBridge/requestPerms one end of a
// net.Pipe() whose other end is served by an in-process bridge.Session -
// no real unix socket or GUI session needed. The original dialBridge is
// restored via t.Cleanup. Darwin's DialBridge takes no name argument
// (unlike windows's PipeName-based dial), so this seam's function type
// differs from dispatch_windows_test.go's withBridgeSession.
func withBridgeSession(t *testing.T, sess *bridge.Session) {
	t.Helper()
	orig := dialBridge
	t.Cleanup(func() { dialBridge = orig })
	dialBridge = func() (io.ReadWriteCloser, error) {
		client, server := net.Pipe()
		go sess.Handle(context.Background(), server)
		return client, nil
	}
}

type closeBeforeDoneConn struct {
	io.ReadWriteCloser
}

func (c *closeBeforeDoneConn) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte(`"event":"done"`)) {
		_ = c.ReadWriteCloser.Close()
		return 0, io.ErrClosedPipe
	}
	return c.ReadWriteCloser.Write(p)
}

func withBridgeDisconnectBeforeDone(t *testing.T, be *dryrun.Backend) {
	t.Helper()
	orig := dialBridge
	t.Cleanup(func() { dialBridge = orig })
	dialBridge = func() (io.ReadWriteCloser, error) {
		client, server := net.Pipe()
		sess := &bridge.Session{Backend: be}
		go sess.Handle(context.Background(), &closeBeforeDoneConn{ReadWriteCloser: server})
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
	withBridgeSession(t, &bridge.Session{Backend: &dryrun.Backend{Clipboard: "hello-bridge"}})

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
	withBridgeSession(t, &bridge.Session{Backend: &dryrun.Backend{Clipboard: "hello-jsonl"}})

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

func TestForwardToBridgePostStartDisconnectWritesUnknownDoneOnce(t *testing.T) {
	be := &dryrun.Backend{Clipboard: "lost-terminal-done"}
	withBridgeDisconnectBeforeDone(t, be)

	opts := parsedOptions{Dest: "local", JSONL: true}
	seq, diags := parseAndValidate(opts, []string{"qclip", "qclip"})
	if len(diags) > 0 {
		t.Fatalf("parseAndValidate diags: %+v", diags)
	}

	var stdout, stderr bytes.Buffer
	code := forwardToBridge(opts, seq, &stdout, &stderr, testAbort(&stderr))
	if code != output.ExitStateUnknown {
		t.Fatalf("exit = %d, want %d (stderr=%q)", code, output.ExitStateUnknown, stderr.String())
	}
	if got := strings.Count(strings.Join(be.Calls, "\n"), "ClipboardGet"); got != 2 {
		t.Errorf("ClipboardGet calls = %d, want 2 (calls=%v)", got, be.Calls)
	}

	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode output line %q: %v", line, err)
		}
		events = append(events, event)
	}
	if len(events) != 4 || events[0]["event"] != "start" || events[3]["event"] != "done" {
		t.Fatalf("events = %#v, want one start, two results, one done", events)
	}
	if events[1]["status"] != "ok" || events[2]["status"] != "ok" || events[3]["ok"] != float64(2) || events[3]["state"] != "unknown" {
		t.Errorf("events = %#v, want two ok results and unknown done ok=2", events)
	}
}

// TestForwardToBridgeDialFailureAborts covers the "no bridge reachable"
// path (help-macos.txt SESSION BRIDGE FOR SSH (REMOTE MAC)): forwardToBridge
// must abort E_SESSION / exit 4 without ever printing a start line, exactly
// like a Preflight abort on the direct local path.
func TestForwardToBridgeDialFailureAborts(t *testing.T) {
	orig := dialBridge
	t.Cleanup(func() { dialBridge = orig })
	dialBridge = func() (io.ReadWriteCloser, error) {
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

// remoteEnv forces darwin.IsRemoteSession() to true for the duration of the
// test by setting SSH_CONNECTION (t.Setenv, auto-restored) - the only way
// requestPerms's own bridge-forwarding branch (not under a dialBridge-style
// seam) is reachable from a test running directly on this dev host.
func remoteEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SSH_CONNECTION", "10.0.0.1 22 10.0.0.2 22")
	t.Setenv("SSH_TTY", "")
}

// TestRequestPermsOverBridgeSuccess covers the "missed contract" gap the
// ticket's Codebase Findings called out: today `gotto-hando <dest>
// --request-perms` against a macOS target is broken end to end because
// requestPerms never routed to the bridge or emitted the JSONL perms
// object. This drives requestPerms itself (not just forwardToBridge) with
// IsRemoteSession forced true and dialBridge seamed to an in-process
// bridge.Session whose RequestPerms callback succeeds partially
// (accessibility ok, screen missing) - proving both the routing and the
// plain-mode perms= line writePermsResult builds from the decoded
// remote.PermsEvent.
func TestRequestPermsOverBridgeSuccess(t *testing.T) {
	remoteEnv(t)
	withBridgeSession(t, &bridge.Session{
		Backend: &dryrun.Backend{},
		RequestPerms: func() (accessibility, screen bool, err error) {
			return true, false, nil
		},
	})

	opts := parsedOptions{Dest: "local"}
	var stdout, stderr bytes.Buffer
	code := requestPerms(opts, &stdout, &stderr, testAbort(&stderr))
	if code != output.ExitPreflight {
		t.Fatalf("exit = %d, want %d (accessibility ok, screen missing) (stderr=%q)", code, output.ExitPreflight, stderr.String())
	}
	if out := stdout.String(); out != "perms=accessibility:ok,screen:missing\n" {
		t.Errorf("stdout = %q, want the plain perms= line", out)
	}
}

// TestRequestPermsOverBridgeNoBridgeAborts covers requestPerms's "no bridge
// reachable" path: same E_SESSION / exit 4 hint as forwardToBridge's own
// dial-failure case, and no output on stdout.
func TestRequestPermsOverBridgeNoBridgeAborts(t *testing.T) {
	remoteEnv(t)
	orig := dialBridge
	t.Cleanup(func() { dialBridge = orig })
	dialBridge = func() (io.ReadWriteCloser, error) {
		return nil, errors.New("no bridge listening")
	}

	opts := parsedOptions{Dest: "local"}
	var stdout, stderr bytes.Buffer
	code := requestPerms(opts, &stdout, &stderr, testAbort(&stderr))
	if code != output.ExitPreflight {
		t.Fatalf("exit = %d, want %d (stderr=%q)", code, output.ExitPreflight, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty (no output on a dial failure)", stdout.String())
	}
	if !strings.Contains(stderr.String(), "E_SESSION") {
		t.Errorf("stderr = %q, want E_SESSION", stderr.String())
	}
}
