# vndl: notes for Claude

A stateless downloader (Go backend wrapping yt-dlp/ffmpeg, plus a Vite/React SPA).
`README.md` covers the architecture, env vars and one-time server setup. This file
holds the working rules and the operational knowledge the README doesn't.

## Git

- Never commit or push unless explicitly asked. This includes version bumps.
- Conventional Commits, subject line only (`fix:`, `feat:`, `chore:`, `docs:`, ...).
  No body and no co-author or "Generated with" trailers. Prefer one commit.
- `main` is the only long-lived branch. There is no `master`.
- `origin` is `git@gitlab.com:vncius/vndl.git` (SSH). HTTPS has no credentials here.
- **Every push to `main` deploys to production** (see Deploy). Treat a push as a
  release.

## Versioning and changelog

- The version lives only in `frontend/package.json` (`"version"`). `bun.lock`
  doesn't carry it, and the backend has no version.
- SemVer: bug fixes only → patch, new features → minor.
- `CHANGELOG.md` follows Keep a Changelog. Add entries under `## [Unreleased]`
  as work lands. To release, rename that block to `## [X.Y.Z] - YYYY-MM-DD`
  and leave an empty `## [Unreleased]` above it. Write entries in user-facing
  language: what changed for the person using the site.
- Release commit: `chore: release vX.Y.Z`, containing the bump and the changelog.

## Checks before committing

- Backend: `cd backend && go vet ./... && go test ./...`
- Frontend: `cd frontend && bun run lint && bun run format:check && bun run build`.
  `build` is `vite build && tsc -b`, in that order (vite generates the router
  types tsc needs).

## Running the backend locally without Docker

Production pins yt-dlp in `backend/Dockerfile` (`ARG YTDLP_VERSION`). To reproduce production behavior, install that exact
version in a scratch venv and point the server at it:

```sh
python3 -m venv <scratch>/venv && <scratch>/venv/bin/pip install yt-dlp==<YTDLP_VERSION>
cd backend && go build -o <scratch>/vndl ./cmd/server
PORT=18080 YTDLP_PATH=<scratch>/venv/bin/yt-dlp RATE_LIMIT_RPS=50 RATE_LIMIT_BURST=50 <scratch>/vndl
```

API flow: `POST /api/probe {"url"}` → `POST /api/downloads {"url","format_id","title","container"}`
→ `GET /api/downloads/{job_id}/file`. When reproducing a yt-dlp error, pass the
same flags as `baseArgs`/`ytDlpArgs` in `backend/internal/downloader/ytdlp.go`.
Flags change error messages: e.g. `--no-playlist` turns a `/video/1` link's
"No video could be found" into "Video #1 is unavailable".

Stop a background server by PID (`kill $(cat pidfile)`), not `pkill -f <pattern>`:
the pattern also matches the shell running the command and kills it.

## Twitter/X sensitive-media fallback

yt-dlp can't see age-restricted tweets logged out. `downloader.NeedsFxFallback`
matches yt-dlp's error text (`sensitiveMediaRe` in `fxtwitter.go`). On a match,
the probe is redone through `api.fxtwitter.com`, and the formats come back as
`fx-<bitrate>`. On download, `stream.go` resolves `fx-` formats to a direct
`video.twimg.com` URL and streams it through yt-dlp's generic extractor.

- If age-restricted tweets break, first check the production logs for the probe
  error text. A new yt-dlp wording means `sensitiveMediaRe` needs updating.
- Never log tweet IDs or URLs (privacy rule across the whole backend: only the
  platform host is logged).
- Known age-restricted test tweets are in yt-dlp's own extractor tests
  (`'age_limit': 18` entries in `yt_dlp/extractor/twitter.py`), e.g.
  `https://x.com/s2FAKER/status/1621117700482416640`.

## Deploy

- `.gitlab-ci.yml` deploys on every push to `main`. A self-hosted shell runner
  (tag `home-server`) on this same machine runs `docker compose up -d --build`
  in its own checkout (`/home/gitlab-runner/builds/...`), with the env file from
  the `VNDL_ENV_FILE` CI variable. This clone and its `.env` are **not** what
  production uses.
- Compose project name is `vndl`. Containers are `vndl-backend-1` and
  `vndl-frontend-1`. The frontend (Caddy) is on `127.0.0.1:8081` and proxies
  `/api` to the backend. The backend port is never published.
- Production logs: `docker logs --since 24h vndl-backend-1` (ANSI-colored text;
  strip with `sed 's/\x1b\[[0-9;]*m//g'`). Dated log files are also written to
  the `LOGS_DIR` set in the production env file. The repo's `./logs` is stale
  local output.

### Verifying a deploy

1. Before pushing, note `docker inspect -f '{{.State.StartedAt}}' vndl-backend-1 vndl-frontend-1`.
2. After pushing, wait until both start times change (CI takes about a minute).
   The runner's checkout isn't readable by this user, so verify by behavior, not
   by its git HEAD.
3. `curl -s http://127.0.0.1:8081/api/health` should return `{"status":"ok"}`.
4. Exercise the change against `http://127.0.0.1:8081/api/...`, or grep the
   served bundle (`curl -s http://127.0.0.1:8081/` lists `assets/index-*.js`)
   for a frontend change.

### Manual rebuilds (avoid when CI can do it)

- `git fetch` first and confirm `origin/main` is an ancestor of `HEAD`. A rebuild
  from a stale clone rolled production back once (2026-11-04).
- Building from this clone uses this clone's `.env`, which differs from
  production's (file size, jobs per IP, job duration, SSE limits, logs dir).
  For a frontend-only change use
  `docker compose up -d --build --no-deps frontend` so the backend keeps its
  production settings.
