#!/usr/bin/env python3
"""The feature tour video: each part's recording with its captions in a bar
under the picture (never over it), one after the other, H.264 for phones.

Usage: ci-tour.py <out.mp4> <part> [<part> ...]
A part is a path prefix P with P.mp4 (the recording), P-start.txt (when it
began, Unix time) and P-chapters.txt ("<unix time>\t<caption>" lines, from
FeatureTour.swift). A part recorded in landscape on a portrait screen
(simctl records the screen as it is mounted) is turned upright when its
name says pad (the iPad part runs in landscape).
"""
import json
import os
import subprocess
import sys
import tempfile
import textwrap

W, H, BAR = 1080, 1920, 220
BG, FG, ACCENT = "0x1a1b26", "0xc0caf5", "0x7aa2f7"  # Tokyo Night


def font():
    try:
        return subprocess.run(["fc-match", "-f", "%{file}", "DejaVu Sans:bold"], capture_output=True, text=True).stdout.strip()
    except OSError:
        return ""


def duration(path):
    r = subprocess.run(["ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "json", path], capture_output=True, text=True)
    return float(json.loads(r.stdout)["format"]["duration"])


def size(path):
    r = subprocess.run(["ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height", "-of", "json", path], capture_output=True, text=True)
    s = json.loads(r.stdout)["streams"][0]
    return s["width"], s["height"]


def part(prefix, tmp, n, fontfile):
    video = prefix + ".mp4"
    if not os.path.exists(video):
        print("no " + video, file=sys.stderr)
        return None
    start = float(open(prefix + "-start.txt").read().strip()) if os.path.exists(prefix + "-start.txt") else 0.0
    chapters = []
    if os.path.exists(prefix + "-chapters.txt"):
        for line in open(prefix + "-chapters.txt", encoding="utf-8"):
            t, _, text = line.rstrip("\n").partition("\t")
            if text:
                chapters.append((max(0.0, float(t) - start), text))
    # From just before the first caption (the Simulator's home screen
    # before the app starts isn't worth watching).
    skip = max(0.0, chapters[0][0] - 1.5) if chapters else 0.0
    chapters = [(t - skip, text) for t, text in chapters]
    end = duration(video) - skip
    w, h = size(video)
    turn = "pad" in os.path.basename(prefix) and h > w
    filters = []
    if turn:
        # Upright, then the left part (the sidebar and the start of the
        # terminal) larger, so it can be read on a phone.
        filters.append("transpose=2")
        filters.append("crop=trunc(iw*0.68/2)*2:ih:0:0")
    area = H - BAR
    filters.append(f"scale={W}:{area}:force_original_aspect_ratio=decrease")
    filters.append(f"pad={W}:{H}:(ow-iw)/2:({area}-ih)/2:color={BG}")
    filters.append(f"drawbox=x=0:y={area}:w={W}:h=4:color={ACCENT}:t=fill")
    for i, (t, text) in enumerate(chapters):
        until = chapters[i + 1][0] if i + 1 < len(chapters) else end + 1
        f = os.path.join(tmp, f"cap-{n}-{i}.txt")
        with open(f, "w", encoding="utf-8") as fh:
            fh.write("\n".join(textwrap.wrap(text, 46)))
        filters.append(
            f"drawtext=fontfile={fontfile}:textfile={f}:fontsize=40:line_spacing=12:fontcolor={FG}"
            f":x=(w-text_w)/2:y={area}+({BAR}-text_h)/2:enable='between(t,{t:.2f},{until:.2f})'"
        )
    out = os.path.join(tmp, f"part-{n}.mp4")
    subprocess.run(["ffmpeg", "-loglevel", "error", "-y", "-ss", f"{skip:.2f}", "-i", video, "-an", "-r", "30", "-vf", ",".join(filters),
                    "-c:v", "libx264", "-pix_fmt", "yuv420p", "-preset", "veryfast", "-crf", "23", out], check=True)
    return out


def main():
    out, prefixes = sys.argv[1], sys.argv[2:]
    os.makedirs(os.path.dirname(os.path.abspath(out)), exist_ok=True)
    fontfile = font()
    with tempfile.TemporaryDirectory() as tmp:
        parts = [p for p in (part(x, tmp, n, fontfile) for n, x in enumerate(prefixes)) if p]
        if not parts:
            print("no parts recorded", file=sys.stderr)
            return 1
        lst = os.path.join(tmp, "list.txt")
        with open(lst, "w") as fh:
            fh.writelines(f"file '{p}'\n" for p in parts)
        subprocess.run(["ffmpeg", "-loglevel", "error", "-y", "-f", "concat", "-safe", "0", "-i", lst,
                        "-c", "copy", "-movflags", "+faststart", out], check=True)
    print(out)
    return 0


if __name__ == "__main__":
    sys.exit(main())
