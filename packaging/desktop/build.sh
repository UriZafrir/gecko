#!/bin/sh
# Builds the Gecko desktop apps into dist/desktop with electron-builder:
#   mac        Gecko-<ver>-mac-universal.dmg (Apple silicon and Intel)
#   appimage   Gecko-<ver>-linux-x86_64.AppImage (ARCHS="x64 arm64" for both)
#   windows    Gecko-<ver>-windows-x64-setup.exe and a portable .zip
#
#   packaging/desktop/build.sh mac|appimage|windows
#
# Run through `make package-mac` etc., which build the web client first and
# set VERSION, PKG_VERSION and LDFLAGS. A dmg needs macOS (hdiutil); the
# Windows setup is built on Windows (or Linux with wine).
set -eu
cd "$(dirname "$0")/../.."

GO=${GO:-go}
VERSION=${VERSION:-dev}
PKG_VERSION=${PKG_VERSION:-0.0.0}
LDFLAGS=${LDFLAGS:--s -w -X main.version=$VERSION}
BINS=dist/build/desktop
mkdir -p "$BINS"
target=${1:?usage: build.sh mac|appimage|windows|linux-dir}

gobuild() { # goos goarch electron-arch
	ext=
	[ "$1" = windows ] && ext=.exe
	echo "building gecko for $1/$2"
	mkdir -p "$BINS/$1-$3"
	CGO_ENABLED=0 GOOS=$1 GOARCH=$2 "$GO" build -trimpath -ldflags "$LDFLAGS" -o "$BINS/$1-$3/gecko$ext" ./cmd/gecko
}

# Build id, as the Android app shows it: short commit (-dirty with changes)
# and the build time; main.js formats it in local time.
commit=$(git rev-parse --short=7 HEAD 2>/dev/null || echo unknown)
[ -z "$(git status --porcelain --untracked-files=no 2>/dev/null)" ] || commit=$commit-dirty
printf '{"commit":"%s","time":"%s","version":"%s"}\n' "$commit" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$VERSION" >desktop/build-id.json
cp packaging/linux/gecko-256.png desktop/icon.png
cp desktop/package.json dist/build/desktop/package.json.orig
trap 'mv -f dist/build/desktop/package.json.orig desktop/package.json' EXIT
# The app's version (Info.plist, setup.exe) is the release tag, not the one in package.json.
node -e 'const f="desktop/package.json",p=require("./"+f);p.version=process.argv[1];require("fs").writeFileSync(f,JSON.stringify(p,null,2)+"\n")' "$PKG_VERSION"

(cd packaging/desktop && npm ci --no-audit --no-fund)
eb() { packaging/desktop/node_modules/.bin/electron-builder --projectDir desktop --config "$PWD/packaging/desktop/electron-builder.config.cjs" "$@"; }

case $target in
mac)
	gobuild darwin arm64 arm64
	gobuild darwin amd64 x64
	eb --mac --universal
	;;
appimage)
	args=
	for a in ${ARCHS:-x64}; do
		case $a in x64) gobuild linux amd64 x64 ;; arm64) gobuild linux arm64 arm64 ;; esac
		args="$args --$a"
	done
	# shellcheck disable=SC2086
	eb --linux AppImage $args
	;;
linux-dir)
	# The unpacked Linux app, for the distro packages (packaging/build.sh).
	args=
	for a in ${ARCHS:-x64}; do
		case $a in x64) gobuild linux amd64 x64 ;; arm64) gobuild linux arm64 arm64 ;; esac
		args="$args --$a"
	done
	# shellcheck disable=SC2086
	eb --linux dir $args
	;;
windows)
	gobuild windows amd64 x64
	eb --win --x64
	;;
*) echo "unknown target: $target (mac, appimage, windows)" >&2 && exit 2 ;;
esac
ls -l dist/desktop/Gecko-* 2>/dev/null || true
