# Changelog

All notable changes to this project are documented here. Format loosely
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versioning follows [Semantic Versioning](https://semver.org/).

## [1.10.1] - 2026-09-22

### Fixed

- No download options appeared for sites that don't report a video codec
  per format (e.g. xvideos, xnxx) — an unknown codec was treated as
  audio-only and filtered out.
- A download whose video fragments the site refuses to serve (currently
  Redtube, upstream in yt-dlp) no longer stalls silently for minutes; it
  now fails within seconds with a clear message.

## [1.10.0] - 2026-09-22

### Added

- A tap-to-open info tooltip next to the NSFW toggle, listing the sites it
  unlocks — the old `title` attribute never worked on mobile, since there's
  no hover to trigger it.

### Changed

- The `$` prompt marker (header, and each `$ cmd` line in the download
  console) now uses the same green accent as the OG preview image, instead
  of plain foreground color.

## [1.9.0] - 2026-09-22

### Added

- SEO groundwork for publishing: meta description, canonical URL,
  Open Graph/Twitter card tags, JSON-LD `WebApplication` data,
  `robots.txt`, `sitemap.xml`, and an OG preview image.

## [1.8.1] - 2026-09-22

### Fixed

- Log files now bind-mount to `./logs` at the repo root instead of a
  named Docker volume, so they're readable directly (`cat
  logs/vndl-2026-09-23.log`) without `docker compose exec`. Also fixed a
  silent failure this exposed: a bind-mounted directory the container
  couldn't write to made file logging quietly no-op instead of erroring;
  it now logs a clear startup warning explaining why and how to fix it.

## [1.8.0] - 2026-09-22

### Added

- Persistent, privacy-first log files: dated files (`vndl-YYYY-MM-DD.log`)
  on a mounted volume, auto-rotated daily and pruned after
  `LOG_RETENTION_DAYS` (default 30) — logs survive a container rebuild,
  unlike stdout capture, which is lost with it. Same logging rules as
  before: only the platform host is ever recorded, never a URL or title.

## [1.7.3] - 2026-09-22

### Changed

- Removed an unused icon asset.

## [1.7.2] - 2026-09-22

### Changed

- Condensed the README — trimmed to what's needed to understand, develop,
  and deploy the project; dropped the deep design-rationale prose (now
  implicit in code comments and this changelog) and removed explicit
  adult site names in favor of a generic mention.

## [1.7.1] - 2026-09-22

### Added

- The app version is now shown in the footer, linking to this changelog
  on GitLab.

## [1.7.0] - 2026-09-22

### Added

- Probe result caching: a repeated identical `/probe` request (same link
  pasted twice, a page reload, a viral link probed by many visitors)
  is served from memory instead of re-running yt-dlp
  (`PROBE_CACHE_TTL`, default 10m).

## [1.6.3] - 2026-09-22

### Fixed

- Video+audio merges on platforms without an `ext=m4a` audio format
  (e.g. Twitter/X) could silently fall back to an audio-only download
  with no video at all.
- Default quality selection picked the lowest resolution instead of the
  highest on platforms that don't report `filesize` per format.

## [1.6.2] - 2026-09-22

### Changed

- Documented that the project is developed with heavy AI assistance,
  guided throughout by a developer who reviews and directs the
  implementation.

## [1.6.1] - 2026-09-22

### Fixed

- Probe/download failures for private, deleted, or age-restricted
  content returned HTTP 502, which Cloudflare's edge silently replaced
  with its own generic error page instead of the real message. Switched
  to 422 and added specific messages for known failure patterns.

## [1.6.0] - 2026-09-22

### Added

- Periodic progress log lines during long downloads/encodes, instead of
  only a single line at completion.
- Server-side smoothing of yt-dlp's own jittery ETA estimate.
- A ticking elapsed-time indicator and scanner animation in place of a
  static "merging/encoding" message on the frontend.

## [1.5.1] - 2026-09-22

### Fixed

- Client IP attribution (used for rate limiting and logs) picked up
  cloudflared's own address instead of the real visitor's, once a
  Cloudflare Tunnel was added in front of Caddy. Now prefers
  `CF-Connecting-IP`.

## [1.5.0] - 2026-09-22

### Added

- Color-accented, human-readable log output by default, replacing raw
  JSON lines (`LOG_FORMAT=json` to opt back into structured output).

## [1.4.1] - 2026-09-22

### Fixed

- CI deploy health check didn't retry on a cold-start connection reset,
  only on connection-refused.

## [1.4.0] - 2026-09-22

### Changed

- Reverted self-signed LAN HTTPS in favor of terminating TLS via a
  Cloudflare Tunnel.

## [1.3.1] - 2026-09-22

### Fixed

- CI deploy failed outright on a fresh runner workspace with no `.env`,
  instead of falling back to sane defaults.

## [1.3.0] - 2026-09-22

### Added

- Self-signed HTTPS for LAN access via Caddy's local CA.
- Automatic deployment on push to `main` via a self-hosted GitLab
  Runner.

## [1.2.2] - 2026-09-22

### Security

- Hardened several issues found in a security audit: unbounded
  job/memory growth, unbounded SSE connections, a concurrency slot held
  past the actual subprocess's lifetime, missing request body size
  limits, unvalidated format/container fields, and a URL-fragment
  "smuggling" vector.

## [1.2.1] - 2026-09-22

### Changed

- Documented load testing methodology and production hardening
  decisions.

## [1.2.0] - 2026-09-22

### Added

- Server-wide concurrency cap on yt-dlp/ffmpeg processes and explicit
  HTTP server timeouts.

## [1.1.1] - 2026-09-22

### Security

- Fixed per-IP rate limiting being trivially bypassable by spoofing
  `X-Forwarded-For` against a directly-exposed backend port.

## [1.1.0] - 2026-09-22

### Added

- A fake yt-dlp harness for load testing the backend without hitting
  any real video platform.

## [1.0.4] - 2026-09-22

### Changed

- Footer buttons now consistently sized.

## [1.0.3] - 2026-09-22

### Added

- Repository link icon in the footer.

## [1.0.2] - 2026-09-22

### Changed

- Updated favicon.

## [1.0.1] - 2026-09-22

### Fixed

- Frontend build order — the route tree must be generated before
  typechecking, not after.

## [1.0.0] - 2026-09-22

### Added

- Initial release: Go backend wrapping yt-dlp/ffmpeg, React frontend
  download console, Docker Compose deployment, README, GPL-3.0 license,
  configurable ports via `.env`.
