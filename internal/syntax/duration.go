package syntax

import (
	"math"
	"strconv"
	"strings"
)

// parseDurMS parses a DURATIONS value (help.txt:270-273) into milliseconds:
// a number with an optional "ms" or "s" suffix (no suffix = ms). Decimals
// are allowed; negative, NaN and Inf are errors. It returns the rounded
// millisecond value and a message on failure.
func parseDurMS(s string) (int, string) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, "empty duration"
	}
	mult := 1.0
	switch {
	case strings.HasSuffix(t, "ms"):
		t = t[:len(t)-2]
		mult = 1.0
	case strings.HasSuffix(t, "s"):
		t = t[:len(t)-1]
		mult = 1000.0
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
	if err != nil {
		return 0, "invalid duration " + strconv.Quote(s)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, "duration is NaN or Inf"
	}
	if f < 0 {
		return 0, "negative duration"
	}
	return int(math.Round(f * mult)), ""
}

// ParseDurationMS parses a DURATIONS value into milliseconds for callers
// outside the parser (for example the --delay option feeding the IR
// defaults). ok is false on a malformed value.
func ParseDurationMS(s string) (int, bool) {
	ms, msg := parseDurMS(s)
	return ms, msg == ""
}
