#!/bin/sh
# Runs one UI test class on the booted simulator ($SIM_ID), recording the
# screen to <out>/<video>.mp4 (unless the video name is "-"). The tests find
# the daemons through GECKO_LINK and GECKO_OLD_LINK and save screenshots in
# <out>/shots. Usage: ci-uitest.sh <xctestrun> <Class> <video> <out> [shot prefix]
set -u
xctestrun=$1 class=$2 video=$3 out=$4 prefix=${5:-}
mkdir -p "$out" && out=$(cd "$out" && pwd)
tag=$(echo "$class" | tr / -)$prefix
mkdir -p "$out/shots" "$out/results"
if [ "$video" != - ]; then
  xcrun simctl io "$SIM_ID" recordVideo --codec=h264 --force "$out/$video.mp4" > "$out/results/$video-record.log" 2>&1 &
  rec=$!
  sleep 2
fi
TEST_RUNNER_GECKO_LINK=${GECKO_LINK:-} TEST_RUNNER_GECKO_OLD_LINK=${GECKO_OLD_LINK:-} \
TEST_RUNNER_GECKO_SHOTS="$out/shots" TEST_RUNNER_GECKO_SHOT_PREFIX="$prefix" \
xcodebuild test-without-building -xctestrun "$xctestrun" -destination "id=$SIM_ID" \
  -only-testing:"GeckoUITests/$class" -resultBundlePath "$out/results/$tag.xcresult" > "$out/results/$tag.log" 2>&1
status=$?
grep -E "Test Case|error|failed|passed|Executed" "$out/results/$tag.log" | tail -40
if [ "$video" != - ]; then
  sleep 1
  kill -INT "$rec" 2> /dev/null
  wait "$rec" 2> /dev/null
fi
exit $status
