import { ClipboardIcon } from "@phosphor-icons/react";
import { useMutation } from "@tanstack/react-query";
import {
  type ButtonHTMLAttributes,
  type ReactNode,
  type SubmitEvent,
  useEffect,
  useRef,
  useState,
} from "react";
import { Dot } from "@/components/dot";
import { useDownloadProgress } from "@/hooks/use-download-progress";
import { useNsfwMode } from "@/hooks/use-nsfw-mode";
import {
  cancelDownload,
  createDownload,
  downloadCancelUrl,
  fetchAndSaveFile,
  probe,
  type Format,
} from "@/lib/api";
import { formatBytes, formatDuration } from "@/lib/format";
import { isNsfwUrl } from "@/lib/nsfw";
import { cn } from "@/lib/utils";

export function DownloadConsole() {
  const [url, setUrl] = useState("");
  const [audioOnly, setAudioOnly] = useState(false);
  const [formatId, setFormatId] = useState<string | null>(null);
  const [jobId, setJobId] = useState<string | null>(null);
  const [nsfwBlocked, setNsfwBlocked] = useState(false);
  const fileAbortRef = useRef<AbortController | null>(null);
  const { enabled: nsfwMode } = useNsfwMode();

  const probeMutation = useMutation({
    mutationFn: probe,
    onSuccess: (meta) => {
      setFormatId(pickDefaultFormat(meta.formats));
      setAudioOnly(false);
      setJobId(null);
    },
  });

  const fileMutation = useMutation({
    mutationFn: ({ jobId, filename }: { jobId: string; filename: string }) => {
      const controller = new AbortController();
      fileAbortRef.current = controller;
      return fetchAndSaveFile(jobId, filename, controller.signal);
    },
  });

  const downloadMutation = useMutation({
    mutationFn: createDownload,
    onSuccess: (result) => {
      setJobId(result.job_id);
      fileMutation.mutate({ jobId: result.job_id, filename: result.filename });
    },
  });

  const cancelMutation = useMutation({ mutationFn: cancelDownload });

  const progress = useDownloadProgress(jobId);
  const meta = probeMutation.data;
  const videoFormats = meta ? videoOnlyDistinct(meta.formats) : [];
  const selected = videoFormats.find((f) => f.format_id === formatId);

  const isBusy = probeMutation.isPending || downloadMutation.isPending;
  const isDownloading =
    jobId !== null &&
    progress?.status !== "done" &&
    progress?.status !== "error" &&
    progress?.status !== "canceled";

  // sendBeacon survives the page actually being torn down, unlike fetch.
  useEffect(() => {
    if (!jobId || !isDownloading) return;
    function cancelOnUnload() {
      navigator.sendBeacon(downloadCancelUrl(jobId!));
    }
    window.addEventListener("pagehide", cancelOnUnload);
    return () => window.removeEventListener("pagehide", cancelOnUnload);
  }, [jobId, isDownloading]);

  async function handlePaste() {
    try {
      const text = await navigator.clipboard.readText();
      if (text.trim()) setUrl(text.trim());
    } catch {
      // clipboard permission denied or unavailable — nothing to do
    }
  }

  function handleFetch(e: SubmitEvent<HTMLFormElement>) {
    e.preventDefault();
    const trimmed = url.trim();
    if (!trimmed) return;

    if (!nsfwMode && isNsfwUrl(trimmed)) {
      setNsfwBlocked(true);
      probeMutation.reset();
      setJobId(null);
      return;
    }

    setNsfwBlocked(false);
    setJobId(null);
    probeMutation.mutate(trimmed);
  }

  function handleDownload() {
    if (!meta) return;
    if (!audioOnly && !selected) return;

    const needsMerge = !audioOnly && selected?.acodec === "none";
    const isAvc = /^(avc1|h264)/i.test(selected?.vcodec ?? "");
    const container = needsMerge ? (isAvc ? "mp4" : "mkv") : (selected?.ext ?? "mp4");

    downloadMutation.mutate({
      url,
      audio_only: audioOnly,
      // Middle fallback matters: some platforms never have an m4a audio
      // track, so without it the selector collapses straight to bare
      // "bestaudio" — audio only, no video, silently.
      format_id: audioOnly
        ? ""
        : needsMerge
          ? isAvc
            ? `${selected!.format_id}+bestaudio[ext=m4a]/${selected!.format_id}+bestaudio/best`
            : `${selected!.format_id}+bestaudio/best`
          : (selected?.format_id ?? ""),
      title: meta.title,
      ext: audioOnly ? "mp3" : container,
      container,
    });
  }

  function handleCancel() {
    if (!jobId) return;
    fileAbortRef.current?.abort();
    cancelMutation.mutate(jobId);
  }

  return (
    <div className="flex flex-col gap-10">
      <section className="flex flex-col gap-3">
        <Prompt cmd="paste_url" />
        <form onSubmit={handleFetch} className="flex items-end gap-4">
          <input
            id="url"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="https://…"
            disabled={isBusy}
            className="flex-1 border-b border-border/40 bg-transparent py-1 font-mono text-sm outline-none placeholder:text-muted-foreground/50 disabled:opacity-50"
          />
          <TermButton
            type="button"
            onClick={handlePaste}
            disabled={isBusy}
            title="paste from clipboard"
          >
            <ClipboardIcon size={14} />
          </TermButton>
          <TermButton type="submit" disabled={isBusy || !url.trim()}>
            {probeMutation.isPending ? "fetching…" : "fetch"}
          </TermButton>
        </form>
        {nsfwBlocked && (
          <ErrorLine>this site requires nsfw mode — enable it in the footer</ErrorLine>
        )}
        {probeMutation.isError && <ErrorLine>{(probeMutation.error as Error).message}</ErrorLine>}
      </section>

      {meta && (
        <section className="flex flex-col gap-4">
          <Prompt cmd="probe_result" />
          <a
            href={probeMutation.variables ?? url}
            target="_blank"
            rel="noopener noreferrer"
            className="group flex cursor-pointer gap-4"
          >
            {meta.thumbnail && (
              <img
                src={meta.thumbnail}
                alt=""
                className="h-20 w-20 shrink-0 object-cover grayscale"
              />
            )}
            <div className="flex min-w-0 flex-col justify-center gap-1">
              <p className="truncate text-sm group-hover:underline">{meta.title}</p>
              <p className="text-xs text-muted-foreground">
                {meta.uploader}
                <Dot />
                {formatDuration(meta.duration)}
              </p>
            </div>
          </a>
        </section>
      )}

      {meta && (
        <section className="flex flex-col gap-4">
          <Prompt cmd="select_format" />

          <TermButton
            onClick={() => setAudioOnly((v) => !v)}
            className={cn("self-start", audioOnly && "bg-foreground text-background")}
          >
            {audioOnly ? "[x]" : " "} audio only
            <Dot />
            mp3
          </TermButton>

          {!audioOnly && (
            <ol className="flex max-h-64 flex-col overflow-y-auto">
              {videoFormats.map((f, i) => (
                <li key={f.format_id}>
                  <button
                    type="button"
                    onClick={() => setFormatId(f.format_id)}
                    className={cn(
                      "flex w-full cursor-pointer items-baseline justify-between border-b border-border/30 py-1.5 text-left text-sm",
                      formatId === f.format_id ? "bg-foreground text-background" : "hover:bg-muted",
                    )}
                  >
                    <span>
                      <span className="text-muted-foreground">
                        {String(i + 1).padStart(2, "0")}
                      </span>{" "}
                      {f.resolution || f.format_note || f.format_id}
                      <Dot />
                      {f.ext}
                    </span>
                    <span className="text-xs opacity-70">{formatBytes(f.filesize)}</span>
                  </button>
                </li>
              ))}
            </ol>
          )}

          <TermButton
            onClick={handleDownload}
            disabled={isBusy || (!audioOnly && !selected) || isDownloading}
            className="self-start"
          >
            {downloadMutation.isPending ? "starting…" : "download"}
          </TermButton>
          {downloadMutation.isError && (
            <ErrorLine>{(downloadMutation.error as Error).message}</ErrorLine>
          )}
        </section>
      )}

      {jobId && (
        <section className="flex flex-col gap-3">
          <Prompt cmd="status" />
          <ProgressReadout
            status={progress?.status}
            percent={progress?.percent}
            speed={progress?.speed}
            eta={progress?.eta}
          />
          {isDownloading && (
            <TermButton
              onClick={handleCancel}
              disabled={cancelMutation.isPending}
              className="self-start"
            >
              {cancelMutation.isPending ? "canceling…" : "cancel"}
            </TermButton>
          )}
        </section>
      )}
    </div>
  );
}

