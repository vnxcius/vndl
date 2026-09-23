# vndl

A stateless video/audio downloader for YouTube, Twitter/X, TikTok and
Instagram (plus adult sites behind an opt-in NSFW toggle). Paste a link,
pick a quality (or extract audio as mp3), download. Nothing is stored
server-side — no database, no persisted files.

> **Note:** this project is heavily vibe coded — most of the
> implementation was written with Claude (Sonnet 5) — but guided end to
> end by a developer who understands what the code is actually doing.

## Stack

- **Frontend**: Vite + React, TanStack Router/Query, Tailwind v4,
  shadcn/ui on Base UI. Pure client-side SPA. Package manager: bun.
- **Backend**: Go, stdlib `net/http` only. Wraps `yt-dlp` + `ffmpeg`.
- **Deployment**: Docker Compose (`docker-compose.yml`) for both services.

## How it works

- No database — job state lives in memory (`backend/internal/jobs`),
  keyed by a random ID, and expires a few minutes after the download
  finishes.
- `POST /api/downloads` creates a job. The client opens an SSE stream
  (`/events`) for live progress and requests `/file`, which starts
  `yt-dlp`, writes to a per-job temp directory, streams the result back,
  then deletes everything — nothing produced by yt-dlp is kept past that
  one request.
- Per-IP rate limiting and a server-wide cap on concurrent yt-dlp/ffmpeg
  processes, both in-memory, no external dependencies.
- Successful `/probe` results are cached in memory for a few minutes so
  the same link isn't re-probed repeatedly.
- Sensitive (age-restricted) tweets, which yt-dlp can't see logged out,
  fall back to the public [fxtwitter](https://github.com/FxEmbed/FxEmbed)
  API. Only the tweet ID is sent, from the server — never the visitor's IP
  — and only when yt-dlp has already failed for that reason.
- Only the platform host is ever logged (e.g. `www.youtube.com`) — never
  the URL or title. Logs are color-accented text on stdout by default
  (`LOG_FORMAT=json` for structured output instead).
- Optional dated log files (`LOG_DIR`), bind-mounted to `./logs` at the
  repo root by default — `cat logs/vndl-2026-09-23.log`, no `docker
  compose exec` needed. History survives a container rebuild this way;
  stdout capture alone doesn't. Auto-pruned after `LOG_RETENTION_DAYS`.
  If the container can't write there, `mkdir -p logs` on the host first
  (a fresh Docker-created directory is root-owned).
- Only YouTube/Twitter/TikTok/Instagram (plus opt-in adult sites) are
  accepted; the NSFW toggle is a client-side preference only, not a
  server-side security boundary.

## Development

Backend needs `yt-dlp`/`ffmpeg`, which only exist inside its Docker
image — always run it via Docker unless you install both locally.

```sh
cp .env.example .env
docker compose up --build
```

- Frontend: http://localhost:8081
- Backend: http://localhost:8080

For frontend-only iteration (proxies `/api` to the backend container):

```sh
cd frontend && bun install && bun dev   # http://localhost:5173
```

Frontend build runs `vite build && tsc -b` (in that order — `vite build`
generates the gitignored router types `tsc` needs).

SEO metadata (canonical URL, Open Graph/Twitter tags, `sitemap.xml`,
`robots.txt`) is hardcoded to `dl.vncius.dev` in `frontend/index.html` and
`frontend/public/{robots.txt,sitemap.xml}` — update those if deploying
under a different domain.

HTTPS is handled externally (e.g. a Cloudflare Tunnel) — the app itself
only serves plain HTTP.

### Configuration (env vars)

| var | default | purpose |
| --- | --- | --- |
| `FRONTEND_PORT` | `8081` | host port for the frontend |
| `BACKEND_PORT` | `8080` | only used with a `docker-compose.override.yml` for direct local debugging |
| `PORT` | `8080` | backend listen port inside its container |
| `ALLOWED_ORIGINS` | `http://localhost:5173` | CORS allow-list |
| `RATE_LIMIT_RPS` / `RATE_LIMIT_BURST` | `1` / `5` | per-IP token bucket |
| `MAX_CONCURRENT_YTDLP` | `6` | server-wide cap on concurrent yt-dlp/ffmpeg processes |
| `CONCURRENCY_MAX_WAIT` | `15s` | max wait for a free slot over the cap |
| `JOB_TTL` | `5m` | how long a finished job stays queryable |
| `YTDLP_PATH` / `FFMPEG_PATH` | `yt-dlp` / auto | binary paths |
| `LOG_FORMAT` | `text` | `text` or `json` |
| `PROBE_CACHE_TTL` | `10m` | how long probe results are cached; `0` disables |
| `LOG_DIR` | *(empty = disabled)* | directory for dated log files; `docker-compose.yml` bind-mounts this to `./logs` |
| `LOG_RETENTION_DAYS` | `30` | how many days of log files to keep before deleting |

The backend's port is never published by `docker-compose.yml` — the
frontend (Caddy) is the sole entry point, which per-IP rate limiting
depends on. Client IP is read from `CF-Connecting-IP` (Cloudflare
Tunnel) first, then `X-Forwarded-For`, then the raw TCP peer.

## Deployment

`.gitlab-ci.yml` redeploys on every push to `main`, via a self-hosted
GitLab Runner on the host itself (no inbound access needed). One-time
setup on the server:

1. Install [`gitlab-runner`](https://docs.gitlab.com/runner/install/).
2. Create a project runner in GitLab (tag `home-server`), then register:
   ```sh
   sudo gitlab-runner register --non-interactive \
     --url "https://gitlab.com" --token "<token>" \
     --executor "shell" --description "vndl-home-server"
   ```
3. `sudo usermod -aG docker gitlab-runner && sudo systemctl restart gitlab-runner`

Every push to `main` then rebuilds and restarts the stack automatically.

## Project layout

```bash
backend/
  cmd/server/           .entrypoint
  internal/
    api/                .HTTP handlers
    config/             .env-based config
    downloader/         .yt-dlp process building, format probing, url allow-list
    jobs/               .in-memory job state + SSE fan-out
    logging/            .canonical log line helper
    middleware/         .CORS, rate limiting
frontend/
  src/
    routes/             .TanStack Router file-based routes
    components/         .UI (download-console is the main feature)
    hooks/              .use-download-progress (SSE)
    lib/                .api client, formatting helpers
```

## Changelog

See [CHANGELOG.md](CHANGELOG.md).

## License

[GPL-3.0](LICENSE)
