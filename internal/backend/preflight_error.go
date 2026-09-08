package backend

import "github.com/kang-sw/gotto-hando/internal/output"

// PreflightError is returned by Backend.Preflight to carry the exact
// output.ErrorCode the abort should report (help.txt ERROR CODES,
// :680-706). engine.Run unwraps it with errors.As; a plain (non-coded)
// error from Preflight falls back to E_UNKNOWN. internal/output never
// imports internal/backend (no import cycle: output has no
// project-internal imports), so this type is safe to define here.
type PreflightError struct {
	Code output.ErrorCode
	Msg  string
}

func (e *PreflightError) Error() string { return e.Msg }
