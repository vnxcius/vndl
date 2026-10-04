const API_BASE = import.meta.env.VITE_API_BASE ?? "";

export interface Format {
  format_id: string;
  ext: string;
  resolution: string;
  format_note: string;
  vcodec: string;
  acodec: string;
  filesize: number;
  height: number;
}

export interface Metadata {
  title: string;
  thumbnail: string;
  duration: number;
  uploader: string;
  formats: Format[];
}

export interface ProgressEvent {
  // "expired" is client-side only: the job is gone from the server.
  status: "downloading" | "processing" | "done" | "error" | "canceled" | "expired";
  percent?: number;
  total?: string;
  speed?: string;
  eta?: string;
  error?: string;
}

export const terminalStatuses: ReadonlySet<ProgressEvent["status"]> = new Set([
  "done",
  "error",
  "canceled",
  "expired",
]);

export interface CreateDownloadInput {
  url: string;
  format_id: string;
  audio_only: boolean;
  title: string;
  ext: string;
  container?: string;
}

export interface CreateDownloadResult {
  job_id: string;
  filename: string;
}

class ApiError extends Error {}

async function readError(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: string };
    return body.error ?? `request failed (${res.status})`;
  } catch {
    return `request failed (${res.status})`;
  }
}

export async function probe(url: string): Promise<Metadata> {
  const res = await fetch(`${API_BASE}/api/probe`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ url }),
  });
  if (!res.ok) throw new ApiError(await readError(res));
  return res.json() as Promise<Metadata>;
}

export async function createDownload(input: CreateDownloadInput): Promise<CreateDownloadResult> {
  const res = await fetch(`${API_BASE}/api/downloads`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  if (!res.ok) throw new ApiError(await readError(res));
  return res.json() as Promise<CreateDownloadResult>;
}

export function downloadFileUrl(jobId: string): string {
  return `${API_BASE}/api/downloads/${jobId}/file`;
}

export function downloadEventsUrl(jobId: string): string {
  return `${API_BASE}/api/downloads/${jobId}/events`;
}

export function downloadCancelUrl(jobId: string): string {
  return `${API_BASE}/api/downloads/${jobId}/cancel`;
}

export async function cancelDownload(jobId: string): Promise<void> {
  await fetch(downloadCancelUrl(jobId), { method: "POST" });
}

// Returns null once the job has expired on the server.
export async function getDownloadStatus(jobId: string): Promise<ProgressEvent | null> {
  const res = await fetch(`${API_BASE}/api/downloads/${jobId}`);
  if (res.status === 404) return null;
  if (!res.ok) throw new ApiError(await readError(res));
  return res.json() as Promise<ProgressEvent>;
}

// A plain link rather than fetch-to-blob: the browser's own download manager
// takes over the transfer, so it survives the tab being backgrounded and can
// resume. Only called once the job is done, so an error body isn't saved.
export function saveFile(jobId: string): void {
  const a = document.createElement("a");
  a.href = downloadFileUrl(jobId);
  a.download = ""; // filename comes from the server's Content-Disposition
  a.click();
}
