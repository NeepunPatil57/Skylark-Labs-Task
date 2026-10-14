#!/bin/sh
# Started by MediaMTX (runOnInit) for each path. MediaMTX sets MTX_PATH and
# RTSP_PORT. Loops the pre-encoded clip forever without re-encoding.
set -eu

clip=/opt/streams/clips/$MTX_PATH.mp4

exec ffmpeg -hide_banner -loglevel warning \
	-re -stream_loop -1 -i "$clip" \
	-c copy -f rtsp -rtsp_transport tcp \
	"rtsp://localhost:$RTSP_PORT/$MTX_PATH"
