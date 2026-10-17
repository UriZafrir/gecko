#!/bin/sh
# Boots an iOS Simulator for the CI job: the named device on the newest
# iOS 18 runtime (made if the runner has none), with the on-screen keyboard
# (no hardware keyboard), a tidy status bar, and light or dark appearance.
# Usage: ci-simulator.sh "<device type name>"; writes SIM_ID to $GITHUB_ENV
# and prints it last.
set -eu
name=$1
runtime=$(xcrun simctl list runtimes -j | python3 -c '
import json, sys
rs = [r for r in json.load(sys.stdin)["runtimes"] if r["isAvailable"] and r["identifier"].startswith("com.apple.CoreSimulator.SimRuntime.iOS-18")]
rs.sort(key=lambda r: [int(x) for x in r["version"].split(".")])
print(rs[-1]["identifier"])')
id=$(xcrun simctl list devices -j | python3 -c '
import json, sys
d = json.load(sys.stdin)["devices"].get(sys.argv[1], [])
m = [x["udid"] for x in d if x["name"] == sys.argv[2] and x["isAvailable"]]
print(m[0] if m else "")' "$runtime" "$name")
if [ -z "$id" ]; then
  type=$(xcrun simctl list devicetypes -j | python3 -c '
import json, sys
print([t["identifier"] for t in json.load(sys.stdin)["devicetypes"] if t["name"] == sys.argv[1]][0])' "$name")
  id=$(xcrun simctl create "$name" "$type" "$runtime")
fi
echo "simulator: $name ($runtime) $id" >&2
defaults write com.apple.iphonesimulator ConnectHardwareKeyboard -bool false
{
  xcrun simctl boot "$id" || true
  xcrun simctl bootstatus "$id" -b
  xcrun simctl status_bar "$id" override --time 9:41 --dataNetwork wifi --wifiBars 3 --batteryState charged --batteryLevel 100 || true
  xcrun simctl ui "$id" appearance light || true
} >&2
echo "SIM_ID=$id" >> "${GITHUB_ENV:-/dev/null}"
echo "$id"
