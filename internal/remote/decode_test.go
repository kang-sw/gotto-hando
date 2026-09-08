package remote

import "testing"

// TestDecodeEventFunctionsMatchTheirOwnTag asserts each Decode* function
// accepts its own event tag and rejects every other line (including
// malformed JSON), matching JSONL's discriminated "event" field
// (help.txt JSONL :595-619).
func TestDecodeEventFunctionsMatchTheirOwnTag(t *testing.T) {
	start := `{"event":"start","out":"/tmp/x/","dest":"local"}`
	abort := `{"event":"abort","code":"E_SESSION","msg":"boom"}`
	perms := `{"event":"perms","accessibility":"ok","screen":"missing"}`
	done := `{"event":"done","ok":1,"err":0,"skip":0,"elapsed_ms":12,"held_released":0}`
	notEvent := `{"line":1,"status":"ok","cmd":"qinfo"}`
	malformed := `not json`

	if v, ok := DecodeStart([]byte(start)); !ok || v.Out != "/tmp/x/" || v.Dest != "local" {
		t.Errorf("DecodeStart(start) = %+v, %v", v, ok)
	}
	for _, bad := range []string{abort, perms, done, notEvent, malformed} {
		if _, ok := DecodeStart([]byte(bad)); ok {
			t.Errorf("DecodeStart(%q) = ok, want false", bad)
		}
	}

	if v, ok := DecodeAbort([]byte(abort)); !ok || v.Code != "E_SESSION" || v.Msg != "boom" {
		t.Errorf("DecodeAbort(abort) = %+v, %v", v, ok)
	}
	for _, bad := range []string{start, perms, done, notEvent, malformed} {
		if _, ok := DecodeAbort([]byte(bad)); ok {
			t.Errorf("DecodeAbort(%q) = ok, want false", bad)
		}
	}

	if v, ok := DecodePerms([]byte(perms)); !ok || v.Accessibility != "ok" || v.Screen != "missing" {
		t.Errorf("DecodePerms(perms) = %+v, %v", v, ok)
	}
	for _, bad := range []string{start, abort, done, notEvent, malformed} {
		if _, ok := DecodePerms([]byte(bad)); ok {
			t.Errorf("DecodePerms(%q) = ok, want false", bad)
		}
	}

	if v, ok := DecodeDone([]byte(done)); !ok || v.OK != 1 || v.ElapsedMS != 12 {
		t.Errorf("DecodeDone(done) = %+v, %v", v, ok)
	}
	for _, bad := range []string{start, abort, perms, notEvent, malformed} {
		if _, ok := DecodeDone([]byte(bad)); ok {
			t.Errorf("DecodeDone(%q) = ok, want false", bad)
		}
	}
}

// TestDecodeDoneStateUnknown asserts the "state":"unknown" field round-
// trips through DoneEvent.State (help.txt:598-600 - state is only ever
// set together with exit 5).
func TestDecodeDoneStateUnknown(t *testing.T) {
	line := `{"event":"done","ok":0,"err":0,"skip":0,"elapsed_ms":5,"held_released":1,"state":"unknown"}`
	v, ok := DecodeDone([]byte(line))
	if !ok {
		t.Fatal("DecodeDone = false, want true")
	}
	if v.State != "unknown" || v.HeldReleased != 1 {
		t.Errorf("DecodeDone = %+v, want State=unknown HeldReleased=1", v)
	}
}
