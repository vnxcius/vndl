# --- build stage ---
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server

# --- runtime stage ---
FROM python:3.13-alpine
# Pinned deliberately — check yt-dlp's security advisories before bumping.
ARG YTDLP_VERSION=2026.08.19
RUN apk add --no-cache ffmpeg ca-certificates && \
    pip install --no-cache-dir --break-system-packages "yt-dlp==${YTDLP_VERSION}"

# 1000: the common first-regular-user UID, so the ./logs bind mount (see
# docker-compose.yml) is usually already writable without a manual chown.
RUN addgroup -g 1000 -S vndl && adduser -S -u 1000 -G vndl vndl
COPY --from=build /out/server /usr/local/bin/server

USER vndl
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["server"]
