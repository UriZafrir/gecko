#!/bin/bash
# Smoke test of an installed Gecko package (deb, rpm, Arch, snap), run as an
# ordinary user: the CLI, a daemon in a fresh GECKO_HOME on a spare port, a
# session, and the window under Xvfb (screenshot in <out>) opened without
# --url, so it finds the daemon itself.
#   packaging/linux/smoke.sh <out dir>
# GECKO_SMOKE_DESKTOP names the window command (default gecko-desktop).
set -u
OUT=$(cd "${1:-.}" && pwd)
PORT=17697
export GECKO_HOME=$(mktemp -d)/gecko-home
mkdir -p "$GECKO_HOME" && chmod 700 "$GECKO_HOME"
printf '{"listen":"127.0.0.1:%s","name":"smoke"}\n' $PORT >"$GECKO_HOME/config.json"
echo '{"theme":"tokyo-night"}' >"$GECKO_HOME/settings.json" # screenshots use Tokyo Night
fail=0
log() { echo "[$(date +%T)] $*" | tee -a "$OUT/smoke.log"; }
check() { if eval "$2"; then log "PASS $1"; else log "FAIL $1"; fail=1; fi; }
. /etc/os-release 2>/dev/null && log "on $PRETTY_NAME"

log "gecko: $(command -v gecko) $(gecko version </dev/null)"
check "gecko url starts the daemon" "gecko url </dev/null | tee -a '$OUT/smoke.log' | grep -q 'token='"
TOKEN=$(cat "$GECKO_HOME/token")
check "daemon answers /api/info" "curl -fsS -H 'Authorization: Bearer $TOKEN' http://127.0.0.1:$PORT/api/info | grep -q smoke"
check "web UI served" "curl -fsS http://127.0.0.1:$PORT/ | grep -q '<title>Gecko'"
S=$(gecko new -n smoke </dev/null | tail -1)
sleep 2
gecko send "$S" 'echo "hello from $(. /etc/os-release; echo $ID) $(uname -m)"' </dev/null
sleep 2
check "terminal ran the command" "gecko capture '$S' </dev/null | grep -q 'hello from'"

desk=${GECKO_SMOKE_DESKTOP:-gecko-desktop}
if command -v "$desk" >/dev/null && command -v Xvfb >/dev/null; then
	Xvfb :77 -screen 0 1280x800x24 -nolisten tcp </dev/null >/dev/null 2>&1 &
	XPID=$!
	export DISPLAY=:77
	sleep 2
	for mode in sandbox no-sandbox; do
		[ $mode = no-sandbox ] && export GECKO_DESKTOP_NO_SANDBOX=1
		"$desk" </dev/null >"$OUT/desktop-$mode.log" 2>&1 &
		DPID=$!
		sleep 12
		if kill -0 $DPID 2>/dev/null; then
			log "window up ($mode)"
			gecko send "$S" 'echo "typed while the window is open"' </dev/null
			sleep 3
			import -window root "$OUT/window.png" 2>/dev/null || true
			kill $DPID; wait $DPID 2>/dev/null
			break
		fi
		log "window exited in $mode mode: $(tail -3 "$OUT/desktop-$mode.log" | tr '\n' ' ')"
	done
	check "window ran" "[ -f '$OUT/window.png' ]"
	kill $XPID 2>/dev/null
fi
check "session outlives the window" "gecko capture '$S' </dev/null | grep -q 'typed while the window is open\|hello from'"
gecko capture "$S" </dev/null >"$OUT/capture.txt"
gecko stop </dev/null
exit $fail
