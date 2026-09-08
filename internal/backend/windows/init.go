//go:build windows

package windows

// dpiAwarenessContextPerMonitorAwareV2 is DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2
// (WinUser.h: `((DPI_AWARENESS_CONTEXT)-4)`, a pseudo-handle sentinel, not a
// real pointer). ^uintptr(3) is bitwise-NOT of 3, i.e. every bit set except
// the low two - the same all-ones-except-low-bits pattern -4's two's
// complement representation has at any pointer width, so this reproduces
// the sentinel without depending on int64/uintptr size assumptions.
const dpiAwarenessContextPerMonitorAwareV2 = ^uintptr(3)

// init declares Per-Monitor-V2 DPI awareness at process start (ticket
// Decisions: manifest + runtime SetProcessDpiAwarenessContext fallback).
// The call's failure is ignored - when cmd/gotto-hando/rsrc_windows_amd64.syso's
// manifest already applied Per-Monitor-V2 awareness, a second
// SetProcessDpiAwarenessContext call legitimately fails (awareness is
// already set and cannot be changed again), which is not an error
// condition; when the manifest did not apply for some reason, this call
// alone is sufficient on Windows 10 1703+.
func init() {
	procSetProcessDpiAwarenessContext.Call(dpiAwarenessContextPerMonitorAwareV2)
}
