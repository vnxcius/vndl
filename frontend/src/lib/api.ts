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
  status: "downloading" | "processing" | "done" | "error" | "canceled";
  percent?: number;
  total?: string;
  speed?: string;
  eta?: string;
}

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

// Fetches first (rather than an <a download> straight to /file) so an
// error/cancellation's JSON body isn't saved as if it were the file.
export async function fetchAndSaveFile(
  jobId: string,
  filename: string,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch(downloadFileUrl(jobId), { signal });
  if (!res.ok) throw new ApiError(await readError(res));

  const blob = await res.blob();
  const objectUrl = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = objectUrl;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(objectUrl);
}
