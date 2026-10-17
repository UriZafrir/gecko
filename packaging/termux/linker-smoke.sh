#!/bin/sh
# Starts a Linux build of gecko the way Termux from Google Play does, through
# Android's own linker, on a plain Linux machine of the same CPU (CI):
#
#   packaging/termux/linker-smoke.sh dist/gecko-linux-arm64 arm64
#
# The linker comes from Google's Android 15 emulator image (the runtime
# APEX in its system partition), checked by sha256 and kept in
# $LINKER_CACHE (default ~/.cache/gecko-android-linker). Then it runs
# `linker64 gecko version` and a daemon started that way, and asks it for
# /api/info. Needs curl, unzip, python3 and debugfs (e2fsprogs).
set -eu

GECKO=$(realpath "$1")
ARCH=$2
case $ARCH in
arm64)
	IMG=arm64-v8a-35_r02.zip
	IMG_SHA1=2026a06409db630b56711afdbffb457c1dbaed49
	LINKER_SHA256=3f3047ad0ce58675d3a106b1a5d67840e71f4c1a193f7062ec40ea740f349790
	;;
amd64)
	IMG=x86_64-35_r02.zip
	IMG_SHA1=2d857d170c0d1b827149565da34b3383e5306f7f
	LINKER_SHA256=e4cced5698a0613a266bd3ef4e47683e7673aa8dc3c4c71488ae01d17210c4d4
	;;
*) echo "arm64 or amd64" >&2; exit 2 ;;
esac
CACHE=${LINKER_CACHE:-$HOME/.cache/gecko-android-linker}
LINKER=$CACHE/linker64-$ARCH
HERE=$(dirname "$(realpath "$0")")

fetch() {
	t=$(mktemp -d)
	trap 'rm -rf "$t"' EXIT
	echo "getting Android's linker from $IMG"
	curl -fsSL -o "$t/img.zip" "https://dl.google.com/android/repository/sys-img/android/$IMG"
	echo "$IMG_SHA1  $t/img.zip" | sha1sum -c
	unzip -q -j "$t/img.zip" '*/system.img' -d "$t"
	rm "$t/img.zip"
	python3 -I "$HERE/lpunpack.py" "$t/system.img" system "$t/system.ext4"
	rm "$t/system.img"
	debugfs -R "dump /system/apex/com.android.runtime.apex $t/runtime.apex" "$t/system.ext4" 2>/dev/null
	unzip -q "$t/runtime.apex" apex_payload.img -d "$t"
	debugfs -R "dump /bin/linker64 $t/linker64" "$t/apex_payload.img" 2>/dev/null
	echo "$LINKER_SHA256  $t/linker64" | sha256sum -c
	mkdir -p "$CACHE"
	install -m 755 "$t/linker64" "$LINKER"
}
[ -x "$LINKER" ] && echo "$LINKER_SHA256  $LINKER" | sha256sum -c --quiet || fetch

echo "== linker64 gecko version"
"$LINKER" "$GECKO" version </dev/null | tee /dev/stderr | grep -q "linux/$ARCH"

echo "== linker64 gecko daemon"
export GECKO_HOME
GECKO_HOME=$(mktemp -d)
"$LINKER" "$GECKO" daemon --listen 127.0.0.1:7691 </dev/null >"$GECKO_HOME/daemon.log" 2>&1 &
pid=$!
for _ in $(seq 50); do
	[ -s "$GECKO_HOME/token" ] && curl -fsS -o /dev/null http://127.0.0.1:7691/ 2>/dev/null && break
	sleep 0.2
done
curl -fsS -H "Authorization: Bearer $(cat "$GECKO_HOME/token")" http://127.0.0.1:7691/api/info | tee /dev/stderr | grep -q '"version"'
echo
kill "$pid"
wait "$pid" 2>/dev/null || true
cat "$GECKO_HOME/daemon.log"
echo "ok: Android's linker starts $GECKO"