function Prompt({ cmd }: { cmd: string }) {
  return (
    <p className="text-xs text-muted-foreground">
      <span className="text-green-600 dark:text-green-400">$</span> {cmd}
    </p>
  );
}

function ErrorLine({ children }: { children: ReactNode }) {
  return <p className="text-sm text-red-600 dark:text-red-500">error: {children}</p>;
}

function TermButton({ className, children, ...props }: ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      {...props}
      className={cn(
        "inline-flex w-fit cursor-pointer items-center gap-1 font-mono text-sm transition-colors",
        "hover:bg-foreground hover:text-background",
        "disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-40",
        className,
      )}
    >
      <span aria-hidden="true">[</span>
      {children}
      <span aria-hidden="true">]</span>
    </button>
  );
}

function ProgressReadout({
  status,
  percent,
  speed,
  eta,
}: {
  status?: string;
  percent?: number;
  speed?: string;
  eta?: string;
}) {
  if (status === "error") {
    return <ErrorLine>the download failed — try another format</ErrorLine>;
  }
  if (status === "canceled") {
    return <p className="text-sm text-muted-foreground">canceled</p>;
  }
  if (status === "done") {
    return (
      <p className="text-sm text-green-600 dark:text-green-500">
        ok — saved, check your browser downloads
      </p>
    );
  }
  if (status === "processing") {
    return <ProcessingIndicator />;
  }

  const pct = Math.round(percent ?? 0);
  return (
    <div className="flex flex-col gap-1 text-sm">
      <p className="font-mono">
        {asciiBar(pct)} {pct}%
      </p>
      <p className="text-xs text-muted-foreground">
        {speed ?? "--"}
        <Dot />
        eta {eta ?? "--"}
      </p>
    </div>
  );
}

