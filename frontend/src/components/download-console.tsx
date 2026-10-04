import {
  CheckSquareIcon,
  ClipboardTextIcon,
  DownloadSimpleIcon,
  SquareIcon,
  XIcon,
} from "@phosphor-icons/react";
import { useMutation } from "@tanstack/react-query";
import { type SubmitEvent, useEffect, useState } from "react";
import { Dot } from "@/components/dot";
import { ErrorLine, Hint, OkLine, TermButton } from "@/components/term";
import { useDownloadProgress } from "@/hooks/use-download-progress";
import { useNsfwMode } from "@/hooks/use-nsfw-mode";
import {
  cancelDownload,
  createDownload,
  probe,
  saveFile,
  terminalStatuses,
  type Format,
} from "@/lib/api";
import { asciiBar, scannerBar } from "@/lib/ascii";
import { formatBytes, formatDuration } from "@/lib/format";
import { isNsfwUrl } from "@/lib/nsfw";
import { cn } from "@/lib/utils";
import { Prompt } from "./prompt";

export function DownloadConsole() {
  const [url, setUrl] = useState("");
  const [audioOnly, setAudioOnly] = useState(false);
  const [formatId, setFormatId] = useState<string | null>(null);
  const [jobId, setJobId] = useState<string | null>(null);
  const [nsfwBlocked, setNsfwBlocked] = useState(false);
  const { enabled: nsfwMode } = useNsfwMode();

  const probeMutation = useMutation({
    mutationFn: probe,
    onSuccess: (meta) => {
      setFormatId(pickDefaultFormat(meta.formats));
      setAudioOnly(false);
      setJobId(null);
    },
  });

  const downloadMutation = useMutation({
    mutationFn: createDownload,
    onSuccess: (result) => {
      setJobId(result.job_id);
      saveFile(result.job_id);
    },
  });

  const cancelMutation = useMutation({ mutationFn: cancelDownload });

  const progress = useDownloadProgress(jobId);
  const meta = probeMutation.data;
  const videoFormats = meta ? videoOnlyDistinct(meta.formats) : [];
  const selected = videoFormats.find((f) => f.format_id === formatId);

  const isBusy = probeMutation.isPending || downloadMutation.isPending;
  const isDownloading = jobId !== null && !(progress && terminalStatuses.has(progress.status));

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
      // Audio-only still sends the selection: the backend ignores it for yt-dlp,
      // but needs it to route fx- formats through the direct-URL fallback.
      format_id: audioOnly
        ? (selected?.format_id ?? "")
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
    cancelMutation.mutate(jobId);
  }

  return (
    <div className="flex flex-col gap-10">
      <section className="flex flex-col gap-3">
        <Prompt cmd="paste_url" note="a video link" />
        <form onSubmit={handleFetch} className="flex items-end gap-2 sm:gap-4">
          {/* text-base: anything smaller makes iOS Safari zoom in on focus */}
          <input
            id="url"
            type="url"
            inputMode="url"
            aria-label="video link"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="https://…"
            disabled={isBusy}
            className="min-h-11 min-w-0 flex-1 border-b border-border/50 bg-transparent py-2 font-mono text-base transition-colors outline-none placeholder:text-muted-foreground/70 focus:border-foreground disabled:opacity-50"
          />
          <TermButton
            type="button"
            onClick={handlePaste}
            disabled={isBusy}
            title="paste from clipboard"
            aria-label="paste from clipboard"
          >
            <ClipboardTextIcon size={18} />
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
          <Prompt cmd="result" note="tap to open the original" />
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
              <p className="line-clamp-2 text-sm font-medium group-hover:underline">{meta.title}</p>
              <p className="text-sm text-muted-foreground">
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
          <Prompt cmd="select_format" note="pick a quality, or audio only" />

          <TermButton
            onClick={() => setAudioOnly((v) => !v)}
            aria-pressed={audioOnly}
            className={cn("self-start", audioOnly && "bg-foreground text-background")}
          >
            {audioOnly ? <CheckSquareIcon size={18} /> : <SquareIcon size={18} />}
            audio only
            <Dot />
            mp3
          </TermButton>

          {!audioOnly && (
            <ol className="flex max-h-80 flex-col overflow-y-auto border-t border-border/30">
              {videoFormats.map((f, i) => (
                <li key={f.format_id}>
                  <button
                    type="button"
                    onClick={() => setFormatId(f.format_id)}
                    className={cn(
                      "flex min-h-11 w-full cursor-pointer items-center justify-between gap-3 border-b border-border/30 px-2 py-2 text-left text-sm",
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
                    <span className="shrink-0 opacity-75">{formatBytes(f.filesize)}</span>
                  </button>
                </li>
              ))}
            </ol>
          )}

          <TermButton
            variant="primary"
            onClick={handleDownload}
            disabled={isBusy || (!audioOnly && !selected) || isDownloading}
            className="w-full sm:w-fit"
          >
            <DownloadSimpleIcon size={18} weight="bold" />
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
            error={progress?.error}
          />
          {isDownloading && <Hint>your browser is saving it — you can leave this page</Hint>}
          {isDownloading && (
            <TermButton
              onClick={handleCancel}
              disabled={cancelMutation.isPending}
              className="self-start"
            >
              <XIcon size={16} />
              {cancelMutation.isPending ? "canceling…" : "cancel"}
            </TermButton>
          )}
        </section>
      )}
    </div>
  );
}

function ProgressReadout({
  status,
  percent,
  speed,
  eta,
  error,
}: {
  status?: string;
  percent?: number;
  speed?: string;
  eta?: string;
  error?: string;
}) {
  if (status === "error") {
    return <ErrorLine>{error || "the download failed — try another format"}</ErrorLine>;
  }
  if (status === "canceled") {
    return <Hint>canceled</Hint>;
  }
  if (status === "expired") {
    return <ErrorLine>this download expired — start it again</ErrorLine>;
  }
  if (status === "done") {
    return <OkLine>done — check your browser's downloads</OkLine>;
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
      <p className="text-sm text-muted-foreground">
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
      <p className="text-sm text-muted-foreground">merging / encoding… {elapsed} elapsed</p>
    </div>
  );
}

function pickDefaultFormat(formats: Format[]): string | null {
  const best = videoOnlyDistinct(formats)[0];
  return best?.format_id ?? null;
}

function videoOnlyDistinct(formats: Format[]): Format[] {
  return (
    formats
      // Empty vcodec means "unknown", not audio-only — some sites report it for every format.
      .filter((f) => f.vcodec !== "none" && f.resolution !== "audio only")
      .sort((a, b) => (b.height || 0) - (a.height || 0) || (b.filesize || 0) - (a.filesize || 0))
  );
}
