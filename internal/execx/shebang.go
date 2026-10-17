package execx

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/gecko-term/gecko/internal/config"
)

// Shebang makes a script with a usual #! line (#!/usr/bin/env node,
// #!/bin/sh) start inside Termux, which has no /usr or /bin: the line names
// the interpreter under $PREFIX instead. Termux's shells get this from
// termux-exec, a library preloaded into programs that use the C library;
// Gecko (a static Go program) starts processes without it, e.g. the
// command of a new tab. It returns the argv to start; outside Termux, or
// for anything else, argv itself.
func Shebang(argv []string) []string {
	prefix := config.TermuxPrefix()
	if prefix == "" || len(argv) == 0 {
		return argv
	}
	return termuxShebang(argv, prefix, func(p string) bool { _, err := os.Stat(p); return err == nil })
}

func termuxShebang(argv []string, prefix string, exists func(string) bool) []string {
	f, err := os.Open(argv[0])
	if err != nil {
		return argv
	}
	line, _ := bufio.NewReader(f).ReadString('\n')
	f.Close()
	if !strings.HasPrefix(line, "#!") {
		return argv
	}
	// The kernel splits a #! line into the interpreter and one argument.
	interp, arg, _ := strings.Cut(strings.TrimSpace(line[2:]), " ")
	arg = strings.TrimSpace(arg)
	if interp == "" || exists(interp) {
		return argv
	}
	var dir string
	for _, d := range []string{"/usr/local/bin/", "/usr/bin/", "/bin/", "/usr/sbin/", "/sbin/"} {
		if strings.HasPrefix(interp, d) {
			dir = d
			break
		}
	}
	if dir == "" {
		return argv
	}
	mapped := filepath.Join(prefix, "bin", strings.TrimPrefix(interp, dir))
	if !exists(mapped) {
		return argv
	}
	out := []string{mapped}
	if arg != "" {
		out = append(out, arg)
	}
	return append(out, argv...)
}