// No real percentage is available during merging/encoding, so this shows
// elapsed time plus a scanner sweep instead of faking a progress bar.
function ProcessingIndicator() {
  const [startedAt] = useState(() => Date.now());
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 200);
    return () => clearInterval(id);
  }, []);

  const elapsedSeconds = Math.floor((now - startedAt) / 1000);
  const mins = Math.floor(elapsedSeconds / 60);
  const secs = elapsedSeconds % 60;
  const elapsed = mins > 0 ? `${mins}:${String(secs).padStart(2, "0")}` : `${secs}s`;

  return (
    <div className="flex flex-col gap-1 text-sm">
      <p className="font-mono">{scannerBar(now)}</p>
      <p className="text-xs text-muted-foreground">merging / encoding… {elapsed} elapsed</p>
    </div>
  );
}

function scannerBar(now: number, width = 24, segment = 4): string {
  const travel = width - segment;
  const cycle = travel * 2;
  const pos = Math.floor(now / 80) % cycle;
  const offset = pos < travel ? pos : cycle - pos;
  return "░".repeat(offset) + "█".repeat(segment) + "░".repeat(width - segment - offset);
}

function asciiBar(percent: number, width = 24): string {
  const clamped = Math.max(0, Math.min(100, percent));
  const filled = Math.round((clamped / 100) * width);
  return "█".repeat(filled) + "░".repeat(width - filled);
}

function pickDefaultFormat(formats: Format[]): string | null {
  const best = videoOnlyDistinct(formats)[0];
  return best?.format_id ?? null;
}

function videoOnlyDistinct(formats: Format[]): Format[] {
  return formats
    // Empty vcodec means "unknown", not audio-only — some sites report it for every format.
    .filter((f) => f.vcodec !== "none" && f.resolution !== "audio only")
    .sort((a, b) => (b.height || 0) - (a.height || 0) || (b.filesize || 0) - (a.filesize || 0));
}
