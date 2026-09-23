// Raw capacity test: how much probe/download traffic can the backend
// actually absorb before it degrades? Run against the fake-yt-dlp backend
// (docker-compose.loadtest.yml) with rate limiting effectively disabled
// (RATE_LIMIT_RPS/BURST set very high) so we're measuring the server's own
// ceiling, not the limiter cutting requests off early.
//
//   docker compose -f docker-compose.loadtest.yml up -d --build
//   RATE_LIMIT_RPS=100000 RATE_LIMIT_BURST=100000 \
//     docker compose -f docker-compose.loadtest.yml up -d --build backend
//   docker run --rm -i --network vndl-loadtest_default -v "$PWD/loadtest:/s" \
//     grafana/k6 run /s/k6-throughput.js -e BASE_URL=http://backend:8080

import http from "k6/http";
import { check } from "k6";
import { Rate, Trend } from "k6/metrics";

const BASE_URL = __ENV.BASE_URL || "http://localhost:18080";

export const options = {
  scenarios: {
    probe_ramp: {
      executor: "ramping-vus",
      exec: "probe",
      startVUs: 0,
      stages: [
        { duration: "15s", target: 50 },
        { duration: "20s", target: 200 },
        { duration: "20s", target: 500 },
        { duration: "15s", target: 0 },
      ],
    },
    download_ramp: {
      executor: "ramping-vus",
      exec: "download",
      startVUs: 0,
      startTime: "70s", // after probe_ramp finishes, so results don't conflate
      stages: [
        { duration: "15s", target: 20 },
        { duration: "20s", target: 80 },
        { duration: "20s", target: 200 },
        { duration: "15s", target: 0 },
      ],
    },
  },
  thresholds: {
    http_req_failed: ["rate<0.5"], // just a smoke ceiling, not a pass/fail gate
  },
};

const probeErrors = new Rate("probe_errors");
const downloadErrors = new Rate("download_errors");
const downloadDuration = new Trend("full_download_duration");

function fakeUrl() {
  return `https://www.youtube.com/watch?v=lt${__VU}_${__ITER}`;
}

export function probe() {
  const res = http.post(
    `${BASE_URL}/api/probe`,
    JSON.stringify({ url: fakeUrl() }),
    { headers: { "Content-Type": "application/json" } },
  );
  const ok = check(res, { "probe 200": (r) => r.status === 200 });
  probeErrors.add(!ok);
}

export function download() {
  const start = Date.now();
  const createRes = http.post(
    `${BASE_URL}/api/downloads`,
    JSON.stringify({
      url: fakeUrl(),
      format_id: "137+bestaudio/best",
      audio_only: false,
      title: "loadtest",
      ext: "mp4",
      container: "mp4",
    }),
    { headers: { "Content-Type": "application/json" } },
  );
  if (!check(createRes, { "create 201": (r) => r.status === 201 })) {
    downloadErrors.add(1);
    return;
  }
  const jobId = createRes.json("job_id");
  const fileRes = http.get(`${BASE_URL}/api/downloads/${jobId}/file`);
  const ok = check(fileRes, { "file 200": (r) => r.status === 200 });
  downloadErrors.add(!ok);
  downloadDuration.add(Date.now() - start);
}
