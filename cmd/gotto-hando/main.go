// Command gotto-hando drives a macOS/Windows desktop from the shell, one
// command per line. The complete, normative behavior is assets/help.txt
// (assets.Help / HelpMacos / HelpWindows / HelpRemote); this program must
// match it exactly.
package main

import "os"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
