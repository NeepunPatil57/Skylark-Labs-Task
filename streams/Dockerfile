# Test cameras: MediaMTX RTSP server plus looping sample clips.
FROM bluenviron/mediamtx:1.21.2-ffmpeg

RUN apk add --no-cache font-dejavu

COPY generate-clips.sh publish.sh /opt/streams/
# Strip CRLF in case the scripts were checked out on Windows.
RUN sed -i 's/\r$//' /opt/streams/*.sh && sh /opt/streams/generate-clips.sh /opt/streams/clips

COPY mediamtx.yml /mediamtx.yml

EXPOSE 8554
