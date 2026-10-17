#!/bin/bash
# CI smoke test of the AppImage under Xvfb: fresh GECKO_HOME, spare port;
# checks the bundled daemon starts, the UI loads, a session outlives the
# window, and `Gecko.AppImage gecko <args>` runs the CLI.
#   packaging/desktop/smoke/appimage.sh dist/desktop/Gecko-*.AppImage out/
set -u
AI=$(readlink -f "$1")
OUT=${2:-out}
PORT=17696
mkdir -p "$OUT" && OUT=$(cd "$OUT" && pwd)
export GECKO_HOME=$RUNNER_TEMP/gecko-home DISPLAY=:99
rm -rf "$GECKO_HOME" && mkdir -p "$GECKO_HOME" && chmod 700 "$GECKO_HOME"
printf '{"listen":"127.0.0.1:%s","name":"appimage-ci"}\n' $PORT >"$GECKO_HOME/config.json"
echo '{"theme":"tokyo-night"}' >"$GECKO_HOME/settings.json" # demos use Tokyo Night
fail=0
log() { echo "[$(date +%T)] $*" | tee -a "$OUT/smoke.log"; }
check() { if eval "$2"; then log "PASS $1"; else log "FAIL $1"; fail=1; fi; }

Xvfb :99 -screen 0 1280x800x24 -nolisten tcp </dev/null >/dev/null 2>&1 &
XPID=$!
sleep 2
log "CLI: $("$AI" gecko version </dev/null 2>&1)"
"$AI" </dev/null >>"$OUT/app.log" 2>&1 &
APID=$!
for _ in $(seq 60); do [ -s "$GECKO_HOME/state/web" ] && break; sleep 0.5; done
sleep 6
TOKEN=$(cat "$GECKO_HOME/token")
check "daemon answers /api/info" "curl -fsS -H 'Authorization: Bearer $TOKEN' http://127.0.0.1:$PORT/api/info | tee -a '$OUT/smoke.log' | grep -q appimage-ci"
check "daemon runs the copied gecko" "pgrep -af 'gecko-appimage-[0-9]+ daemon' | tee -a '$OUT/smoke.log' | grep -q daemon"
S=$("$AI" gecko new -n smoke </dev/null | tail -1)
log "session: $S"
sleep 2
"$AI" gecko send "$S" 'echo "hello from the AppImage CLI"' </dev/null
sleep 3
import -window root "$OUT/appimage-1.png" || true
kill -TERM $APID; wait $APID 2>/dev/null
sleep 2
check "session outlives the window" "'$AI' gecko capture '$S' </dev/null | grep -q 'hello from the AppImage CLI'"
"$AI" gecko stop </dev/null
kill $XPID
cp "$GECKO_HOME/state/daemon.log" "$OUT/" 2>/dev/null
exit $fail
