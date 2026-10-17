#!/bin/sh
# After the device matrix: a contact sheet of each device's screenshots and
# one video of all the devices' tours, 04-devices.mp4. Needs ImageMagick
# and ffmpeg. Usage: ci-report.sh <dir with one folder per device> <out dir>
set -eu
in=$1 out=$2
mkdir -p "$out/contact" "$out/tmp"
font=$(fc-match -f '%{file}' 'DejaVu Sans:bold' 2>/dev/null || true)
list="$out/tmp/list.txt"
: > "$list"
for d in "$in"/*/; do
  dev=$(basename "$d")
  label=$(cat "$d/device.txt" 2>/dev/null || echo "$dev")
  if ls "$d"shots/*.png > /dev/null 2>&1; then
    montage -label '%t' -font "${font:-DejaVu-Sans}" -pointsize 22 "$d"shots/*.png \
      -tile 6x -geometry 360x780+12+12 -background '#19211a' -fill '#d8e0d2' \
      -title "$label" "$out/contact/contact-$dev.png"
  fi
  if [ -f "$d/tour.mp4" ]; then
    ffmpeg -loglevel error -y -i "$d/tour.mp4" -an -r 30 \
      -vf "scale=1920:1080:force_original_aspect_ratio=decrease,pad=1920:1080:(ow-iw)/2:(oh-ih)/2:color=0x19211a,drawtext=fontfile=${font}:text='${label}':x=40:y=40:fontsize=44:fontcolor=0xd8e0d2" \
      -c:v libx264 -pix_fmt yuv420p -preset veryfast "$out/tmp/$dev.mp4"
    echo "file '$dev.mp4'" >> "$list"
  fi
done
if [ -s "$list" ]; then
  (cd "$out/tmp" && ffmpeg -loglevel error -y -f concat -safe 0 -i list.txt -c copy ../04-devices.mp4)
fi
rm -rf "$out/tmp"
ls -l "$out" "$out/contact"
