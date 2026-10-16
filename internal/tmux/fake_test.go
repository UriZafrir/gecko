package tmux

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeTmux puts a tmux on PATH that answers list-clients and list-windows
// with canned output.
func fakeTmux(t *testing.T, clients, windows string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script")
	}
	dir := t.TempDir()
	for name, out := range map[string]string{"clients": clients, "windows": windows} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(out), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	script := "#!/bin/sh\ncase \"$1\" in\nlist-clients) cat " + dir + "/clients;;\nlist-windows) cat " + dir + "/windows;;\n*) exit 1;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestClientsAndWindowsFake(t *testing.T) {
	fakeTmux(t,
		"/dev/pts/4|~|main|~|4242|~|✳ Fix the tests\n/dev/pts/7|~|other|~|99|~|a |~| title\n",
		"main|~|0|~|claude|~|1|~|1|~|claude|~|4242|~|✳ Fix the tests\nmain|~|1|~|bash|~|0|~|2|~|bash|~|4300|~|ubunty\n")
	cs := Clients()
	if len(cs) != 2 || cs[0] != (Client{TTY: "/dev/pts/4", Session: "main", PanePid: 4242, Title: "✳ Fix the tests"}) {
		t.Fatalf("Clients() = %+v", cs)
	}
	if cs[1].Title != "a |~| title" {
		t.Errorf("title with separator: %q", cs[1].Title)
	}
	if c, ok := ClientOn(cs, "/dev/pts/7"); !ok || c.Session != "other" {
		t.Errorf("ClientOn = %+v %v", c, ok)
	}
	if ClientSession("/dev/pts/4") != "main" || ClientSession("/dev/pts/1") != "" {
		t.Error("ClientSession")
	}
	ws := Windows()
	if len(ws) != 2 || ws[0].PanePid != 4242 || ws[0].Title != "✳ Fix the tests" || !ws[0].Active || ws[1].Panes != 2 || ws[1].PanePid != 4300 {
		t.Fatalf("Windows() = %+v", ws)
	}
}
