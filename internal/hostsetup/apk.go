package hostsetup

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// apkABI is the Android ABI whose libgecko.so is the linux/<arch> build.
var apkABI = map[string]string{"amd64": "x86_64", "arm64": "arm64-v8a"}

// fromAPK copies lib/<abi>/libgecko.so out of the app's APK into dir as
// gecko-linux-<arch>, once per APK, and returns its path.
func fromAPK(apk, goarch, dir string) (string, error) {
	abi := apkABI[goarch]
	if apk == "" || abi == "" {
		return "", fmt.Errorf("the Android app has no Gecko for linux/%s (it carries linux/amd64 and linux/arm64); install Gecko on that machine yourself", goarch)
	}
	ast, err := os.Stat(apk)
	if err != nil {
		return "", err
	}
	dst := filepath.Join(dir, "gecko-linux-"+goarch)
	if st, err := os.Stat(dst); err == nil && !st.ModTime().Before(ast.ModTime()) {
		return dst, nil
	}
	zr, err := zip.OpenReader(apk)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "lib/"+abi+"/libgecko.so" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
		tmp := dst + ".tmp"
		out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			return "", err
		}
		if err := out.Close(); err != nil {
			return "", err
		}
		return dst, os.Rename(tmp, dst)
	}
	return "", fmt.Errorf("the app has no Gecko for linux/%s inside", goarch)
}
