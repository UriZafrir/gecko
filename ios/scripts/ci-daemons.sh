#!/bin/sh
# Starts the Gecko daemons the UI tests talk to, on this Mac (the Simulator
# shares its network), each with its own GECKO_HOME and token, a few tabs,
# the Tokyo Night theme, and a stand-in for Claude Code on the PATH:
# - studio-mac, this branch's gecko on port 7681 (on every address, so its
#   Open on another device has a link and QR code to show);
# - an older one (protocol 6, from main) on 127.0.0.1:7682;
# - build-box, a second gecko (~/.gecko-terminal, port 7683) that studio-mac
#   adds as a machine: over ssh to this Mac when the runner's sshd can be
#   turned on, else through gecko bridge run locally (the feature tour).
# Usage: ci-daemons.sh <gecko> <older gecko> <out dir>; writes GECKO_LINK,
# GECKO_OLD_LINK, GECKO_BUILDBOX_LINK, GECKO_REMOTE (ssh or bridge) and
# GECKO_STUDIO_HOME to $GITHUB_ENV.
# ci-daemons.sh reset <gecko>: ends every studio-mac tab and opens the first
# three again in a workspace named tour (a clean start for the feature tour).
set -eu
abs() { (cd "$(dirname "$1")" && echo "$(pwd)/$(basename "$1")"); }
tmp=${RUNNER_TEMP:-/tmp}
home=$tmp/gecko-home
# The ports (another base to try the script on a machine running Gecko).
p1=${GECKO_CI_PORT:-7681} p2=$((${GECKO_CI_PORT:-7681} + 1)) p3=$((${GECKO_CI_PORT:-7681} + 2))

tabs() { # gecko [workspace]
  cd "$tmp/work/parser"
  GECKO_HOME=$home "$1" new ${2:+-w "$2"} -n shell -C "$tmp/work/parser" > /dev/null
  GECKO_HOME=$home "$1" new ${2:+-w "$2"} -n build -C "$tmp/work" > /dev/null
  GECKO_HOME=$home "$1" new ${2:+-w "$2"} -n logs -C "$tmp/work" > /dev/null
}

