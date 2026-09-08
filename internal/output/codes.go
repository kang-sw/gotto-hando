// Package output implements the plain and JSONL result/done/abort
// formatters and the abort exit-code mapping (CONCEPT.md ch.12: "internal/
// output: plain/jsonl 라이터... exit code 계산"). See assets/help.txt
// OUTPUT (`:543-594`), JSONL (`:595-624`), ERROR CODES (`:680-707`) and
// EXIT CODES (`:708-723`) - the single source of truth this package
// formats against.
package output

// ErrorCode is one of the canonical E_* error codes (ERROR CODES,
// help.txt:680-706), printed as "(<E_CODE>)" on plain err/abort lines and
// as the "code" field in JSONL.
type ErrorCode string

const (
	ESyntax     ErrorCode = "E_SYNTAX"
	EValidate   ErrorCode = "E_VALIDATE"
	EConnect    ErrorCode = "E_CONNECT"
	EPermission ErrorCode = "E_PERMISSION"
	ESession    ErrorCode = "E_SESSION"
	EBounds     ErrorCode = "E_BOUNDS"
	ENoWindow   ErrorCode = "E_NOWINDOW"
	EInput      ErrorCode = "E_INPUT"
	ECapture    ErrorCode = "E_CAPTURE"
	EClipboard  ErrorCode = "E_CLIPBOARD"
	EExec       ErrorCode = "E_EXEC"
	ETimeout    ErrorCode = "E_TIMEOUT"
	EUnknown    ErrorCode = "E_UNKNOWN"
)

// Process exit codes (EXIT CODES, help.txt:708-723).
const (
	ExitOK             = 0 // every line ok
	ExitRuntimeFailure = 1 // one or more lines failed at run time
	ExitValidation     = 2 // syntax/validation error, -f mixed with lines, unsupported option
	ExitConnect        = 3 // destination failure
	ExitPreflight      = 4 // preflight failed (permission, session, bounds, ...)
	ExitStateUnknown   = 5 // ssh connection lost mid-run
)

// AbortExit maps an abort's error code to its process exit code, per the
// ticket Phase 1 text and EXIT CODES line 712-714 ("an option the
// platform does not support" folds into exit 2 / E_VALIDATE):
// E_VALIDATE -> 2, E_CONNECT -> 3, every other code -> 4.
func AbortExit(code ErrorCode) int {
	switch code {
	case EValidate:
		return ExitValidation
	case EConnect:
		return ExitConnect
	default:
		return ExitPreflight
	}
}
