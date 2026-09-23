import { useEffect, useState } from "react";
import { downloadEventsUrl, type ProgressEvent } from "@/lib/api";

export function useDownloadProgress(jobId: string | null): ProgressEvent | null {
  const [event, setEvent] = useState<ProgressEvent | null>(null);

  useEffect(() => {
    setEvent(null);
    if (!jobId) return;

    const source = new EventSource(downloadEventsUrl(jobId));

    source.onmessage = (message) => {
      try {
        setEvent(JSON.parse(message.data) as ProgressEvent);
      } catch {
        // malformed event — skip it, the stream will keep going
      }
    };
    source.onerror = () => source.close();

    return () => source.close();
  }, [jobId]);

  return event;
}
