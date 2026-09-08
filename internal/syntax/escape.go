package syntax

import (
	"strconv"
	"strings"
)

// applyEscapes resolves TEXT ESCAPES (help.txt:275-284) in a txt/paste/clip
// payload: \n -> U+000A, \t -> U+0009, \\ -> backslash, \uXXXX -> that code
// point. Any other backslash sequence is a syntax error. The parser records
// one resolved string; whether \n means a Return key press (txt) or a
// literal newline (paste/clip) is an engine concern (help.txt:104-107).
// rel is the 0-based rune offset within s at which an error occurred.
func applyEscapes(s string) (out string, rel int, msg string) {
	rs := []rune(s)
	var b strings.Builder
	for i := 0; i < len(rs); i++ {
		if rs[i] != '\\' {
			b.WriteRune(rs[i])
			continue
		}
		if i+1 >= len(rs) {
			return "", i, "dangling backslash"
		}
		switch rs[i+1] {
		case 'n':
			b.WriteRune('\n')
			i++
		case 't':
			b.WriteRune('\t')
			i++
		case '\\':
			b.WriteRune('\\')
			i++
		case 'u':
			if i+5 >= len(rs) {
				return "", i, "\\u needs 4 hex digits"
			}
			hex := string(rs[i+2 : i+6])
			v, err := strconv.ParseUint(hex, 16, 32)
			if err != nil {
				return "", i, "\\u needs 4 hex digits"
			}
			b.WriteRune(rune(v))
			i += 5
		default:
			return "", i, "invalid escape \\" + string(rs[i+1])
		}
	}
	return b.String(), 0, ""
}
