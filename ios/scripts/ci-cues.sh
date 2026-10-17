#!/bin/sh
# The computer's side of the feature tour (FeatureTour.swift): runs in the
# background while the test runs, and does what the test asks for (an empty
# file named after the step in <cues dir>), then marks it done (<step>.done).
# Usage: ci-cues.sh <gecko> <cues dir>; needs GECKO_STUDIO_HOME and GECKO_REMOTE.
set -u
g=$1 cues=$2
mkdir -p "$cues"
gk() { GECKO_HOME=$GECKO_STUDIO_HOME "$g" "$@"; }

step() {
  case "$1" in
    add-host)
      # As you would for a real build-box: gecko host add <name> <ssh destination>.
      if [ "$GECKO_REMOTE" = ssh ]; then
        gk host ls | grep -q '^build-box' || gk host add build-box "$(whoami)@localhost"
      fi
      ;;
    managed)
      # An agent on the computer driving Gecko, typed into the logs tab so
      # the phone shows each command and what it prints.
      gk send logs "gecko new -n agent-task -C ~ && sleep 3 && gecko send agent-task 'echo hello from an agent; uname -sm' && sleep 2 && gecko capture agent-task | tail -n 4 && gecko agents --json | head -c 400; echo"
      ;;
  esac
}

while :; do
  for f in "$cues"/*; do
    [ -f "$f" ] || continue
    case "$f" in *.done | *.log) continue ;; esac
    [ -e "$f.done" ] && continue
    n=$(basename "$f")
    echo "$(date +%T) $n" >> "$cues/cues.log"
    step "$n" >> "$cues/$n.log" 2>&1
    touch "$f.done"
  done
  sleep 0.3
done
