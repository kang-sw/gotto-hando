package main

import (
	"bufio"
	"io"
	"os"
	"strings"
)

// collectLines resolves the input line source per SYNOPSIS (help.txt:11,
// :24-30): -f FILE (or stdin via "-") takes priority when given;
// otherwise positional [line ...] args are used, each split on an
// embedded "\n" (help.txt:24-25: "$'a\nb' passes two lines"); otherwise,
// when neither is given and stdin is not a terminal, stdin is read as a
// pipe. It performs no comment/blank-line handling or syntax validation
// (SYNTAX, help.txt:134-176) - that belongs to the Phase 2 parser.
func collectLines(fileFlag string, hasFile bool, positional []string, stdin io.Reader) ([]string, error) {
	if hasFile {
		if fileFlag == "-" {
			return readAllLines(stdin)
		}
		f, err := os.Open(fileFlag)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return readAllLines(f)
	}
	if len(positional) > 0 {
		var lines []string
		for _, p := range positional {
			lines = append(lines, strings.Split(p, "\n")...)
		}
		return lines, nil
	}
	if !isTerminal(stdin) {
		return readAllLines(stdin)
	}
	return nil, nil
}

func readAllLines(r io.Reader) ([]string, error) {
	var lines []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

// isTerminal reports whether r is a character-device *os.File (the
// SYNOPSIS "stdin is a TTY" case, help.txt:29-30). Non-*os.File readers
// (as used in unit tests) are never treated as a terminal.
func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
