package session

import (
	"strings"
	"testing"
)

// Shells don't inherit an outer Gecko's session variables, but do get the
// GECKO_HOME of the daemon they run in, and the Android app's DNS file.
func TestBaseEnv(t *testing.T) {
	t.Setenv("GECKO_SESSION", "outer")
	t.Setenv("GECKO_HOST", "outer")
	t.Setenv("GECKO_HOME", "/data/data/app/files/gecko")
	t.Setenv("GECKO_RESOLV_CONF", "/data/data/app/files/gecko/resolv.conf")
	t.Setenv("TMUX", "x")
	env := strings.Join(baseEnv(), "\n") + "\n"
	for _, gone := range []string{"GECKO_SESSION=", "GECKO_HOST=", "TMUX="} {
		if strings.Contains(env, gone) {
			t.Errorf("%s kept", gone)
		}
	}
	for _, kept := range []string{"GECKO_HOME=/data/data/app/files/gecko\n", "GECKO_RESOLV_CONF=/data/data/app/files/gecko/resolv.conf\n"} {
		if !strings.Contains(env, kept) {
			t.Errorf("%s dropped", kept)
		}
	}
}
