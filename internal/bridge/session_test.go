package bridge_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kang-sw/gotto-hando/internal/backend/dryrun"
	"github.com/kang-sw/gotto-hando/internal/bridge"
	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/syntax"
)

func parseSeq(t *testing.T, lines ...string) *ir.Sequence {
	t.Helper()
	seq, diags := syntax.Parse(lines, ir.Defaults{})
	if len(diags) != 0 {
		t.Fatalf("parse diags: %+v", diags)
	}
	if d := ir.Validate(seq, len(lines)); len(d) != 0 {
		t.Fatalf("validate diags: %+v", d)
	}
	return seq
}

// writeRequest builds and writes one bridge request (bridge.EncodeRequest +
// "\n") to conn. Run in its own goroutine by callers, since net.Pipe()'s
// Write blocks until the peer's Read has consumed every byte.
func writeRequest(t *testing.T, conn net.Conn, seq *ir.Sequence, env bridge.RunEnvelope) {
	t.Helper()
	body, err := bridge.EncodeRequest(seq, env)
	if err != nil {
		t.Errorf("EncodeRequest: %v", err)
		return
	}
	if _, err := conn.Write(append(body, '\n')); err != nil {
		// A closed/half-torn-down pipe on the read side is expected in the
		// disconnect-mid-run test; only a real caller failure is fatal
		// there, so this stays a soft log, not t.Fatal.
		t.Logf("write request: %v", err)
	}
}

// event is one decoded JSONL line read back from a bridge connection.
type event struct {
	raw    map[string]any
	Event  string
	Line   int
	Status string
	Cmd    string
}

// readEvents reads every '\n'-delimited JSON object from conn until EOF (the
// server closes the connection at the end of Handle) and decodes each into
// an event.
func readEvents(t *testing.T, conn net.Conn) []event {
	t.Helper()
	var out []event
	sc := bufio.NewScanner(conn)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			t.Fatalf("decode event %q: %v", line, err)
		}
		ev := event{raw: raw}
		if v, ok := raw["event"].(string); ok {
			ev.Event = v
		}
		if v, ok := raw["line"].(float64); ok {
			ev.Line = int(v)
		}
		if v, ok := raw["status"].(string); ok {
			ev.Status = v
		}
		if v, ok := raw["cmd"].(string); ok {
			ev.Cmd = v
		}
		out = append(out, ev)
	}
	return out
}

// (a) a normal run produces the full start/result.../done JSONL sequence.
func TestHandleNormalRunProducesStartResultsDone(t *testing.T) {
	seq := parseSeq(t, "k[]a", "k[]b")
	be := &dryrun.Backend{}
	sess := &bridge.Session{Backend: be}

	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		sess.Handle(context.Background(), server)
		close(done)
	}()
	go writeRequest(t, client, seq, bridge.RunEnvelope{})

	events := readEvents(t, client)
	<-done

	if len(events) != 4 {
		t.Fatalf("events = %+v, want 4 (start, 2 results, done)", events)
	}
	if events[0].Event != "start" {
		t.Errorf("events[0].Event = %q, want start", events[0].Event)
	}
	if events[1].Status != "ok" || events[1].Cmd != "k" || events[1].Line != 1 {
		t.Errorf("events[1] = %+v, want ok/k/line=1", events[1])
	}
	if events[2].Status != "ok" || events[2].Cmd != "k" || events[2].Line != 2 {
		t.Errorf("events[2] = %+v, want ok/k/line=2", events[2])
	}
	if events[3].Event != "done" {
		t.Errorf("events[3].Event = %q, want done", events[3].Event)
	}
	if v, _ := events[3].raw["ok"].(float64); v != 2 {
		t.Errorf("done.ok = %v, want 2", events[3].raw["ok"])
	}
}

// (b) an IR "v" mismatch -> start then abort E_CONNECT, no done.
func TestHandleVersionMismatchAborts(t *testing.T) {
	seq := parseSeq(t, "qinfo")
	seq.V = ir.SchemaVersion + 1 // simulate a caller on a different IR schema
	be := &dryrun.Backend{}
	sess := &bridge.Session{Backend: be}

	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		sess.Handle(context.Background(), server)
		close(done)
	}()
	go writeRequest(t, client, seq, bridge.RunEnvelope{})

	events := readEvents(t, client)
	<-done

	if len(events) != 2 {
		t.Fatalf("events = %+v, want 2 (start, abort)", events)
	}
	if events[0].Event != "start" {
		t.Errorf("events[0].Event = %q, want start", events[0].Event)
	}
	if events[1].Event != "abort" {
		t.Fatalf("events[1].Event = %q, want abort", events[1].Event)
	}
	if code, _ := events[1].raw["code"].(string); code != "E_CONNECT" {
		t.Errorf("abort code = %q, want E_CONNECT", code)
	}
	// No line ever ran: the version check happens before engine.Run.
	if len(be.Calls) != 0 {
		t.Errorf("backend calls = %v, want none (version mismatch precedes Preflight/Run)", be.Calls)
	}
}

