#!/bin/bash
# CI smoke test of the macOS app: mounts the dmg, copies Gecko.app to
# /Applications, starts it with a fresh GECKO_HOME and a spare port, checks
# that it started its bundled daemon and that the UI loads, types into a
# terminal, quits, relaunches and checks the session survived. Screenshots
# (and a screen recording when allowed) go to $OUT.
#   packaging/desktop/smoke/mac.sh dist/desktop/Gecko-*.dmg out/
set -u
DMG=$1
OUT=${2:-out}
PORT=17694
mkdir -p "$OUT/frames"
OUT=$(cd "$OUT" && pwd)
export GECKO_HOME=$RUNNER_TEMP/gecko-home
rm -rf "$GECKO_HOME" && mkdir -p "$GECKO_HOME" && chmod 700 "$GECKO_HOME"
printf '{"listen":"127.0.0.1:%s","name":"mac-ci"}\n' $PORT >"$GECKO_HOME/config.json"
# Demos use the Tokyo Night theme.
echo '{"theme":"tokyo-night"}' >"$GECKO_HOME/settings.json"
fail=0
log() { echo "[$(date +%T)] $*" | tee -a "$OUT/smoke.log"; }
check() { if eval "$2"; then log "PASS $1"; else log "FAIL $1"; fail=1; fi; }

mnt=$(hdiutil attach -nobrowse -noautoopen "$DMG" | awk -F'\t' '/\/Volumes\//{print $NF}' | tail -1)
log "mounted at $mnt: $(ls "$mnt" | tr '\n' ' ')"
rm -rf /Applications/Gecko.app && cp -R "$mnt/Gecko.app" /Applications/
hdiutil detach "$mnt" -quiet || true
APP=/Applications/Gecko.app
G=$APP/Contents/Resources/gecko
log "bundled gecko: $("$G" version)"
log "archs: $(lipo -archs "$G") / $(lipo -archs "$APP/Contents/MacOS/Gecko")"
log "build id: $(cat "$APP/Contents/Resources/app.asar" | strings | grep -o '"commit":"[^"]*","time":"[^"]*"' | head -1)"
codesign -dv "$APP" 2>&1 | grep -E 'Signature|Identifier' | tee -a "$OUT/smoke.log"

# Frames for the video, two a second, for as long as the test runs.
(i=0; while [ ! -f "$OUT/.stop" ]; do screencapture -x -C "$OUT/frames/$(printf %04d $i).png" 2>/dev/null; i=$((i+1)); sleep 0.5; done) &
# A real recording too, where the runner allows screen capture video.
(screencapture -x -v -V 45 "$OUT/screen.mov" >/dev/null 2>&1 || true) &

launch() {
	"$APP/Contents/MacOS/Gecko" >>"$OUT/app.log" 2>&1 &
	APID=$!
	for _ in $(seq 60); do [ -s "$GECKO_HOME/state/web" ] && break; sleep 0.5; done
}

log "first launch"
launch
sleep 6
TOKEN=$(cat "$GECKO_HOME/token")
check "daemon answers /api/info" "curl -fsS -H 'Authorization: Bearer $TOKEN' http://127.0.0.1:$PORT/api/info | tee -a '$OUT/smoke.log' | grep -q mac-ci"
check "daemon runs the bundled gecko" "pgrep -lf 'Gecko.app/Contents/Resources/gecko daemon' | tee -a '$OUT/smoke.log' | grep -q daemon"
check "web UI served" "curl -fsS http://127.0.0.1:$PORT/ | grep -q '<title>Gecko'"
screencapture -x "$OUT/mac-1-start.png"

# A terminal: click "New tab" with System Events if allowed, else make one
# with the CLI. Either way type into it through the UI or gecko send.
osascript -e 'tell application "System Events" to tell process "Gecko" to set frontmost to true' 2>>"$OUT/smoke.log"
sleep 1
S=$("$G" new -n smoke 2>>"$OUT/smoke.log")
sleep 2
log "session: $S"
typed=0
if osascript -e 'tell application "System Events" to keystroke "echo typed in the Gecko window on $(uname -sm); export MARK=42"' -e 'tell application "System Events" to key code 36' 2>>"$OUT/smoke.log"; then typed=1; fi
sleep 2
"$G" send "$S" 'echo "hello from gecko send on macOS $(sw_vers -productVersion)"; export MARK2=43' >>"$OUT/smoke.log" 2>&1
sleep 3
screencapture -x "$OUT/mac-2-typed.png"
"$G" capture "$S" >"$OUT/capture-1.txt" 2>&1
check "terminal ran the command" "grep -q 'hello from gecko send on macOS' '$OUT/capture-1.txt'"
log "keystrokes through the window: $typed"

log "quit the app"
kill -TERM $APID; wait $APID 2>/dev/null
sleep 2
check "window gone, daemon still up" "! kill -0 $APID 2>/dev/null && curl -fsS -H 'Authorization: Bearer $TOKEN' http://127.0.0.1:$PORT/api/info >/dev/null"

log "relaunch"
launch
sleep 6
"$G" send "$S" 'echo "same shell after relaunch: MARK2=$MARK2"' >>"$OUT/smoke.log" 2>&1
sleep 3
screencapture -x "$OUT/mac-3-relaunch.png"
"$G" capture "$S" >"$OUT/capture-2.txt" 2>&1
check "session survived the relaunch" "grep -q 'same shell after relaunch: MARK2=43' '$OUT/capture-2.txt'"
kill -TERM $APID; wait $APID 2>/dev/null
touch "$OUT/.stop"
sleep 1
"$G" stop >>"$OUT/smoke.log" 2>&1
cp "$GECKO_HOME/state/daemon.log" "$OUT/" 2>/dev/null
wait
exit $fail
