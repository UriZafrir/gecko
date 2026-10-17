#!/bin/sh
# Device checks for Gecko in Termux that instrumentation can't do (Android
# kills the app when a permission is revoked). Needs adb, the app installed
# and Termux with Gecko set up, as for TermuxTest.
#
#   android/termux-check.sh
set -eu
R=io.github.geckoterm.android
P=com.termux.permission.RUN_COMMAND
seen() { # text: whether the screen shows it
	adb shell uiautomator dump /sdcard/ui.xml >/dev/null </dev/null
	adb shell cat /sdcard/ui.xml </dev/null | grep -q "$1"
}
wait_for() {
	for _ in $(seq 20); do seen "$1" && return 0; sleep 1; done
	echo "FAIL: no \"$1\" on screen"; exit 1
}
adb shell pm revoke $R $P </dev/null
# The machine list, then its Termux row.
adb shell am start -n $R/.ConnectActivity </dev/null >/dev/null
wait_for 'text="Termux"'
b=$(adb shell cat /sdcard/ui.xml </dev/null | tr '>' '\n' | grep -m1 'text="Termux"' | grep -o 'bounds="[^"]*"' | grep -o '[0-9][0-9]*' | tr '\n' ' ')
set -- $b
adb shell input tap $((($1 + $3) / 2)) $((($2 + $4) / 2)) </dev/null
wait_for "Let Gecko start commands in Termux"
echo "ok: without the permission the setup asks for it"
adb shell pm grant $R $P </dev/null
echo "ok: granted again"
adb shell am force-stop $R </dev/null
