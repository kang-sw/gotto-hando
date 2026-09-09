package remote

import "encoding/json"

// StartEvent is the remote process's own JSONL "start" object
// (help.txt JSONL :596-597). The wrapper uses it to confirm the remote is
// alive and prints its OWN out/dest (help-remote.txt HOW IT WORKS step 4
// permits rewriting only out/dest/capture-paths/err-src) - but Target.OS
// is relayed AS-IS from this event, not regenerated from the local
// process's own runtime.GOOS: target.os is not one of the permitted
// rewrites, and for the feature's primary cross-OS use case (e.g. macOS
// driving a Windows box) the two genuinely differ (review I1).
type StartEvent struct {
	Event  string     `json:"event"`
	Out    string     `json:"out"`
	Dest   string     `json:"dest"`
	Target TargetInfo `json:"target"`
}

// TargetInfo is the "start" event's nested "target" object (help.txt
// JSONL :596-597), mirroring internal/output's own private targetInfo
// shape so DecodeStart can round-trip the remote's real OS.
type TargetInfo struct {
	OS string `json:"os"`
}

// DecodeStart reports whether line is a "start" event.
func DecodeStart(line []byte) (StartEvent, bool) {
	var v StartEvent
	if json.Unmarshal(line, &v) != nil || v.Event != "start" {
		return StartEvent{}, false
	}
	return v, true
}

// AbortEvent is the JSONL "abort" event (help.txt:611-614).
type AbortEvent struct {
	Event string `json:"event"`
	Code  string `json:"code"`
	Msg   string `json:"msg"`
}

// DecodeAbort reports whether line is an "abort" event.
func DecodeAbort(line []byte) (AbortEvent, bool) {
	var v AbortEvent
	if json.Unmarshal(line, &v) != nil || v.Event != "abort" {
		return AbortEvent{}, false
	}
	return v, true
}

// PermsEvent is the --request-perms terminal JSONL event (help.txt
// JSONL "--request-perms" :617-619).
type PermsEvent struct {
	Event         string `json:"event"`
	Accessibility string `json:"accessibility"`
	Screen        string `json:"screen"`
}

// DecodePerms reports whether line is a "perms" event.
func DecodePerms(line []byte) (PermsEvent, bool) {
	var v PermsEvent
	if json.Unmarshal(line, &v) != nil || v.Event != "perms" {
		return PermsEvent{}, false
	}
	return v, true
}

// DoneEvent is the JSONL "done" event (help.txt:598-600).
type DoneEvent struct {
	Event        string `json:"event"`
	OK           int    `json:"ok"`
	Err          int    `json:"err"`
	Skip         int    `json:"skip"`
	ElapsedMS    int64  `json:"elapsed_ms"`
	HeldReleased int    `json:"held_released"`
	State        string `json:"state"`
}

// DecodeDone reports whether line is a "done" event.
func DecodeDone(line []byte) (DoneEvent, bool) {
	var v DoneEvent
	if json.Unmarshal(line, &v) != nil || v.Event != "done" {
		return DoneEvent{}, false
	}
	return v, true
}
