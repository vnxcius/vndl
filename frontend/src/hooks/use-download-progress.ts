import { useEffect, useState } from "react";
import {
  downloadEventsUrl,
  getDownloadStatus,
  type ProgressEvent,
  terminalStatuses,
} from "@/lib/api";

const maxRetryDelayMs = 10_000;

// Mobile browsers freeze or kill a backgrounded tab's connections, so a
// dropped stream is reconnected, after first asking the server whether the
// job ended while nobody was listening. Returning to the tab retries at once.
export function useDownloadProgress(jobId: string | null): ProgressEvent | null {
  const [event, setEvent] = useState<ProgressEvent | null>(null);

  useEffect(() => {
    setEvent(null);
    if (!jobId) return;

    let source: EventSource | null = null;
    let retryTimer: ReturnType<typeof setTimeout> | undefined;
    let retryDelay = 1000;
    let finished = false;

    function finish(ev: ProgressEvent) {
      finished = true;
      source?.close();
      clearTimeout(retryTimer);
      setEvent(ev);
    }

    function connect() {
      clearTimeout(retryTimer);
      source?.close();
      source = new EventSource(downloadEventsUrl(jobId!));

      source.onopen = () => {
        retryDelay = 1000;
      };
      source.onmessage = (message) => {
        try {
          const ev = JSON.parse(message.data) as ProgressEvent;
          if (terminalStatuses.has(ev.status)) finish(ev);
          else setEvent(ev);
        } catch {
          // malformed event — skip it, the stream will keep going
        }
      };
      source.onerror = () => {
        source?.close();
        if (!finished) void recover();
      };
    }

    async function recover() {
      try {
        const ev = await getDownloadStatus(jobId!);
        if (finished) return;
        if (ev === null) return finish({ status: "expired" });
        if (terminalStatuses.has(ev.status)) return finish(ev);
      } catch {
        // offline or server unreachable — retry below
      }
      if (finished) return;
      clearTimeout(retryTimer);
      retryTimer = setTimeout(connect, retryDelay);
      retryDelay = Math.min(retryDelay * 2, maxRetryDelayMs);
    }

    function onVisible() {
      if (document.visibilityState !== "visible" || finished) return;
      if (source?.readyState === EventSource.OPEN) return;
      retryDelay = 1000;
      void recover();
    }

    connect();
    document.addEventListener("visibilitychange", onVisible);

    return () => {
      finished = true;
      clearTimeout(retryTimer);
      source?.close();
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [jobId]);

  return event;
}
