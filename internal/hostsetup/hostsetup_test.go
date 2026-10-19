package hostsetup

import (
	"archive/zip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestShQuote(t *testing.T) {
	if got := shQuote("it's"); got != `'it'\''s'` {
		t.Fatal(got)
	}
}

func TestPlatform(t *testing.T) {
	if platform("Linux", "x86_64") != "linux/amd64" || platform("Darwin", "arm64") != "darwin/arm64" || platform("SunOS", "i86pc") != "" {
		t.Fatal("platform mapping")
	}
}

func TestExplain(t *testing.T) {
	if !strings.Contains(Explain("me@x: Permission denied (publickey).", "x"), "key login") {
		t.Fatal("permission hint")
	}
	if !strings.Contains(Explain("sh: 1: exec: gecko: not found", "box"), "gecko host install box") {
		t.Fatal("install hint")
	}
}

func TestKind(t *testing.T) {
	for out, want := range map[string]string{
		"demo@ubunty: Permission denied (publickey,password).":                                                           NeedsPassword,
		"me@box: Permission denied (publickey,keyboard-interactive).":                                                    NeedsPassword,
		"me@box: Permission denied (publickey).":                                                                         "",
		"No ED25519 host key is known for ubunty and you have requested strict checking.\nHost key verification failed.": HostKeyUnknown,
		"@@@\n@    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @\n":                                             HostKeyChanged,
		"ssh: connect to host box port 22: Connection refused":                                                           "",
	} {
		if got := Kind(out); got != want {
			t.Errorf("Kind(%q) = %q, want %q", out, got, want)
		}
	}
}

func TestAskpassSecret(t *testing.T) {
	env := askpassEnv("hunter2")
	id := ""
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "GECKO_ASKPASS="); ok {
			id = v
		}
		if strings.Contains(e, "hunter2") {
			t.Errorf("secret in the environment: %s", e)
		}
	}
	if s, ok := AskpassSecret(id, "demo@box's password: "); !ok || s != "hunter2" {
		t.Errorf("password prompt: %q %v", s, ok)
	}
	if _, ok := AskpassSecret(id, "Are you sure you want to continue connecting (yes/no)? "); ok {
		t.Error("answered a host key question")
	}
	if _, ok := AskpassSecret("other", "Password: "); ok {
		t.Error("unknown id answered")
	}
	forgetSecret("hunter2")
	if _, ok := AskpassSecret(id, "Password: "); ok {
		t.Error("still known after forgetting")
	}
}

func TestFromAPK(t *testing.T) {
	dir := t.TempDir()
	apk := filepath.Join(dir, "base.apk")
	f, _ := os.Create(apk)
	zw := zip.NewWriter(f)
	for name, body := range map[string]string{"lib/x86_64/libgecko.so": "amd64 gecko", "lib/arm64-v8a/libgecko.so": "arm64 gecko", "classes.dex": "x"} {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	f.Close()
	for arch, want := range map[string]string{"amd64": "amd64 gecko", "arm64": "arm64 gecko"} {
		p, err := fromAPK(apk, arch, filepath.Join(dir, "dist"))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(p)
		st, _ := os.Stat(p)
		if string(b) != want || st.Mode().Perm()&0o100 == 0 || filepath.Base(p) != "gecko-linux-"+arch {
			t.Errorf("%s: %q %v %s", arch, b, st.Mode(), p)
		}
	}
	if _, err := fromAPK(apk, "riscv64", dir); err == nil {
		t.Error("riscv64")
	}
}

// installDir runs on the machine: it picks the gecko `gecko bridge` runs
// there when this login may replace it, else ~/.local/bin, and lists the
// other geckos it leaves.
func TestInstallDir(t *testing.T) {
	pick := func(home string) (dir string, others []string) {
		t.Helper()
		cmd := exec.Command("sh", "-c", installDir)
		cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if v, ok := strings.CutPrefix(l, "GECKO_DIR: "); ok {
				dir = v
			} else if v, ok := strings.CutPrefix(l, "GECKO_OTHER: "); ok {
				others = append(others, v)
			}
		}
		return
	}
	put := func(p string, mode os.FileMode) {
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte("#!/bin/sh\necho gecko v1\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
	// Nothing there yet: ~/.local/bin, made.
	home := t.TempDir()
	if dir, _ := pick(home); dir != home+"/.local/bin" {
		t.Fatalf("no gecko: %q", dir)
	}
	if st, err := os.Stat(home + "/.local/bin"); err != nil || !st.IsDir() {
		t.Fatal("~/.local/bin not made")
	}
	// One in ~/go/bin (go install): that one is replaced, not shadowed.
	home = t.TempDir()
	put(home+"/go/bin/gecko", 0o755)
	if dir, others := pick(home); dir != home+"/go/bin" || len(others) != 0 {
		t.Fatalf("gecko in ~/go/bin: %q %q", dir, others)
	}
	// One in a folder this login can't write: ~/.local/bin, and the other
	// one is named.
	home = t.TempDir()
	put(home+"/bin/gecko", 0o755)
	os.Chmod(home+"/bin", 0o555)
	defer os.Chmod(home+"/bin", 0o755)
	if os.Getuid() != 0 {
		if dir, others := pick(home); dir != home+"/.local/bin" || len(others) != 1 || others[0] != home+"/bin/gecko" {
			t.Fatalf("gecko in a read-only folder: %q %q", dir, others)
		}
	}
}
