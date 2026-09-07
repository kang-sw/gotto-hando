// Package assets embeds the help texts. They are the normative
// specification of gotto-hando: what --help, --help-macos, --help-windows
// and --help-remote print is exactly these files.
package assets

import _ "embed"

// Help is the text printed by --help: the complete manual for using the
// tool (syntax, commands, execution model, output, limits).
//
//go:embed help.txt
var Help string

// HelpMacos is the text printed by --help-macos: signing identity, TCC
// permissions, GUI sessions and displays on macOS.
//
//go:embed help-macos.txt
var HelpMacos string

// HelpWindows is the text printed by --help-windows: interactive session,
// Task Scheduler, SmartScreen, UAC/UIPI, DPI and exec code pages.
//
//go:embed help-windows.txt
var HelpWindows string

// HelpRemote is the text printed by --help-remote: server, SSH tunnel,
// profiles, security model and the wire protocol.
//
//go:embed help-remote.txt
var HelpRemote string
