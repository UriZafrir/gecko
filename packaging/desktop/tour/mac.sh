#!/bin/bash
# The feature tour on a macOS CI runner (after smoke/mac.sh installed
# /Applications/Gecko.app): a fresh GECKO_HOME and HOME, the screen
# recorded with screencapture, the tour driven by tour.cjs. The second
# machine is a second Gecko daemon on the same runner, reached over ssh to
# localhost (its own home and port). Writes $OUT/tour.mov, captions.txt
# and t0.txt.
#   packaging/desktop/tour/mac.sh out/tour
set -u
OUT=${1:-out/tour}
mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)
HERE=$(cd "$(dirname "$0")" && pwd)
APP=/Applications/Gecko.app
G=$APP/Contents/Resources/gecko
PORT=17696 CDP=17699
log() { echo "[$(date +%T)] $*" | tee -a "$OUT/tour-setup.log"; }

H=$RUNNER_TEMP/tour-me
rm -rf "$H" && mkdir -p "$H/bin" "$H/projects/webapp/src/auth"
export GECKO_HOME=$H/.gecko-terminal
mkdir -p "$GECKO_HOME/state" && chmod 700 "$GECKO_HOME" "$GECKO_HOME/state"
printf '{"listen":"0.0.0.0:%s","name":"mac-ci"}\n' $PORT >"$GECKO_HOME/config.json"
echo '{"theme":"tokyo-night","fontSize":13,"opacity":1}' >"$GECKO_HOME/settings.json"
touch "$GECKO_HOME/no-login-service"
cp "$HERE/claude" "$H/bin/claude" && chmod +x "$H/bin/claude"
ln -s "$G" "$H/bin/gecko"
cat >"$H/.bashrc" <<'RC'
export PATH="$HOME/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
export BASH_SILENCE_DEPRECATION_WARNING=1
PS1='\[\e[32m\]\u@mac-ci\[\e[0m\]:\[\e[34m\]\W\[\e[0m\]\$ '
if [ "$PWD" = "$HOME" ]; then cd ~/projects/webapp; fi
RC
cp "$H/.bashrc" "$H/.bash_profile"
printf '[user]\n\tname = Demo\n\temail = demo@example.com\n[init]\n\tdefaultBranch = main\n' >"$H/.gitconfig"
(cd "$H/projects/webapp" && echo "export const maxAge = (ttl) => Math.floor(ttl / 1000);" >src/auth/session.js &&
	HOME=$H git init -q && HOME=$H git add -A && HOME=$H git commit -qm "Login with session cookies" && HOME=$H git checkout -qb fix-login)

# The second machine: sshd on, a key for localhost.
REMOTE_ADDED=0
sudo launchctl enable system/com.openssh.sshd 2>/dev/null
sudo launchctl bootstrap system /System/Library/LaunchDaemons/ssh.plist 2>/dev/null ||
	sudo launchctl load -w /System/Library/LaunchDaemons/ssh.plist 2>/dev/null
mkdir -p ~/.ssh && chmod 700 ~/.ssh
[ -f ~/.ssh/id_ed25519 ] || ssh-keygen -q -t ed25519 -N '' -f ~/.ssh/id_ed25519
cat ~/.ssh/id_ed25519.pub >>~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys
ssh-keyscan -H localhost >>~/.ssh/known_hosts 2>/dev/null
if ssh -o BatchMode=yes -o ConnectTimeout=10 localhost true </dev/null 2>>"$OUT/tour-setup.log"; then
	log "ssh localhost works"
	REMOTE_LABEL='a second Gecko daemon on this runner (own home and port), over ssh to localhost'
else
	log "no ssh to localhost; using gecko bridge directly"
	REMOTE_ADDED=1
	mkdir -p "$RUNNER_TEMP/second/.gecko-terminal"
	printf '{"listen":"127.0.0.1:17697","name":"second"}\n' >"$RUNNER_TEMP/second/.gecko-terminal/config.json"
	printf '{"listen":"0.0.0.0:%s","name":"mac-ci","hosts":[{"name":"second","command":["env","GECKO_HOME=%s","%s","bridge"]}]}\n' \
		$PORT "$RUNNER_TEMP/second/.gecko-terminal" "$G" >"$GECKO_HOME/config.json"
	REMOTE_LABEL='a second Gecko daemon on this runner (own home and port), connected with gecko bridge'
fi

(cd "$HERE" && npm ci --no-audit --no-fund --silent)

# Behind the window, for the transparency chapter: a photo wallpaper and a
# Chrome window with big text, opened before Gecko so Gecko is on top.
CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
WALL=$(cd "$HERE/../../.." && pwd)/web/public/themes/jellyfish.jpg
osascript -e "tell application \"System Events\" to tell every desktop to set picture to \"$WALL\"" >>"$OUT/tour-setup.log" 2>&1 || log "wallpaper not set"
if [ -x "$CHROME" ]; then
	"$CHROME" --user-data-dir="$RUNNER_TEMP/backdrop" --no-first-run --no-default-browser-check \
		--app="file://$HERE/backdrop.html" --window-position=20,70 --window-size=560,430 </dev/null >/dev/null 2>&1 &
	sleep 5
fi

echo "$(date +%s)|start" >"$OUT/t0.txt"
screencapture -x -v -V 420 "$OUT/tour.mov" >/dev/null 2>&1 &
RPID=$!
sleep 2
cd "$H"
HOME=$H BASH_SILENCE_DEPRECATION_WARNING=1 PATH=$H/bin:$PATH "$APP/Contents/MacOS/Gecko" --remote-debugging-port=$CDP >"$OUT/app.log" 2>&1 &
APID=$!
for _ in $(seq 60); do curl -fs http://127.0.0.1:$CDP/json/version >/dev/null && break; sleep 0.5; done
sleep 4
osascript -e 'tell application "System Events" to tell process "Gecko" to set frontmost to true' 2>/dev/null

TOUR_CDP=$CDP TOUR_PLATFORM=mac TOUR_CAPTIONS=$OUT/captions.txt TOUR_BOUNDS=0,25,1020,676 \
	TOUR_PHONE_BROWSER=$([ -x "$CHROME" ] && echo "$CHROME") TOUR_PHONE_PROFILE=$RUNNER_TEMP/phone \
	TOUR_PHONE_LABEL='a second client (Chrome, sized like a phone)' \
	TOUR_REMOTE=localhost TOUR_REMOTE_NAME=$([ $REMOTE_ADDED = 1 ] && echo second || echo mac-ci-2) \
	TOUR_REMOTE_LABEL="$REMOTE_LABEL" TOUR_REMOTE_ADDED=$REMOTE_ADDED TOUR_TMUX=$(command -v tmux >/dev/null && echo 1 || echo 0) \
	TOUR_SEE_THROUGH='macOS blurs what is behind (vibrancy)' \
	node "$HERE/tour.cjs" </dev/null 2>&1 | tee "$OUT/tour.log"
rc=${PIPESTATUS[0]}
sleep 1
kill -INT $RPID 2>/dev/null; wait $RPID 2>/dev/null
kill -TERM $APID 2>/dev/null
pkill -f "user-data-dir=$RUNNER_TEMP/backdrop" 2>/dev/null
HOME=$H "$G" stop >/dev/null 2>&1
env -u GECKO_HOME ~/.local/bin/gecko stop >/dev/null 2>&1
cp "$GECKO_HOME/state/daemon.log" "$OUT/" 2>/dev/null
ls -la "$OUT" | tee -a "$OUT/tour-setup.log"
exit $rc
