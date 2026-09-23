# Load testing

Tests the backend's HTTP handling, rate limiting, and concurrency control
under heavy load — **without a single request ever reaching YouTube,
TikTok, Twitter/X, Instagram, or any other real site.** `cmd/fakeytdlp`
stands in for real `yt-dlp`: it understands just enough of yt-dlp's CLI
surface (the exact invocations `internal/downloader` builds) to answer
probes instantly and simulate a download's progress output and output
file, with zero network calls.

## Running it

```sh
docker compose -f docker-compose.loadtest.yml up -d --build
```

This builds `backend/Dockerfile.loadtest` (server + fakeytdlp, no python/
ffmpeg/yt-dlp at all) and fronts it with Caddy exactly like production
(`docker-compose.yml`) — the backend's port is not published, Caddy is the
only entry point, on `:18080`. A `backend-direct` service also exists,
published on `:18081`, for the raw-ceiling test below, which deliberately
bypasses Caddy on purpose.

Then run either scenario with [k6](https://k6.io):

```sh
# raw capacity ceiling — targets the backend directly, rate limiting
# effectively disabled via env, to measure the server's own limits
RATE_LIMIT_RPS=100000 RATE_LIMIT_BURST=100000 \
  docker compose -f docker-compose.loadtest.yml up -d --build backend
docker run --rm --network vndl-loadtest_default -v "$PWD/loadtest:/s" \
  grafana/k6 run /s/k6-throughput.js -e BASE_URL=http://backend-direct:8080

# bot/spoofing attack simulation — targets Caddy, production rate-limit
# defaults (RPS=1, burst=5)
docker compose -f docker-compose.loadtest.yml up -d --build backend
docker run --rm --network vndl-loadtest_default -v "$PWD/loadtest:/s" \
  grafana/k6 run /s/k6-bot-attack.js -e BASE_URL=http://caddy:80
```

Tunable via env before `up`: `RATE_LIMIT_RPS`, `RATE_LIMIT_BURST`,
`MAX_CONCURRENT_YTDLP`, `FAKE_YTDLP_DELAY_MS` (per-step delay in the
simulated download, default 40ms), `FAKE_YTDLP_FILE_BYTES` (simulated
output size, default 2MB).

Tear down with `docker compose -f docker-compose.loadtest.yml down`.

## What this found (and fixed)

**Per-IP rate limiting was completely bypassable.** The backend's port
was published directly to the host, and `ClientIP()` trusted whatever
`X-Forwarded-For` a client sent, unparsed. `k6-bot-attack.js` — 300
virtual users, each sending a different spoofed `X-Forwarded-For` per
request, straight at the exposed backend port — got **155,259 requests
through in 30 seconds, 0 rate-limited**, against `/api/probe`, an
endpoint that spawns a subprocess per call. A single machine, no botnet
needed.

Fixed two ways, together (see `docker-compose.yml` and
`internal/middleware/ratelimit.go` for the detailed reasoning):

1. The backend's port is no longer published — Caddy is the only entry
   point, over the internal compose network.
2. `ClientIP()` now takes the *rightmost* entry of `X-Forwarded-For` (the
   one Caddy itself appends, since Caddy adds to the header rather than
   passing a client-supplied value through) instead of the raw header.

Re-run against the fixed stack (`k6-bot-attack.js` via Caddy, same
attack pattern, production rate-limit defaults): **931 → 198 requests
accepted** out of 100k+, the rest correctly rejected with 429. Numbers
moved between runs because of machine noise, not the fix; both runs
landed under 1% acceptance versus 100% before.

**No global concurrency cap.** Per-IP limiting doesn't stop a
*distributed* flood — many distinct real IPs, each individually within
its own limit, could still collectively spawn unbounded concurrent
yt-dlp/ffmpeg processes. Added `MAX_CONCURRENT_YTDLP` (default 6), a
server-wide cap on `/api/probe` and `/api/downloads/{id}/file`
regardless of caller identity. First implementation rejected anything
over the cap immediately, which turned a burst of 15 *legitimate*
concurrent requests against a cap of 3 into 12 instant failures; changed
it to queue for a bounded wait (`CONCURRENCY_MAX_WAIT`, default 15s)
instead, which let 12 of the same 15 complete in turn and only the
genuine overflow time out.

**No `http.Server` timeouts.** `http.ListenAndServe` uses the zero-value
`http.Server`, which has no read/write/idle timeouts — a slow or
malformed client can hold a connection (and a goroutine) open
indefinitely (Slowloris). Added `ReadHeaderTimeout`/`ReadTimeout`/
`IdleTimeout`, deliberately *not* `WriteTimeout`: `/file` legitimately
holds a response open for as long as the download takes plus however
long a slow client takes to receive a large file, and a blanket write
deadline would cut off real downloads, not just attacks.

**Raw capacity, once the above weren't in the way:** ~2,600 req/s
sustained across a mixed probe+full-download workload (500 VUs probing,
200 VUs doing full create→file cycles), 0% errors, memory back to
baseline and zero leftover scratch directories after the run. This is
on ordinary dev hardware — the point wasn't to find a specific ceiling
number (that depends entirely on the box it runs on) but to confirm
nothing leaks or falls over under sustained concurrent load. It didn't.

**Not tested, and out of scope for this harness:** actual yt-dlp/ffmpeg
CPU and memory cost under concurrent *real* downloads (the fake binary is
intentionally cheap — that's what makes it safe to hammer). If you're
sizing hardware for production, `MAX_CONCURRENT_YTDLP` is the knob that
matters most; start low (4-6) and raise it only if the box has CPU/memory
to spare, since each real yt-dlp+ffmpeg invocation is genuinely heavy in
a way the fake one deliberately isn't.
