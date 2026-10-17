package execx

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gecko-term/gecko/internal/config"
)

// selfExeEnv tells a program started through the linker its own path
// (/proc/self/exe is the linker); Termux's packages read it.
const selfExeEnv = "TERMUX_EXEC__PROC_SELF_EXE"

// ThroughLinker makes cmd start through Android's linker when Gecko itself
// was started that way (Termux from Google Play, see config.Linker) and
// cmd's program is one of Termux's files, which Android doesn't let Termux
// execute directly: as termux-exec does for Termux's own programs, it runs
// /system/bin/linker64 <program> <args>, and a script's interpreter rather
// than the script. Otherwise it leaves cmd alone. Call it after cmd.Path,
// cmd.Args and cmd.Env are set.
func ThroughLinker(cmd *exec.Cmd) {
	l := config.Linker()
	files := termuxFiles()
	if l == "" || files == "" || cmd.Path == "" || !filepath.IsAbs(cmd.Path) {
		return
	}
	path, argv := linkerArgv(l, files, cmd.Path, cmd.Args, readHeader, exists)
	if argv == nil {
		return
	}
	if path == l {
		env := cmd.Env
		if env == nil {
			env = os.Environ()
		}
		cmd.Env = append(withoutEnv(env, selfExeEnv), selfExeEnv+"="+argv[1])
	}
	cmd.Path, cmd.Args = path, argv
}

// termuxFiles is Termux's files directory, the parent of $PREFIX.
func termuxFiles() string {
	p := config.TermuxPrefix()
	if p == "" {
		return ""
	}
	return filepath.Dir(p)
}

// linkerArgv is the program and argv that start path with args (args[0] is
// the program's name): the linker for Termux's programs, and a script's
// interpreter for Termux's scripts (through the linker when it is one of
// Termux's programs too). It returns a nil argv for anything else.
func linkerArgv(linker, files, path string, args []string, header func(string) []byte, exists func(string) bool) (string, []string) {
	real := path
	if r, err := filepath.EvalSymlinks(path); err == nil {
		real = r
	}
	if !strings.HasPrefix(real, files+"/") {
		return "", nil
	}
	name := path
	if len(args) > 0 {
		name = args[0]
	}
	var rest []string
	if len(args) > 1 {
		rest = args[1:]
	}
	h := header(path)
	switch {
	case bytes.HasPrefix(h, []byte("\x7fELF")):
		// The linker loads the file it is given, keeping symlinks: Termux's
		// multi-call programs tell by that name what to be.
		return linker, append([]string{name, path}, rest...)
	case bytes.HasPrefix(h, []byte("#!")):
		line, _, _ := bytes.Cut(h[2:], []byte("\n"))
		interp, arg, _ := strings.Cut(strings.TrimSpace(string(line)), " ")
		arg = strings.TrimSpace(arg)
		if interp == "" {
			return "", nil
		}
		// #!/usr/bin/env and the like, which Termux doesn't have.
		if mapped := termuxShebang([]string{path}, filepath.Join(files, "usr"), exists); len(mapped) > 1 {
			interp = mapped[0]
		}
		r := interp
		if x, err := filepath.EvalSymlinks(interp); err == nil {
			r = x
		}
		var out []string
		prog := linker
		if strings.HasPrefix(r, files+"/") {
			out = []string{name, interp}
		} else {
			// A system interpreter (/system/bin/sh) starts directly; the
			// script, one of Termux's files, can't be.
			prog, out = interp, []string{interp}
		}
		if arg != "" {
			out = append(out, arg)
		}
		return prog, append(append(out, path), rest...)
	}
	return "", nil
}

func readHeader(path string) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	b, _ := bufio.NewReader(f).Peek(256)
	return b
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func withoutEnv(env []string, key string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, key+"=") {
			out = append(out, kv)
		}
	}
	return out
}
