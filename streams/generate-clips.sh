#!/bin/sh
# Encodes one short H.264 clip per test camera. Runs once at image build time,
# so the running container only repackages (-c copy) and uses almost no CPU.
#
# Encoding mimics a typical IP camera: H.264 main profile, no B-frames,
# 1s GOP (keyframe every 30 frames) for fast startup in the browser.
# cam4 is H.265, like many newer cameras, to exercise the backend's
# transcode fallback (browsers mostly can't play H.265).
set -eu

out=${1:?usage: generate-clips.sh <out-dir>}
font=/usr/share/fonts/dejavu/DejaVuSans-Bold.ttf
size=1280x720
rate=30
seconds=20

mkdir -p "$out"

h264="-c:v libx264 -preset medium -profile:v main -bf 0"
h265="-c:v libx265 -preset fast -tag:v hvc1 -x265-params bframes=0:log-level=error"

# encode <name> <lavfi source> <codec args>
encode() {
	name=$1
	source=$2
	codec=$3
	label="drawtext=fontfile=$font:text=$name:x=32:y=32:fontsize=44:fontcolor=white:box=1:boxcolor=black@0.6:boxborderw=12"
	timer="drawtext=fontfile=$font:text='%{pts\:hms}':x=w-tw-32:y=32:fontsize=44:fontcolor=white:box=1:boxcolor=black@0.6:boxborderw=12"

	ffmpeg -hide_banner -loglevel error -y \
		-f lavfi -i "$source" -t "$seconds" \
		-vf "format=yuv420p,$label,$timer" \
		$codec \
		-g "$rate" -keyint_min "$rate" -sc_threshold 0 \
		-b:v 1000k -maxrate 1000k -bufsize 2000k \
		-an -movflags +faststart \
		"$out/$name.mp4"
	echo "generated $out/$name.mp4"
}

encode cam1 "testsrc2=size=$size:rate=$rate" "$h264"
encode cam2 "life=size=$size:rate=$rate:mold=10:ratio=0.1:death_color=#202040:life_color=#40c0ff" "$h264"
encode cam3 "testsrc=size=$size:rate=$rate" "$h264"
encode cam4 "smptehdbars=size=$size:rate=$rate" "$h265"
