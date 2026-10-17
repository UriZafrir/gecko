package execx

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestTermuxShebang(t *testing.T) {
	d := t.TempDir()
	prefix := "/data/data/com.termux/files/usr"
	script := func(name, body string) string {
		p := filepath.Join(d, name)
		os.WriteFile(p, []byte(body), 0o755)
		return p
	}
	have := map[string]bool{prefix + "/bin/env": true, prefix + "/bin/sh": true, prefix + "/bin/bash": true}
	exists := func(p string) bool { return have[p] }
	node := script("claude", "#!/usr/bin/env node\nconsole.log(1)\n")
	sh := script("tool", "#!/bin/sh\necho hi\n")
	bash := script("b", "#! /bin/bash -e\n")
	elf := script("elf", "\x7fELF...")
	own := script("own", "#!"+prefix+"/bin/sh\n")
	odd := script("odd", "#!/opt/thing/run\n")
	for _, c := range []struct {
		argv, want []string
	}{
		{[]string{node, "--help"}, []string{prefix + "/bin/env", "node", node, "--help"}},
		{[]string{sh}, []string{prefix + "/bin/sh", sh}},
		{[]string{bash, "x"}, []string{prefix + "/bin/bash", "-e", bash, "x"}},
		{[]string{elf}, []string{elf}},
		{[]string{own}, []string{own}},
		{[]string{odd}, []string{odd}},
		{[]string{filepath.Join(d, "missing")}, []string{filepath.Join(d, "missing")}},
	} {
		if got := termuxShebang(c.argv, prefix, exists); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%v: got %v, want %v", c.argv, got, c.want)
		}
	}
	// An interpreter that exists where the line says is left alone.
	have["/bin/sh"] = true
	if got := termuxShebang([]string{sh}, prefix, exists); !reflect.DeepEqual(got, []string{sh}) {
		t.Errorf("existing interpreter: %v", got)
	}
	// Outside Termux nothing changes.
	if got := Shebang([]string{node}); !reflect.DeepEqual(got, []string{node}) {
		t.Errorf("off Termux: %v", got)
	}
}
