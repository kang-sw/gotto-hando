//go:build darwin

package darwin

import (
	"context"
	"os/exec"
	"strings"
)

// buildOpenArgv resolves open's argv (help-macos.txt CAVEAT :247-248, ticket
// Decision): a bare word with no "/" is treated as a bundle name and run as
// `open -a <name>` (case-insensitive); anything containing "/" is treated
// as a path and run as `open <path>` directly.
func buildOpenArgv(target string) []string {
	if strings.Contains(target, "/") {
		return []string{"/usr/bin/open", target}
	}
	return []string{"/usr/bin/open", "-a", target}
}

// Open launches an app or path via /usr/bin/open (help.txt open :363-368).
// A non-nil error (nonzero exit or spawn failure - /usr/bin/open itself
// always exists on macOS, so this is effectively "wrong name"/"bad path")
// is returned as-is; the engine's existing KindOpen case maps it to E_EXEC
// (help-macos.txt "A wrong name is E_EXEC").
func (b *Backend) Open(ctx context.Context, target string) error {
	argv := buildOpenArgv(target)
	return exec.CommandContext(ctx, argv[0], argv[1:]...).Run()
}
