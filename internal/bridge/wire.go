// Package bridge is the GOOS-agnostic core of `gotto-hando --bridge`
// (help-remote.txt SESSION BRIDGE, 260908-feat-remote-ssh Phase 0): the
// wire protocol (this file) and the single-run-at-a-time session loop
// (session.go). It imports only internal/ir, internal/engine, internal/
// output and internal/backend - never anything windows-specific - so it is
// fully testable with net.Pipe()+internal/backend/dryrun, no real named
// pipe or GUI session required. The windows named-pipe listener
// (internal/backend/windows's PipeListener) is a thin transport adapter
// behind the io.ReadWriteCloser this package's Session.Handle already
// accepts.
package bridge

import (
	"bytes"
	"encoding/json"

	"github.com/kang-sw/gotto-hando/internal/ir"
)

// RunEnvelope is the bridge wire protocol's top-level "run" object
// (help-remote.txt SESSION BRIDGE :122-124): the caller writes ONE IR JSON
// document plus this extra object, e.g. {"deadline_ms":5000,
// "keep_going":true,"quiet":false,"cap_on_error":false}.
//
// DeadlineMS is decoded for wire completeness but not enforced -
// --timeout/deadline_ms enforcement is not wired into engine.Run anywhere
// in this codebase today (a pre-existing gap, not a Phase 0 regression;
// ticket Out of Scope).
type RunEnvelope struct {
	DeadlineMS int  `json:"deadline_ms"`
	KeepGoing  bool `json:"keep_going"`
	Quiet      bool `json:"quiet"`
	CapOnError bool `json:"cap_on_error"`
}

// Framing: one JSON document per bridge request, terminated by '\n' (the
// caller always emits it compact, one line - see EncodeRequest). Named
// pipes have no clean half-close primitive, and the ticket text only says
// "writes ONE IR JSON document ... then half-closes" without specifying
// the framing mechanics; this newline-delimited shape matches the
// JSONL-everywhere convention already used for every other wire/file
// format in this codebase and is trivially fakeable with net.Pipe()/
// io.Pipe() in tests. This is a Phase-0-local protocol detail Phase 1/2
// (the ssh-wrapped <dest> transport) can revisit.

// decodeRequest decodes one bridge request body: the IR sequence via
// ir.Unmarshal (which ignores unknown top-level keys, including "run"),
// plus the run envelope from the same bytes.
func decodeRequest(data []byte) (*ir.Sequence, RunEnvelope, error) {
	seq, err := ir.Unmarshal(data)
	if err != nil {
		return nil, RunEnvelope{}, err
	}
	var wrapped struct {
		Run RunEnvelope `json:"run"`
	}
	if err := json.Unmarshal(data, &wrapped); err != nil {
		return nil, RunEnvelope{}, err
	}
	return seq, wrapped.Run, nil
}

// EncodeRequest builds one bridge request body: seq's IR JSON compacted to
// a single line (ir.Marshal's own output is pretty-printed for readability
// and cannot be used as-is under the newline-delimited framing above) with
// the "run" envelope spliced in as an extra top-level key, and no trailing
// newline (the caller appends "\n" itself before writing to the
// connection). Exported so both cmd/gotto-hando's local forwarder
// (dispatch_windows.go's forwardToBridge) and this package's own tests
// build the identical wire shape from one place.
func EncodeRequest(seq *ir.Sequence, env RunEnvelope) ([]byte, error) {
	pretty, err := ir.Marshal(seq)
	if err != nil {
		return nil, err
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, pretty); err != nil {
		return nil, err
	}
	runObj, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}

	body := compact.Bytes() // e.g. {"v":1,"ops":[...],"defaults":{...}}
	out := make([]byte, 0, len(body)+len(runObj)+8)
	out = append(out, body[:len(body)-1]...) // everything up to the closing '}'
	out = append(out, ',')
	out = append(out, `"run":`...)
	out = append(out, runObj...)
	out = append(out, '}')
	return out, nil
}
