#!/bin/sh
# Starts the Gecko daemons the UI tests talk to, on this Mac (the Simulator
# shares its network): this branch's gecko on 127.0.0.1:7681 and an older
# one (protocol 6, from main) on 127.0.0.1:7682, each with its own GECKO_HOME
# and token, a few tabs, and a stand-in for Claude Code on the PATH.
# Usage: ci-daemons.sh <gecko> <older gecko> <out dir>; writes GECKO_LINK
# and GECKO_OLD_LINK to $GITHUB_ENV.
set -eu
new=$1 old=$2 out=$3
root=$(cd "$(dirname "$0")" && pwd)
tmp=${RUNNER_TEMP:-/tmp}
mkdir -p "$out" "$tmp/fakebin" "$tmp/work/parser"
cp "$root/fake-claude" "$tmp/fakebin/claude"
chmod +x "$tmp/fakebin/claude" "$new" "$old"
export PATH="$tmp/fakebin:$PATH" SHELL=/bin/zsh GECKO_FAKE_WORK=14 BASH_SILENCE_DEPRECATION_WARNING=1
# zsh asks a new user to set it up; an empty config skips that.
touch "$HOME/.zshrc"

start() { # bin home port log
  GECKO_HOME=$2 nohup "$1" daemon --listen "127.0.0.1:$3" > "$4" 2>&1 < /dev/null &
  i=0
  until [ -s "$2/token" ] && curl -fsS -H "Authorization: Bearer $(cat "$2/token")" "http://127.0.0.1:$3/api/info" > /dev/null 2>&1; do
    i=$((i + 1)); [ $i -gt 60 ] && { cat "$4"; exit 1; }
    sleep 0.5
  done
}

start "$new" "$tmp/gecko-home" 7681 "$out/daemon.log"
start "$old" "$tmp/gecko-old-home" 7682 "$out/daemon-old.log"
cd "$tmp/work/parser"
GECKO_HOME=$tmp/gecko-home "$new" new -n shell -C "$tmp/work/parser" > /dev/null
GECKO_HOME=$tmp/gecko-home "$new" new -n build -C "$tmp/work" > /dev/null
GECKO_HOME=$tmp/gecko-home "$new" new -n logs -C "$tmp/work" > /dev/null
GECKO_HOME=$tmp/gecko-old-home "$old" new -n shell -C "$tmp/work" > /dev/null
link=$(GECKO_HOME=$tmp/gecko-home "$new" url | head -1)
oldlink=$(GECKO_HOME=$tmp/gecko-old-home "$old" url | head -1)
echo "gecko: $(echo "$link" | sed 's/token=.*/token=.../')"
echo "older gecko: $(echo "$oldlink" | sed 's/token=.*/token=.../')"
{ echo "GECKO_LINK=$link"; echo "GECKO_OLD_LINK=$oldlink"; } >> "${GITHUB_ENV:-/dev/null}"
