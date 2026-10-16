#!/bin/sh
# Runs MediaMTX (test cameras) and the Go server side by side. If either one
# exits, stop the other and exit non-zero so the platform restarts the
# container. SIGTERM is passed on for a graceful shutdown.
set -u

/mediamtx /mediamtx.yml &
mtx=$!
server &
srv=$!

stop() {
	kill -TERM "$srv" "$mtx" 2>/dev/null
	wait
}
trap 'stop; exit 0' TERM INT

while kill -0 "$srv" 2>/dev/null && kill -0 "$mtx" 2>/dev/null; do
	sleep 1
done

echo "entrypoint: a process exited, stopping the container" >&2
stop
exit 1
