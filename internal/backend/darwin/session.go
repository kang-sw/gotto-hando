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
// `gotto-hando --bridge`) has no real detection logic yet: --bridge does
// not exist until 260908-feat-remote-ssh, so a local darwin run can never
// legitimately observe it. The branch stays reserved here rather than
// wired to a real probe, per the ticket's Out of Scope.
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
