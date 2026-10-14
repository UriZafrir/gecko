#!/bin/sh
# Builds installation packages into dist/packages:
#   linux    gecko_<ver>_<arch>.deb, gecko-<ver>.<arch>.rpm (amd64, arm64)
#   macos    Gecko-<ver>-macos.zip, a Gecko.app for Apple silicon and Intel
#   windows  Gecko-<ver>-setup-<arch>.exe (amd64, arm64)
#
#   packaging/build.sh [linux] [macos] [windows]    (default: all three)
#
# Run through `make packages`, which builds the web client first and sets
# VERSION, PKG_VERSION and LDFLAGS. Linux packages use nfpm (fetched with
# `go run` when it isn't installed); Windows needs makensis (apt install
# nsis, brew install makensis).
set -eu
cd "$(dirname "$0")/.."

GO=${GO:-go}
VERSION=${VERSION:-dev}
PKG_VERSION=${PKG_VERSION:-0.0.0}
LDFLAGS=${LDFLAGS:--s -w -X main.version=$VERSION}
NFPM_VERSION=v2.41.0
OUT=dist/packages
BUILD=dist/build
mkdir -p "$OUT" "$BUILD"

build() { # os arch output
	echo "building gecko for $1/$2"
	CGO_ENABLED=0 GOOS=$1 GOARCH=$2 "$GO" build -trimpath -ldflags "$LDFLAGS" -o "$3" ./cmd/gecko
}

run_nfpm() {
	if command -v nfpm >/dev/null; then
		nfpm "$@"
	else
		"$GO" run "github.com/goreleaser/nfpm/v2/cmd/nfpm@$NFPM_VERSION" "$@"
	fi
}

linux() {
	# nfpm.yaml takes the files from dist/build/linux, one arch at a time.
	dir=$BUILD/linux
	mkdir -p "$dir"
	for arch in amd64 arm64; do
		build linux $arch "$dir/gecko"
		sed 's|@BIN@|/usr/bin/gecko|' packaging/linux/gecko.desktop >"$dir/gecko.desktop"
		for fmt in deb rpm; do
			ARCH=$arch PKG_VERSION=$PKG_VERSION run_nfpm package -f packaging/linux/nfpm.yaml -p $fmt -t "$OUT"
		done
	done
}

macos() {
	app=$BUILD/macos/Gecko.app
	rm -rf "$BUILD/macos"
	mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
	build darwin arm64 "$BUILD/macos/gecko-arm64"
	build darwin amd64 "$BUILD/macos/gecko-amd64"
	"$GO" run packaging/macos/lipo.go "$app/Contents/MacOS/gecko" "$BUILD/macos/gecko-arm64" "$BUILD/macos/gecko-amd64"
	sed "s|@VERSION@|$PKG_VERSION|" packaging/macos/Info.plist >"$app/Contents/Info.plist"
	cp packaging/macos/gecko.icns "$app/Contents/Resources/gecko.icns"
	zip="$PWD/$OUT/Gecko-$PKG_VERSION-macos.zip"
	rm -f "$zip"
	(cd "$BUILD/macos" && zip -qry "$zip" Gecko.app)
	echo "created package: $OUT/Gecko-$PKG_VERSION-macos.zip"
}

windows() {
	makensis=${MAKENSIS:-makensis}
	if ! command -v "$makensis" >/dev/null; then
		echo "windows: makensis not found; install NSIS (apt install nsis, brew install makensis)" >&2
		exit 1
	fi
	# Windows wants a.b.c.d with numbers only: 1.2.3-rc1 becomes 1.2.3.0.
	vi=$(echo "$PKG_VERSION" | sed 's/[^0-9.].*//')
	while [ "$(echo "$vi" | tr -cd . | wc -c)" -lt 3 ]; do vi=$vi.0; done
	for arch in amd64 arm64; do
		exe=$PWD/$BUILD/windows-$arch/gecko.exe
		build windows $arch "$exe"
		"$makensis" -V2 -DVERSION="$PKG_VERSION" -DVIVERSION="$vi" -DEXE="$exe" \
			-DOUTFILE="$PWD/$OUT/Gecko-$PKG_VERSION-setup-$arch.exe" packaging/windows/gecko.nsi
		echo "created package: $OUT/Gecko-$PKG_VERSION-setup-$arch.exe"
	done
}

[ $# -gt 0 ] || set -- linux macos windows
for target; do
	case $target in
	linux | macos | windows) $target ;;
	*) echo "unknown target: $target (linux, macos, windows)" >&2 && exit 2 ;;
	esac
done
