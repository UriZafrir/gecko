#!/bin/sh
# Installs Gecko in Termux (https://termux.dev), no root needed:
#
#   curl -fsSL https://raw.githubusercontent.com/eternaleclipse/gecko/main/packaging/termux/install.sh | sh
#
# It puts gecko in $PREFIX/bin: the Linux build for the phone's CPU from the
# latest GitHub release, or, with --source (or when a release has none), a
# build from source with Termux's Go and Node. Run it again to update. Any
# Termux build works, Google Play's too: that one starts programs through
# Android's linker, and the release builds are static PIEs, which it can
# start (older releases' builds can't be).
#
# Options (after `sh -s --` when piped):
#   --app      also let the Gecko Android app start Gecko here: sets
#              allow-external-apps=true in ~/.termux/termux.properties
#   --source   build from source instead of downloading
#
# Environment: GECKO_URL (a gecko binary to download instead),
# GECKO_REPO (default eternaleclipse/gecko), GECKO_REF (git ref for --source).
set -eu

REPO=${GECKO_REPO:-eternaleclipse/gecko}
APP=0
SOURCE=0
for a in "$@"; do
	case $a in
	--app) APP=1 ;;
	--source) SOURCE=1 ;;
	-h | --help) sed -n '2,19p' "$0" 2>/dev/null || true; exit 0 ;;
	*) echo "gecko install: unknown option $a" >&2; exit 2 ;;
	esac
done

say() { printf '\033[1;32m%s\033[0m\n' "$*"; }
die() { printf 'gecko install: %s\n' "$*" >&2; exit 1; }

case ${PREFIX:-} in
*/com.termux/*) ;;
*) die "run this inside Termux (its \$PREFIX is not set here)" ;;
esac
BIN=$PREFIX/bin
TMP=${TMPDIR:-$PREFIX/tmp}/gecko-install.$$
mkdir -p "$TMP"
trap 'rm -rf "$TMP"' EXIT INT TERM

case $(uname -m) in
aarch64 | arm64) ARCH=arm64 ;;
x86_64) ARCH=amd64 ;;
*) die "Gecko needs a 64-bit phone (arm64 or x86_64); this is $(uname -m)" ;;
esac

need() { # command package
	command -v "$1" >/dev/null 2>&1 || { say "installing $2"; pkg install -y "$2" </dev/null; }
}

download() { # url file
	curl -fL --retry 2 --progress-bar -o "$2" "$1"
}

# A release asset's URL by the end of its name, from GitHub's API.
asset() {
	curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null |
		grep -o '"browser_download_url": *"[^"]*' | sed 's/.*"//' | grep -- "$1\$" | head -n 1
}

fetch() {
	need curl curl
	if [ -n "${GECKO_URL:-}" ]; then
		say "downloading $GECKO_URL"
		download "$GECKO_URL" "$TMP/gecko"
		return
	fi
	url=$(asset "gecko-linux-$ARCH" || true)
	if [ -n "$url" ]; then
		say "downloading $url"
		download "$url" "$TMP/gecko"
		return
	fi
	# Older releases have only packages: take gecko out of the .deb.
	url=$(asset "_$ARCH.deb" || true)
	[ -n "$url" ] || return 1
	say "downloading $url"
	download "$url" "$TMP/gecko.deb"
	(cd "$TMP" && dpkg-deb -x gecko.deb deb) || return 1
	cp "$TMP/deb/usr/bin/gecko" "$TMP/gecko"
}

build() {
	say "building Gecko from source (needs about 1 GB and a few minutes)"
	need git git
	need go golang
	need node nodejs
	git clone --depth 1 ${GECKO_REF:+--branch "$GECKO_REF"} "https://github.com/$REPO.git" "$TMP/src"
	(cd "$TMP/src/web" && npm install --no-audit --no-fund && node build.mjs) </dev/null
	v=$(cd "$TMP/src" && git describe --tags --always 2>/dev/null || echo dev)
	(cd "$TMP/src" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$v" -o "$TMP/gecko" ./cmd/gecko) </dev/null
}

if [ $SOURCE = 1 ]; then
	build
elif ! fetch; then
	say "no download for linux/$ARCH in the latest release"
	build
fi

chmod 755 "$TMP/gecko"
"$TMP/gecko" version </dev/null >/dev/null 2>&1 || die "the downloaded gecko doesn't run on this phone"
mv -f "$TMP/gecko" "$BIN/gecko"
say "installed $("$BIN/gecko" version </dev/null)"

# A daemon from an older version keeps its sessions and picks this one up.
"$BIN/gecko" upgrade --if-running </dev/null >/dev/null 2>&1 || true

if [ $APP = 1 ]; then
	props=$HOME/.termux/termux.properties
	mkdir -p "$HOME/.termux"
	if grep -qs '^[[:space:]]*allow-external-apps[[:space:]]*=[[:space:]]*true' "$props"; then
		:
	else
		[ -f "$props" ] && sed -i '/^[[:space:]]*allow-external-apps[[:space:]]*=/d' "$props"
		echo "allow-external-apps=true" >>"$props"
		say "the Gecko app may now start Gecko in Termux (allow-external-apps=true in $props)"
	fi
	command -v termux-reload-settings >/dev/null 2>&1 && termux-reload-settings || true
fi

cat <<'MSG'

Start it: run `gecko` (it prints the link to open), or in the Gecko app tap
Termux under This phone: it becomes one of This phone's machines.
Worth having too: pkg install git tmux nodejs openssh
To keep it running in the background: termux-wake-lock
MSG
