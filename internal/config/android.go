package config

import (
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

var android = sync.OnceValue(func() bool {
	if runtime.GOOS == "android" {
		return true
	}
	// A plain linux build runs on Android too (the Android app ships one).
	if runtime.GOOS != "linux" || os.Getenv("ANDROID_ROOT") == "" {
		return false
	}
	_, err := os.Stat("/system/build.prop")
	return err == nil
})

// IsAndroid reports whether Gecko runs on an Android device, inside the
// Gecko Android app.
func IsAndroid() bool { return android() }

var nonName = regexp.MustCompile(`[^a-z0-9]+`)

// androidName names the phone after its model ("pixel-8"): Android's
// host name is just "localhost".
func androidName() string {
	out, err := exec.Command("/system/bin/getprop", "ro.product.model").Output()
	if err != nil {
		return ""
	}
	return hostName(string(out))
}

// hostName turns free text into a name usable in session ids.
func hostName(s string) string {
	return strings.Trim(nonName.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-"), "-")
}
