#!/bin/sh
# Lays out Gecko's Linux file tree in $LINUX_ROOT, as the packages install
# it, from a gecko binary and electron-builder's unpacked Linux app
# ($LINUX_UNPACKED). Used by packaging/build.sh; the rpm spec and PKGBUILDs
# install the same paths.
#
#   /usr/bin/gecko                       the CLI and daemon (static Go)
#   /usr/bin/gecko-desktop               starts the window (gecko open uses it)
#   /usr/lib/gecko-desktop/              the window: Electron and app.asar
#   /usr/lib/systemd/user/gecko.service  daemon as a user service (not enabled)
#   /usr/share/applications/gecko.desktop, icons, docs
#   /usr/share/gecko/apparmor/gecko-desktop-package   lets the window use its
#                                        sandbox on Ubuntu 23.10+ (the .deb
#                                        installs it in /etc/apparmor.d)
set -eu
gecko=$1
root=${LINUX_ROOT:?}
unpacked=${LINUX_UNPACKED:?}
here=$(dirname "$0")

install -Dm755 "$gecko" "$root/usr/bin/gecko"
install -Dm755 "$here/gecko-desktop" "$root/usr/bin/gecko-desktop"
mkdir -p "$root/usr/lib/gecko-desktop"
cp -a "$unpacked/." "$root/usr/lib/gecko-desktop/"
# The AppImage's launcher and gecko copy are not used here.
rm -rf "$root/usr/lib/gecko-desktop/usr" "$root/usr/lib/gecko-desktop/gecko-desktop"
chmod 755 "$root/usr/lib/gecko-desktop/chrome-sandbox"
chmod 644 "$root"/usr/lib/gecko-desktop/*.so*
strip --strip-unneeded "$root/usr/lib/gecko-desktop/libvulkan.so.1" 2>/dev/null || true
for m in gecko gecko-desktop; do
	mkdir -p "$root/usr/share/man/man1"
	gzip -9nc "$here/$m.1" >"$root/usr/share/man/man1/$m.1.gz"
done
install -Dm644 "$here/gecko.service" "$root/usr/lib/systemd/user/gecko.service"
mkdir -p "$root/usr/share/applications"
sed 's|@BIN@|/usr/bin/gecko|' "$here/gecko.desktop" >"$root/usr/share/applications/gecko.desktop"
chmod 644 "$root/usr/share/applications/gecko.desktop"
install -Dm644 "$here/gecko.svg" "$root/usr/share/icons/hicolor/scalable/apps/gecko.svg"
install -Dm644 "$here/gecko-256.png" "$root/usr/share/icons/hicolor/256x256/apps/gecko.png"
install -Dm644 "$here/gecko.metainfo.xml" "$root/usr/share/metainfo/dev.gecko.app.metainfo.xml"
mkdir -p "$root/usr/share/gecko/apparmor"
sed -e 's|@ELECTRON@|/usr/lib/gecko-desktop/gecko-desktop.bin|' -e 's|profile gecko-desktop |profile gecko-desktop-package |' \
	-e 's|local/gecko-desktop>|local/gecko-desktop-package>|' -e 's|Installed by `make desktop-sandbox`.|Installed by the gecko-desktop package.|' \
	"$here/apparmor-gecko-desktop" >"$root/usr/share/gecko/apparmor/gecko-desktop-package"
install -Dm644 "$here/../../docs/desktop-packages.md" "$root/usr/share/doc/gecko/desktop-packages.md"
install -Dm644 "$here/../../docs/linux-packages.md" "$root/usr/share/doc/gecko/linux-packages.md"