// (c) two concurrent Handle calls on two net.Pipe()s serialize: the second
// caller's own "start" event is only written after the first caller's
// "done" event, proven by blocking the first run mid-execution via
// dryrun.Backend.FailOn and only releasing it after starting (and letting
// settle) the second Handle goroutine.
func TestHandleSerializesConcurrentRuns(t *testing.T) {
	reached := make(chan struct{})
	release := make(chan struct{})
	var closeOnce sync.Once
	be := &dryrun.Backend{
		FailOn: func(call string) error {
			if call == "KeyDown shift" {
				closeOnce.Do(func() { close(reached) })
				<-release
			}
			return nil
		},
	}
	sess := &bridge.Session{Backend: be}

	blockSeq := parseSeq(t, "kd[]shift")
	fastSeq := parseSeq(t, "qinfo")

	var mu sync.Mutex
	var order []string
	record := func(s string) {
		mu.Lock()
		order = append(order, s)
		mu.Unlock()
	}

	c1, s1 := net.Pipe()
	done1 := make(chan struct{})
	go func() {
		sess.Handle(context.Background(), s1)
		close(done1)
	}()
	go writeRequest(t, c1, blockSeq, bridge.RunEnvelope{})
	read1Done := make(chan struct{})
	go func() {
		for _, ev := range readEvents(t, c1) {
			record("conn1:" + ev.Event)
		}
		close(read1Done)
	}()

	<-reached // the first run is now blocked mid-execution, past its own start

	c2, s2 := net.Pipe()
	done2 := make(chan struct{})
	go func() {
		sess.Handle(context.Background(), s2)
		close(done2)
	}()
	go writeRequest(t, c2, fastSeq, bridge.RunEnvelope{})
	read2Done := make(chan struct{})
	go func() {
		for _, ev := range readEvents(t, c2) {
			record("conn2:" + ev.Event)
		}
		close(read2Done)
	}()

	// Give a wrongly-unlocked second Handle a chance to race ahead before
	// we release the first - if single-run gating were broken, conn2's
	// start would already be recorded here.
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	for _, e := range order {
		if e == "conn2:start" {
			mu.Unlock()
			t.Fatal("second Handle produced output before the first finished - single-run gating not enforced")
		}
	}
	mu.Unlock()

	close(release)
	<-done1
	<-done2
	// Wait for both client-side reader goroutines to finish appending to
	// order too - done1/done2 only mean the SERVER side (Handle) returned,
	// not that the client's own event-reading goroutine has drained and
	// recorded everything yet.
	<-read1Done
	<-read2Done

	mu.Lock()
	defer mu.Unlock()
	idx := func(name string) int {
		for i, e := range order {
			if e == name {
				return i
			}
		}
		return -1
	}
	d1, s2start := idx("conn1:done"), idx("conn2:start")
	if d1 < 0 || s2start < 0 {
		t.Fatalf("order = %v, missing conn1:done or conn2:start", order)
	}
	if d1 > s2start {
		t.Fatalf("order = %v, want conn1:done before conn2:start", order)
	}
}

// (c2) a run that ends with a still-held key (kd[]shift, no matching ku)
// streams the end-of-run auto-release "warn" result over the wire, not just
// into Summary.Results - closing the gap the OnResult call added to
// run.go's post-loop releaseAll block (internal/engine/run.go) is meant to
// fix. TestHandleWriteFailureStopsRunAndReleasesHeldKey only checked the
// backend's KeyUp call, never the wire, so it could not have caught the
// warn line being dropped from the bridge's JSONL stream.
//
// Note: output.Result.Detail ("auto-released shift") is plain-mode-only
// (internal/output/writer.go: writeResultJSON never emits an r.Detail
// field, for any command, local or bridged) - so the wire-visible signal
// for this line is status=warn + cmd=kd, not free text. That is true of
// every JSONL run, not a bridge-specific gap, so it is out of scope here.
func TestHandleStreamsAutoReleaseWarn(t *testing.T) {
	seq := parseSeq(t, "kd[]shift")
	be := &dryrun.Backend{}
	sess := &bridge.Session{Backend: be}

	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		sess.Handle(context.Background(), server)
		close(done)
	}()
	go writeRequest(t, client, seq, bridge.RunEnvelope{})

	events := readEvents(t, client)
	<-done

	// Expect: start, ok (kd[]shift itself), warn (auto-release), done.
	if len(events) != 4 {
		t.Fatalf("events = %+v, want 4 (start, ok, warn, done)", events)
	}
	warn := events[2]
	if warn.Status != "warn" || warn.Cmd != "kd" {
		t.Fatalf("events[2] = %+v, want status=warn cmd=kd (the auto-released kd[]shift line)", warn)
	}
	if events[3].Event != "done" {
		t.Fatalf("events[3].Event = %q, want done (warn must precede done)", events[3].Event)
	}
}

