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
#   --claude   also install Claude Code (arm64 phones): the official
#              linux-arm64 build, run through Termux's glibc loader
#              (glibc-runner), as `claude`; `claude-update` updates it
#
# Environment: GECKO_URL (a gecko binary to download instead),
# GECKO_REPO (default eternaleclipse/gecko), GECKO_REF (git ref for --source).
set -eu

REPO=${GECKO_REPO:-eternaleclipse/gecko}
APP=0
SOURCE=0
CLAUDE=0
for a in "$@"; do
	case $a in
	--app) APP=1 ;;
	--source) SOURCE=1 ;;
	--claude) CLAUDE=1 ;;
	-h | --help) sed -n '2,22p' "$0" 2>/dev/null || true; exit 0 ;;
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

# Claude Code: its native build is for glibc Linux, and Android has no
# /lib/ld-linux-aarch64.so.1, so its own installer can't start it. Termux's
# glibc-runner can (grun). On x86_64 the build stops on Android's seccomp
# filter (it calls access(2) itself), so there it's proot-distro instead.
claude_install() {
	if [ "$ARCH" != arm64 ]; then
		say "Claude Code skipped: its x86_64 Linux build makes a system call Android doesn't allow (access). Use proot-distro: see Claude Code in Termux in docs/android.md"
		return 0
	fi
	say "installing glibc-runner, jq and ripgrep for Claude Code"
	pkg install -y glibc-repo </dev/null >/dev/null || return 1
	pkg install -y glibc-runner jq ripgrep </dev/null >/dev/null || return 1
	cat >"$BIN/claude-update" <<'UPDATE'
#!/data/data/com.termux/files/usr/bin/bash
# claude-update: fetches the official linux-arm64 Claude Code build, checks
# its checksum, and keeps it in ~/.local/share/claude-glibc, where `claude`
# runs it through Termux's glibc loader (glibc-runner). Installed by Gecko's
# Termux installer (install.sh --claude).
set -euo pipefail
B=https://downloads.claude.ai/claude-code-releases
D=$HOME/.local/share/claude-glibc
case $(uname -m) in
aarch64 | arm64) P=linux-arm64 ;;
*) echo "claude-update: only for arm64 phones; this is $(uname -m)" >&2; exit 1 ;;
esac
mkdir -p "$D"
v=$(curl -fsSL "$B/latest")
[[ $v =~ ^[0-9]+\.[0-9]+\.[0-9]+ ]] || { echo "claude-update: unexpected version: $v" >&2; exit 1; }
sum=$(curl -fsSL "$B/$v/manifest.json" | jq -r --arg p "$P" '.platforms[$p].checksum')
[[ $sum =~ ^[0-9a-f]{64}$ ]] || { echo "claude-update: no checksum for $P in $v" >&2; exit 1; }
if [ -x "$D/claude" ] && [ "$(sha256sum "$D/claude" | cut -d' ' -f1)" = "$sum" ]; then
	echo "Claude Code $v is current"
	exit 0
fi
curl -fL --retry 2 -o "$D/claude.new" "$B/$v/$P/claude"
[ "$(sha256sum "$D/claude.new" | cut -d' ' -f1)" = "$sum" ] || { rm -f "$D/claude.new"; echo "claude-update: checksum mismatch" >&2; exit 1; }
chmod 755 "$D/claude.new" && mv -f "$D/claude.new" "$D/claude"
echo "Installed Claude Code $v"
UPDATE
	cat >"$BIN/claude" <<'WRAPPER'
#!/data/data/com.termux/files/usr/bin/sh
# Claude Code's linux-arm64 build through Termux's glibc loader. Update it
# with claude-update (its own updater can't run here). Its searches use
# Termux's ripgrep. Installed by Gecko's Termux installer (install.sh --claude).
export DISABLE_AUTOUPDATER=1 USE_BUILTIN_RIPGREP=0
exec grun "$HOME/.local/share/claude-glibc/claude" "$@"
WRAPPER
	chmod 755 "$BIN/claude-update" "$BIN/claude"
	"$BIN/claude-update" </dev/null || return 1
	v=$("$BIN/claude" --version </dev/null 2>&1) || { echo "$v" >&2; return 1; }
	say "installed $v: run claude (claude-update updates it)"
}

if [ $CLAUDE = 1 ]; then
	claude_install || die "Claude Code didn't install (Gecko did); run this again with --claude to retry"
fi

cat <<'MSG'

Start it: run `gecko` (it prints the link to open), or in the Gecko app tap
Termux under This phone: it becomes one of This phone's machines.
Worth having too: pkg install git tmux nodejs openssh
To keep it running in the background: termux-wake-lock
MSG
