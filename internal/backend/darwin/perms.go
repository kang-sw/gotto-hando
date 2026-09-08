//go:build darwin

package darwin

import "github.com/ebitengine/purego/objc"

// RequestPerms implements `local --request-perms` on darwin (help.txt
// --request-perms :91-108, help-macos.txt GRANTING PERMISSIONS :101-113): a
// process in the GUI session calls AXIsProcessTrustedWithOptions(prompt=true)
// and CGRequestScreenCaptureAccess() so macOS shows its permission prompts.
// It returns whether each grant is (now) in place; the caller prints the
// perms= line and picks the exit code. Both prompts only ADD the app to the
// System Settings lists - a person still flips the toggles, and Screen
// Recording is picked up only by a later process (help-macos.txt :109-112),
// so a fresh grant commonly still reads missing here.
func RequestPerms() (accessibility, screen bool, err error) {
	if err := initFFI(); err != nil {
		return false, false, err
	}
	accessibility = requestAccessibility()
	screen = cgRequestScreenCaptureAccess()
	return accessibility, screen, nil
}

// requestAccessibility calls AXIsProcessTrustedWithOptions with
// {kAXTrustedCheckOptionPrompt: true} so macOS shows the Accessibility
// prompt, returning the current trust state. The option key's CFString
// value equals its own name ("AXTrustedCheckOptionPrompt"), recreated the
// same way session.go recreates the private SkyLight keys. If the CF
// constants could not be resolved, it falls back to the no-prompt query.
func requestAccessibility() bool {
	initWindows() // NSNumber/NSDictionary + the CFString key
	key := cfString("AXTrustedCheckOptionPrompt")
	defer cfRelease(key)
	// @{AXTrustedCheckOptionPrompt: @YES} as a CFDictionary-bridged
	// NSDictionary; dictionaryWithObject:forKey: takes (object, key).
	opts := objc.ID(nsDictionaryClass).Send(selDictWithObjectKey, cfBoolean(true), objc.ID(key))
	if opts == 0 {
		return axIsProcessTrusted()
	}
	return axIsProcessTrustedWithOptions(uintptr(opts))
}
