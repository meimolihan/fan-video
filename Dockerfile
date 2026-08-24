# syntax=docker/dockerfile:1.6
# fan-video: 轻量级媒体库管理服务
# 仅支持本地海报匹配（同名图片 + 视频第一帧兜底）
# 无网络刮削、无 AI 功能、无元数据管理
# 基于 nowen-video server-lite 重构精简而来

FROM --platform=$BUILDPLATFORM node:20-alpine AS frontend
ARG NOWEN_VERSION=1.0.4
WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci
COPY web/ .
ENV VITE_APP_VERSION=${NOWEN_VERSION}
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS backend
ARG TARGETOS
ARG TARGETARCH
ARG NOWEN_VERSION=1.0.4
WORKDIR /app
ENV GOPROXY=https://goproxy.cn,https://goproxy.io,direct
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
      -ldflags="-s -w -X github.com/fan-video/fan-video/internal/version.Version=${NOWEN_VERSION}" \
      -o fan-video ./cmd/server-lite

FROM alpine:3.24
ARG TARGETARCH
ARG NOWEN_VERSION=1.0.4
ARG FFMPEG_VERSION=8.1.2-r0

# Keep the runtime dependency surface minimal. Alpine's BusyBox already
# provides the health-check client and standard process utilities we need.
RUN apk add --no-cache \
      "ffmpeg=${FFMPEG_VERSION}" \
      tzdata \
      ca-certificates \
      su-exec \
    && ffmpeg -version | head -n 1 | grep -F "ffmpeg version 8.1.2" \
    && ffprobe -version | head -n 1 | grep -F "ffprobe version 8.1.2"

# Hardware acceleration drivers are part of the official image because direct
# play, remux and on-demand fallback transcoding are core playback capabilities.
# libva-utils is intentionally omitted: it only provides diagnostics such as
# vainfo and is not required by FFmpeg for VA-API/QSV runtime acceleration.
RUN set -eux; \
    if [ "${TARGETARCH}" = "amd64" ]; then \
      apk add --no-cache intel-media-driver libva-intel-driver mesa-va-gallium; \
    else \
      apk add --no-cache mesa-va-gallium; \
    fi

RUN addgroup -S nowen && adduser -S nowen -G nowen
WORKDIR /app
COPY --from=backend /app/fan-video /usr/local/bin/fan-video
COPY --from=frontend /app/web/dist /app/web/dist
COPY --from=frontend /app/web/public/assets /app/web/dist/assets
COPY scripts/docker-entrypoint.sh /entrypoint.sh

RUN mkdir -p /data /cache /media \
    && chown -R nowen:nowen /data /cache /media \
    && chmod +x /entrypoint.sh

ENV NOWEN_APP_PORT=8080
ENV NOWEN_APP_DATA_DIR=/data
ENV NOWEN_APP_WEB_DIR=/app/web/dist
ENV NOWEN_DATABASE_DB_PATH=/data/nowen.db
ENV NOWEN_CACHE_CACHE_DIR=/cache
ENV NOWEN_LOGGING_LEVEL=info
ENV NOWEN_VERSION=${NOWEN_VERSION}
ENV TZ=Asia/Shanghai

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD /bin/busybox wget -q -O /dev/null http://localhost:8080/api/health || exit 1

CMD ["/entrypoint.sh"]