// (d) a conn whose Write errors mid-run (simulating a disconnected caller)
// causes the run to stop early and a still-held key to appear released in
// dryrun.Backend.Calls. kd[]shift holds a key that only end-of-run
// releaseAll frees; k[]a/k[]b are plain taps (press+release, never held) so
// the only way "KeyUp shift" appears is via the cancel-triggered
// early-stop's releaseAll, not via k[]a/k[]b's own execution.
func TestHandleWriteFailureStopsRunAndReleasesHeldKey(t *testing.T) {
	seq := parseSeq(t, "kd[]shift", "k[]a", "k[]b")
	be := &dryrun.Backend{}
	sess := &bridge.Session{Backend: be}

	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		sess.Handle(context.Background(), server)
		close(done)
	}()
	go writeRequest(t, client, seq, bridge.RunEnvelope{})

	// Read exactly through the "start" event and the first (kd[]shift)
	// result, then close the client end. OnResult fires AFTER a line has
	// already executed, so k[]a itself still runs and streams its own
	// result - but that stream write is the one that now fails (the
	// client already closed), which cancels runCtx: k[]b (the next line)
	// is never reached, and releaseAll still frees the held shift key.
	sc := bufio.NewScanner(client)
	for i := 0; i < 2; i++ {
		if !sc.Scan() {
			t.Fatalf("scan %d: %v", i, sc.Err())
		}
	}
	client.Close()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Handle did not return after the caller disconnected")
	}

	found := false
	for _, c := range be.Calls {
		if c == "KeyUp shift" {
			found = true
		}
	}
	if !found {
		t.Fatalf("calls = %v, want a KeyUp shift release after the caller disconnected", be.Calls)
	}
	for _, c := range be.Calls {
		if c == "KeyDown b" {
			t.Fatalf("calls = %v, line 3 (k[]b) should not have run after the disconnect", be.Calls)
		}
	}
}

// (e) the RunEnvelope's KeepGoing field (-k) actually reaches engine.Run
// through the wire: decodeRequest (wire.go) maps env.KeepGoing into
// engine.RunOptions, and a JSON-tag typo there would silently break -k
// over ssh while every other test still passes (they all use the zero
// RunEnvelope). Builds the request through the real EncodeRequest path (not
// a hand-built struct) so the assertion covers the whole wire, not just
// decodeRequest in isolation. k[]a fails (FailOn), k[]b is a plain tap
// that only runs if the failure did not stop the line: default (fail-fast)
// skips it, KeepGoing:true runs it.
func TestHandleKeepGoingReachesEngineOverWire(t *testing.T) {
	failOnA := func(call string) error {
		if call == "KeyDown a" {
			return errors.New("injected failure")
		}
		return nil
	}

	run := func(t *testing.T, env bridge.RunEnvelope) []event {
		t.Helper()
		seq := parseSeq(t, "k[]a", "k[]b")
		be := &dryrun.Backend{FailOn: failOnA}
		sess := &bridge.Session{Backend: be}

		client, server := net.Pipe()
		done := make(chan struct{})
		go func() {
			sess.Handle(context.Background(), server)
			close(done)
		}()
		go writeRequest(t, client, seq, env)

		events := readEvents(t, client)
		<-done
		return events
	}

	t.Run("default fail-fast skips line 2", func(t *testing.T) {
		events := run(t, bridge.RunEnvelope{})
		if len(events) != 4 {
			t.Fatalf("events = %+v, want 4 (start, err, skip, done)", events)
		}
		if events[1].Status != "err" || events[1].Line != 1 {
			t.Errorf("events[1] = %+v, want err/line=1", events[1])
		}
		if events[2].Status != "skip" || events[2].Line != 2 {
			t.Errorf("events[2] = %+v, want skip/line=2 (no -k)", events[2])
		}
	})

	t.Run("keep_going:true runs line 2", func(t *testing.T) {
		events := run(t, bridge.RunEnvelope{KeepGoing: true})
		if len(events) != 4 {
			t.Fatalf("events = %+v, want 4 (start, err, ok, done)", events)
		}
		if events[1].Status != "err" || events[1].Line != 1 {
			t.Errorf("events[1] = %+v, want err/line=1", events[1])
		}
		if events[2].Status != "ok" || events[2].Line != 2 {
			t.Errorf("events[2] = %+v, want ok/line=2 (keep_going reached the wire)", events[2])
		}
	})
}
