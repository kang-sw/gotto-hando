//go:build windows

package windows

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// sessionState implements the four qinfo session= values
// (help-windows.txt CHECK :15-25):
//
//   - this process's session id != the console's active session id (or no
//     console session exists at all, WTSGetActiveConsoleSessionId's
//     0xFFFFFFFF sentinel) -> "inactive" (service/session 0, disconnected
//     RDP, or an ssh shell with no bridge running - the ONLY way a <dest>
//     run reaches the desktop is through the bridge, help-windows.txt
//     INTERACTIVE SESSION REQUIRED).
//   - same session, OpenInputDesktop fails (WinSta0/the input desktop is
//     not accessible) -> "inactive", the same conservative fallback.
//   - same session, input desktop name is "Default" -> "active".
//   - same session, input desktop name is "Winlogon" -> "locked" (lock
//     screen, UAC secure desktop, Ctrl+Alt+Del screen -
//     help-windows.txt UAC AND ELEVATED APPS).
//   - same session, any other desktop name -> "inactive" (conservative
//     default for an undocumented desktop, mirrors darwin's
//     "otherwise inactive" fallback in session.go).
//
// "bridge" (this process is an ssh-started run answered by a GUI-session
// `gotto-hando --bridge`) has no real detection logic yet: --bridge does
// not exist until 260908-feat-remote-ssh, so a local windows run can never
// legitimately observe it. The branch stays reserved here rather than
// wired to a real probe, per the ticket's Out of Scope (mirrors darwin's
// reserved "bridge" branch).
func sessionState() string {
	pid := windows.GetCurrentProcessId()
	var sid uint32
	if err := windows.ProcessIdToSessionId(pid, &sid); err != nil {
		return "inactive"
	}
	active := windows.WTSGetActiveConsoleSessionId()
	const noActiveConsoleSession = 0xFFFFFFFF
	if active == noActiveConsoleSession || sid != active {
		return "inactive"
	}

	desk, err := openInputDesktop()
	if err != nil {
		return "inactive"
	}
	defer procCloseDesktop.Call(desk)

	name, err := desktopName(desk)
	if err != nil {
		return "inactive"
	}
	switch name {
	case "Default":
		return "active"
	case "Winlogon":
		return "locked"
	default:
		return "inactive"
	}
}

// isRemoteSessionID backs IsRemoteSession's (remote.go) session-id/WinSta0
// half: true when this process's session id is not the active console
// session, or the input desktop is not accessible - the same two probes
// sessionState uses before its "Default"/"Winlogon" branch, which
// IsRemoteSession deliberately does not reuse (that branch only
// distinguishes an unlocked from a locked CONSOLE session, not remote vs.
// local).
func isRemoteSessionID() bool {
	pid := windows.GetCurrentProcessId()
	var sid uint32
	if err := windows.ProcessIdToSessionId(pid, &sid); err != nil {
		return true
	}
	active := windows.WTSGetActiveConsoleSessionId()
	const noActiveConsoleSession = 0xFFFFFFFF
	if active == noActiveConsoleSession || sid != active {
		return true
	}

	desk, err := openInputDesktop()
	if err != nil {
		return true
	}
	defer procCloseDesktop.Call(desk)
	return false
}

// openInputDesktop opens the desktop currently receiving user input with
// just enough access (DESKTOP_READOBJECTS) to read its name - this must
// succeed even while the secure/Winlogon desktop is active, which a normal
// user has no switch or write rights to.
func openInputDesktop() (uintptr, error) {
	h, _, err := procOpenInputDesktop.Call(0, 0, desktopReadobjects)
	if h == 0 {
		return 0, err
	}
	return h, nil
}

// desktopName reads UOI_NAME from a desktop handle via
// GetUserObjectInformationW, distinguishing "Default" (active) from
// "Winlogon" (locked) per help-windows.txt CHECK.
func desktopName(desktop uintptr) (string, error) {
	var buf [64]uint16
	var needed uint32
	r, _, err := procGetUserObjectInformationW.Call(desktop, uoiName,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*2), uintptr(unsafe.Pointer(&needed)))
	if r == 0 {
		return "", err
	}
	return windows.UTF16ToString(buf[:]), nil
}
