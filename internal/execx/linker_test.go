package execx

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLinkerArgv(t *testing.T) {
	files := t.TempDir()
	bin := filepath.Join(files, "usr", "bin")
	os.MkdirAll(bin, 0o755)
	write := func(name, body string) string {
		p := filepath.Join(bin, name)
		os.WriteFile(p, []byte(body), 0o755)
		return p
	}
	bash := write("bash", "\x7fELF\x02\x01\x01")
	coreutils := write("coreutils", "\x7fELF\x02\x01\x01")
	ls := filepath.Join(bin, "ls")
	os.Symlink("coreutils", ls)
	script := write("tool", "#!"+bash+" -e\necho hi\n")
	envScript := write("agent", "#!/usr/bin/env bash\n")
	sysScript := write("sys", "#!/system/bin/sh\n")
	write("env", "\x7fELF\x02\x01\x01")
	text := write("notes", "hello\n")
	const l = "/system/bin/linker64"
	// As on Android: no /usr or /bin.
	exists := func(p string) bool {
		_, err := os.Stat(p)
		return err == nil && (strings.HasPrefix(p, files) || strings.HasPrefix(p, "/system/"))
	}

	for _, c := range []struct {
		path, prog string
		args, want []string
	}{
		{bash, l, []string{"-bash", "-l"}, []string{"-bash", bash, "-l"}},
		// A multi-call program keeps the name it was started by.
		{ls, l, []string{"ls", "-la"}, []string{"ls", ls, "-la"}},
		{coreutils, l, nil, []string{coreutils, coreutils}},
		{script, l, []string{"tool", "x"}, []string{"tool", bash, "-e", script, "x"}},
		{envScript, l, []string{"agent"}, []string{"agent", filepath.Join(bin, "env"), "bash", envScript}},
		{sysScript, "/system/bin/sh", []string{"sys"}, []string{"/system/bin/sh", sysScript}},
		{text, "", []string{"notes"}, nil},
		{"/system/bin/getprop", "", []string{"getprop"}, nil},
	} {
		prog, got := linkerArgv(l, files, c.path, c.args, readHeader, exists)
		if prog != c.prog || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s %q: got %s %q, want %s %q", filepath.Base(c.path), c.args, prog, got, c.prog, c.want)
		}
	}
}

func TestWithoutEnv(t *testing.T) {
	got := withoutEnv([]string{"A=1", "TERMUX_EXEC__PROC_SELF_EXE=/x", "TERMUX_EXEC__PROC_SELF_EXEC=2"}, selfExeEnv)
	if want := []string{"A=1", "TERMUX_EXEC__PROC_SELF_EXEC=2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q", got)
	}
}
