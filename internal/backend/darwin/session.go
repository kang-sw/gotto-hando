//go:build darwin

package darwin

// Session dictionary keys read from CGSessionCopyCurrentDictionary
// (help-macos.txt SESSION BRIDGE FOR SSH / SESSION, LOCK, SECURE INPUT).
// These are the private-but-stable SkyLight key names; CFSTR() cannot be
// read from a dlopen'd symbol, so the keys are recreated at runtime with
// CFStringCreateWithCString.
const (
	sessionKeyScreenIsLocked = "CGSSessionScreenIsLocked"
	sessionKeyOnConsole      = "kCGSSessionOnConsoleKey"
)

// sessionState implements the four qinfo session= values (help.txt:434-440,
// help-macos.txt:25-31):
//
//   - no session dictionary at all -> this process has no GUI session
//     (ssh without a bridge, or a non-Aqua session): "inactive".
//   - CGSSessionScreenIsLocked true -> "locked" (lock screen or screen
//     saver).
//   - kCGSSessionOnConsoleKey true (and not locked) -> "active".
//   - otherwise (a session dictionary exists but this process is not on
//     the console, e.g. fast user switching) -> "inactive".
//
// "bridge" (this process is an ssh-started run answered by a GUI-session
// `gotto-hando --bridge`) is never returned by sessionState() itself: a
// local darwin run's own session probe can only ever observe "active"/
// "locked"/"inactive" via CGSessionCopyCurrentDictionary. "bridge" is
// wired in via a separate probe swap instead - backend.go's NewBridge()
// substitutes bridgeSessionProbe{} (probes.go) for sessionProbe on the
// Backend that `gotto-hando --bridge` (260908-feat-remote-ssh Phase 2)
// constructs, so qinfo answered through the bridge always reports
// session=bridge without this function ever needing a "bridge" branch.
func sessionState() string {
	dict := cgSessionCopyCurrentDictionary()
	if dict == 0 {
		return "inactive"
	}
	defer cfRelease(dict)

	if boolValue(dict, sessionKeyScreenIsLocked) {
		return "locked"
	}
	if boolValue(dict, sessionKeyOnConsole) {
		return "active"
	}
	return "inactive"
}

// boolValue reads a CFBoolean-valued entry from a CFDictionary by string
// key. A missing key or non-boolean value reads as false, matching the
// session dictionary's convention of simply omitting false-valued keys.
func boolValue(dict uintptr, key string) bool {
	k := cfString(key)
	defer cfRelease(k)
	v := cfDictionaryGetValue(dict, k)
	if v == 0 {
		return false
	}
	return cfBooleanGetValue(v)
}
