package syntax

import (
	"strings"

	"github.com/kang-sw/gotto-hando/internal/ir"
)

// parseSelector parses a WINDOW SELECTOR (help.txt:286-297): id:<n>,
// pid:<n>, app:<name> or a bare title substring. With the r flag a bare
// title is an RE2 regex. The value keeps its raw text; id/pid numeric
// validity and regex compilation are checked in validation.
func parseSelector(s string, regex bool) ir.Selector {
	switch {
	case strings.HasPrefix(s, "id:"):
		return ir.Selector{Kind: "id", Value: s[len("id:"):]}
	case strings.HasPrefix(s, "pid:"):
		return ir.Selector{Kind: "pid", Value: s[len("pid:"):]}
	case strings.HasPrefix(s, "app:"):
		return ir.Selector{Kind: "app", Value: s[len("app:"):]}
	default:
		return ir.Selector{Kind: "title", Value: s, Regex: regex}
	}
}
