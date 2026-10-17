#!/bin/sh
# Launcher inside the Gecko AppImage, run by its AppRun (which adds
# --no-sandbox first when the system blocks unprivileged user namespaces).
#
#   Gecko.AppImage                 the window (finds or starts the daemon)
#   Gecko.AppImage gecko <args>    the bundled gecko command line tool
#   gecko <args>                   the same, through a symlink named gecko
#
# The AppImage's files vanish when it exits, and a daemon outlives the
# window, so gecko always runs from a copy in GECKO_HOME/state/bin.
here=$(dirname "$(readlink -f "$0")")
appdir=${APPDIR:-$here}

auto_nosandbox=
if [ "${1:-}" = --no-sandbox ] && ! unshare -Ur true 2>/dev/null; then
	auto_nosandbox=1
	shift
fi

# The copy: one per build (content checksum), private to the user.
home=${GECKO_HOME:-$HOME/.gecko-terminal}
src=$appdir/usr/bin/gecko
sum=$(cksum <"$src" | cut -d' ' -f1)
bin=$home/state/bin/gecko-appimage-$sum
if [ ! -x "$bin" ]; then
	(umask 077 && mkdir -p "$home/state/bin") &&
		cp "$src" "$bin.$$" && chmod 755 "$bin.$$" && mv -f "$bin.$$" "$bin" || bin=$src
	# Keep the two newest copies.
	ls -t "$home"/state/bin/gecko-appimage-* 2>/dev/null | grep -v '\.[0-9]*$' | tail -n +3 | while read -r old; do
		[ "$old" = "$bin" ] || rm -f "$old"
	done
fi

# What the AppImage runtime added to the environment, which gecko and the
# shells it starts must not inherit.
strip() {
	printf '%s' "$1" | tr ':' '\n' | grep -v "^$appdir" | paste -sd: -
}
clean_env() {
	for v in PATH LD_LIBRARY_PATH XDG_DATA_DIRS GSETTINGS_SCHEMA_DIR; do
		eval "val=\${$v:-}"
		[ -n "$val" ] || continue
		val=$(strip "$val")
		if [ -n "$val" ]; then export "$v=$val"; else unset "$v"; fi
	done
	unset APPDIR APPIMAGE ARGV0 OWD
}

name=$(basename "${ARGV0:-$0}")
if [ "${1:-}" = gecko ] || [ "$name" = gecko ]; then
	[ "${1:-}" = gecko ] && shift
	clean_env
	exec "$bin" "$@"
fi

export GECKO_BIN="$bin"
if [ -n "$auto_nosandbox" ]; then
	export GECKO_SANDBOX_FALLBACK=1
	set -- --no-sandbox "$@"
elif [ "${GECKO_DESKTOP_NO_SANDBOX:-}" = 1 ]; then
	set -- --no-sandbox "$@"
fi
exec "$here/gecko-desktop.bin" "$@"
