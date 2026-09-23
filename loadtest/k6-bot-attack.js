// Simulates a distributed bot attack: many distinct "client IPs" hammering
// the backend directly (bypassing Caddy), each spoofing its own
// X-Forwarded-For value. Run against docker-compose.loadtest.yml with
// PRODUCTION rate-limit settings (the compose file's defaults: RPS=1,
// burst=5) — the question this answers is "does per-IP rate limiting
// actually protect the server when the attacker controls (or spoofs) the
// IP dimension it keys on?"
//
//   docker compose -f docker-compose.loadtest.yml up -d --build
//   docker run --rm -i --network vndl-loadtest_default -v "$PWD/loadtest:/s" \
//     grafana/k6 run /s/k6-bot-attack.js -e BASE_URL=http://backend:8080

import http from "k6/http";
import { check } from "k6";
import { Counter } from "k6/metrics";

const BASE_URL = __ENV.BASE_URL || "http://localhost:18080";

export const options = {
  scenarios: {
    bots: {
      executor: "constant-vus",
      vus: 300,
      duration: "30s",
    },
  },
};

const accepted = new Counter("probe_accepted");
const rateLimited = new Counter("probe_rate_limited");

function spoofedIp() {
  // A different "attacker" IP per VU per iteration — this is exactly what
  // an attacker controlling the X-Forwarded-For header can do trivially.
  return `${__VU % 256}.${(__VU * 7) % 256}.${__ITER % 256}.${(__VU + __ITER) % 256}`;
}

export default function () {
  const res = http.post(
    `${BASE_URL}/api/probe`,
    JSON.stringify({ url: `https://www.youtube.com/watch?v=bot${__VU}_${__ITER}` }),
    {
      headers: {
        "Content-Type": "application/json",
        "X-Forwarded-For": spoofedIp(),
      },
    },
  );
  if (res.status === 429) {
    rateLimited.add(1);
  } else {
    accepted.add(1);
  }
  check(res, { "not a server error": (r) => r.status < 500 });
}