if [ "$1" = reset ]; then
  g=$(abs "$2")
  for id in $(GECKO_HOME=$home "$g" ls --json | python3 -c '
import json, sys
d = json.load(sys.stdin)
d = d if isinstance(d, list) else d.get("sessions", [])
print(" ".join(s["id"] + "@" + s.get("tty", "") for s in d if s.get("host") == "studio-mac"))'); do
    GECKO_HOME=$home "$g" kill "${id%@*}" > /dev/null 2>&1 || true
    # gecko kill closes the terminal, which hasn't always ended the shell
    # on macOS here; end what still runs on it.
    tty=${id#*@}
    [ -n "$tty" ] && pkill -HUP -t "${tty#/dev/}" 2> /dev/null || true
  done
  [ "${GECKO_REMOTE:-}" = ssh ] && { GECKO_HOME=$home "$g" host rm build-box > /dev/null 2>&1 || true; }
  sleep 1
  # In a workspace of their own, so the tour's tab bar has just these
  # (whatever the earlier tests left behind).
  tabs "$g" tour
  GECKO_HOME=$home "$g" ls
  exit 0
fi

mkdir -p "$3"
new=$(abs "$1") old=$(abs "$2") out=$(abs "$3")
root=$(cd "$(dirname "$0")" && pwd)
mkdir -p "$out" "$tmp/fakebin" "$tmp/work/parser" "$tmp/buildbox/api" "$tmp/buildbox/deploy" "$HOME/.local/bin"
cp "$root/fake-claude" "$tmp/fakebin/claude"
cp "$new" "$HOME/.local/bin/gecko"
# gecko in the tabs (the feature tour's agent runs it), for studio-mac.
printf '#!/bin/sh\nGECKO_HOME=${GECKO_HOME:-%s} exec %s "$@"\n' "$home" "$new" > "$tmp/fakebin/gecko"
chmod +x "$tmp/fakebin/claude" "$tmp/fakebin/gecko" "$HOME/.local/bin/gecko" "$new" "$old"
export PATH="$tmp/fakebin:$PATH" SHELL=${GECKO_CI_SHELL:-/bin/zsh} GECKO_FAKE_WORK=14 BASH_SILENCE_DEPRECATION_WARNING=1
# zsh asks a new user to set it up; an empty config skips that.
touch "$HOME/.zshrc"

start() { # bin home port log address
  # Every demo and screenshot in Tokyo Night (the web UI's settings, synced to each window).
  mkdir -p "$2"
  printf '{"theme":"tokyo-night"}\n' > "$2/settings.json"
  GECKO_HOME=$2 nohup "$1" daemon --listen "$5:$3" > "$4" 2>&1 < /dev/null &
  i=0
  until [ -s "$2/token" ] && curl -fsS -H "Authorization: Bearer $(cat "$2/token")" "http://127.0.0.1:$3/api/info" > /dev/null 2>&1; do
    i=$((i + 1)); [ $i -gt 60 ] && { cat "$4"; exit 1; }
    sleep 0.5
  done
}

# build-box first: studio-mac's config names it when there's no sshd.
bb=$HOME/.gecko-terminal
mkdir -p "$bb"
printf '{"name":"build-box"}\n' > "$bb/config.json"
start "$HOME/.local/bin/gecko" "$bb" $p3 "$out/daemon-buildbox.log" 127.0.0.1
GECKO_HOME=$bb "$new" new -n api -C "$tmp/buildbox/api" > /dev/null
GECKO_HOME=$bb "$new" new -n deploy -C "$tmp/buildbox/deploy" > /dev/null

# ssh to this Mac, with a key, as studio-mac would reach a real build-box.
remote=bridge
mkdir -p "$HOME/.ssh" && chmod 700 "$HOME/.ssh"
[ -f "$HOME/.ssh/id_ed25519" ] || ssh-keygen -q -t ed25519 -N '' -f "$HOME/.ssh/id_ed25519"
cat "$HOME/.ssh/id_ed25519.pub" >> "$HOME/.ssh/authorized_keys"
chmod 600 "$HOME/.ssh/authorized_keys"
{
  sudo -n launchctl enable system/com.openssh.sshd || true
  sudo -n launchctl bootstrap system /System/Library/LaunchDaemons/ssh.plist || sudo -n launchctl load -w /System/Library/LaunchDaemons/ssh.plist || true
} > "$out/sshd.log" 2>&1
i=0
while [ $i -lt 10 ]; do
  if ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ConnectTimeout=5 "$(whoami)@localhost" true < /dev/null >> "$out/sshd.log" 2>&1; then
    remote=ssh
    break
  fi
  i=$((i + 1))
  sleep 1
done
mkdir -p "$home"
if [ $remote = ssh ]; then
  printf '{"name":"studio-mac"}\n' > "$home/config.json"
else
  printf '{"name":"studio-mac","hosts":[{"name":"build-box","command":["env","GECKO_HOME=%s","%s","bridge"]}]}\n' "$bb" "$HOME/.local/bin/gecko" > "$home/config.json"
fi
echo "build-box: $remote"

start "$new" "$home" $p1 "$out/daemon.log" 0.0.0.0
start "$old" "$tmp/gecko-old-home" $p2 "$out/daemon-old.log" 127.0.0.1
tabs "$new"
GECKO_HOME=$tmp/gecko-old-home "$old" new -n shell -C "$tmp/work" > /dev/null
# The app opens studio-mac at 127.0.0.1 (the Simulator is on this Mac).
link="http://127.0.0.1:$p1/?token=$(cat "$home/token")"
oldlink=$(GECKO_HOME=$tmp/gecko-old-home "$old" url | head -1)
bblink="http://127.0.0.1:$p3/?token=$(cat "$bb/token")"
echo "gecko: $(echo "$link" | sed 's/token=.*/token=.../')"
echo "older gecko: $(echo "$oldlink" | sed 's/token=.*/token=.../')"
{
  echo "GECKO_LINK=$link"
  echo "GECKO_OLD_LINK=$oldlink"
  echo "GECKO_BUILDBOX_LINK=$bblink"
  echo "GECKO_REMOTE=$remote"
  echo "GECKO_STUDIO_HOME=$home"
} >> "${GITHUB_ENV:-/dev/null}"
