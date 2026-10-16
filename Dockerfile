# Backend image for deployment: the Go server plus the bundled test cameras
# (MediaMTX + looping clips) in one container, so the live demo has working
# rtsp://localhost:8554/cam1-4 URLs. Build from the repo root:
#   docker build -t skylark-rtsp/backend .

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# The MediaMTX FFmpeg image already has FFmpeg (with libx264 for the H.265
# transcode fallback) and MediaMTX, so it doubles as the runtime base.
FROM bluenviron/mediamtx:1.21.2-ffmpeg

RUN apk add --no-cache font-dejavu

COPY streams/generate-clips.sh streams/publish.sh /opt/streams/
# Strip CRLF in case the scripts were checked out on Windows.
RUN sed -i 's/\r$//' /opt/streams/*.sh && sh /opt/streams/generate-clips.sh /opt/streams/clips
COPY streams/mediamtx.yml /mediamtx.yml

COPY --from=build /out/server /usr/local/bin/server
COPY deploy/entrypoint.sh /entrypoint.sh
RUN sed -i 's/\r$//' /entrypoint.sh && chmod +x /entrypoint.sh

# Public server defaults: block internal stream URLs except the bundled
# cameras. ALLOWED_ORIGINS must be set to the frontend's URL at deploy time.
ENV PORT=8080 \
    FFMPEG_PATH=ffmpeg \
    BLOCK_PRIVATE_URLS=true \
    ALLOWED_STREAM_HOSTS=localhost:8554

EXPOSE 8080
ENTRYPOINT ["/entrypoint.sh"]
