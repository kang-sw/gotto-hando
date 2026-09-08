package main

import "fmt"

// optKind distinguishes a boolean flag from an option that consumes the
// following argv token as its value.
type optKind int

const (
	optFlag optKind = iota
	optValue
)

// optDef is one recognized option. tableTracked marks options that have a
// real row in assets/help.txt's OPTIONS section (`:47-116`); the four
// --help* flags are SYNOPSIS-only (`:10-45`) and are excluded from the
// OPTIONS-table drift check (options_drift_test.go). --bridge and
// --version DO have their own OPTIONS rows (help.txt:83, :90) even though
// they are also standalone invocation forms in SYNOPSIS, so they are
// tableTracked here.
type optDef struct {
	long         string
	short        string // "" when the row has no short alias
	kind         optKind
	tableTracked bool
}

// optDefs is the option table built from assets/help.txt OPTIONS
// (`:47-116`), used both by parseArgs and by the drift test that keeps
// this table in sync with help.txt.
var optDefs = []optDef{
	{"--file", "-f", optValue, true},
	{"--keep-going", "-k", optFlag, true},
	{"--delay", "", optValue, true},
	{"--out", "", optValue, true},
	{"--jsonl", "", optFlag, true},
	{"--check", "", optFlag, true},
	{"--ir", "", optFlag, true},
	{"--cap-on-error", "", optFlag, true},
	{"--timeout", "", optValue, true},
	{"--ping", "", optFlag, true},
	{"--quiet", "-q", optFlag, true},
	{"--remote-bin", "", optValue, true},
	{"--inline-captures", "", optFlag, true},
	{"--expect-version", "", optValue, true},
	{"--bridge", "", optFlag, true},
	{"--version", "", optFlag, true},
	{"--request-perms", "", optFlag, true},
	// SYNOPSIS-only standalone flags (help.txt:40-42); no OPTIONS row.
	{"--help", "", optFlag, false},
	{"--help-macos", "", optFlag, false},
	{"--help-windows", "", optFlag, false},
	{"--help-remote", "", optFlag, false},
}

func findOptDef(tok string) *optDef {
	for i := range optDefs {
		d := &optDefs[i]
		if tok == d.long || (d.short != "" && tok == d.short) {
			return d
		}
	}
	return nil
}

// parsedOptions is the flat result of scanning argv against optDefs plus
// dest/positional-lines collection.
type parsedOptions struct {
	Dest  string
	Lines []string

	File    string
	HasFile bool

	Delay      string
	Out        string
	JSONL      bool
	Check      bool
	IR         bool
	CapOnError bool
	Timeout    string
	Ping       bool
	Quiet      bool
	KeepGoing  bool

	RemoteBin    string
	HasRemoteBin bool

	InlineCaptures bool

	ExpectVersion    string
	HasExpectVersion bool

	Bridge       bool
	Version      bool
	RequestPerms bool

	Help, HelpMacos, HelpWindows, HelpRemote bool
}

// parseArgs scans argv per SYNOPSIS (help.txt:10-45,44-45): options start
// with "-"; the first remaining token becomes <dest>, later ones become
// [line ...]. It does not validate dest/line syntax or option values
// (DURATIONS grammar etc.) - callers decide what to do with them.
func parseArgs(args []string) (parsedOptions, error) {
	var o parsedOptions
	destSet := false
	for i := 0; i < len(args); i++ {
		tok := args[i]
		if len(tok) > 0 && tok[0] == '-' && tok != "-" {
			def := findOptDef(tok)
			if def == nil {
				return o, fmt.Errorf("unknown option: %s", tok)
			}
			var val string
			if def.kind == optValue {
				i++
				if i >= len(args) {
					return o, fmt.Errorf("option %s requires a value", tok)
				}
				val = args[i]
			}
			switch def.long {
			case "--file":
				o.File, o.HasFile = val, true
			case "--keep-going":
				o.KeepGoing = true
			case "--delay":
				o.Delay = val
			case "--out":
				o.Out = val
			case "--jsonl":
				o.JSONL = true
			case "--check":
				o.Check = true
			case "--ir":
				o.IR = true
			case "--cap-on-error":
				o.CapOnError = true
			case "--timeout":
				o.Timeout = val
			case "--ping":
				o.Ping = true
			case "--quiet":
				o.Quiet = true
			case "--remote-bin":
				o.RemoteBin, o.HasRemoteBin = val, true
			case "--inline-captures":
				o.InlineCaptures = true
			case "--expect-version":
				o.ExpectVersion, o.HasExpectVersion = val, true
			case "--bridge":
				o.Bridge = true
			case "--version":
				o.Version = true
			case "--request-perms":
				o.RequestPerms = true
			case "--help":
				o.Help = true
			case "--help-macos":
				o.HelpMacos = true
			case "--help-windows":
				o.HelpWindows = true
			case "--help-remote":
				o.HelpRemote = true
			}
			continue
		}
		// Positional token: the first one is <dest>, the rest are
		// [line ...] (help.txt:11).
		if !destSet {
			o.Dest = tok
			destSet = true
			continue
		}
		o.Lines = append(o.Lines, tok)
	}
	return o, nil
}
