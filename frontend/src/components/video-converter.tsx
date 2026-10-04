import { ArrowsClockwiseIcon, XIcon } from "@phosphor-icons/react";
import { useEffect, useRef, useState } from "react";
import { ConvertedResult } from "@/components/converted-result";
import { Dot } from "@/components/dot";
import { FilePicker } from "@/components/file-picker";
import { Prompt } from "@/components/prompt";
import { ErrorLine, Hint, TermButton } from "@/components/term";
import { useConvertedFile } from "@/hooks/use-converted-file";
import { asciiBar, scannerBar } from "@/lib/ascii";
import { formatDuration } from "@/lib/format";
import { renameExt, saveUrl } from "@/lib/save";
import { cn } from "@/lib/utils";
import {
  convertVideo,
  isVideoConverterLoaded,
  maxVideoBytes,
  type VideoFormat,
  videoFormats,
} from "@/lib/video-convert";

// Extensions whose MIME type browsers often don't know, so "video/*" alone
// would hide them in the file dialog.
const accept = "video/*,.mkv,.avi,.mov,.m4v,.flv,.wmv,.3gp,.ts,.mts";

type Phase = "idle" | "loading" | "converting" | "done" | "error" | "canceled";

export function VideoConverter() {
  const [file, setFile] = useState<File | null>(null);
  const [format, setFormat] = useState<VideoFormat>("mp4");
  const [phase, setPhase] = useState<Phase>("idle");
  const [progress, setProgress] = useState(0);
  const [startedAt, setStartedAt] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useConvertedFile();
  const abortRef = useRef<AbortController | null>(null);

  const busy = phase === "loading" || phase === "converting";

  // Leaving the page mid-conversion would silently throw the work away.
  useEffect(() => {
    if (!busy) return;
    const warn = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [busy]);

  useEffect(() => () => abortRef.current?.abort(), []);

  function handleFile(picked: File) {
    setFile(picked);
    setResult(null);
    setError(null);
    setPhase("idle");
    if (picked.size > maxVideoBytes) {
      setError("this file is too large to convert in the browser (max 2 GiB)");
    }
  }

  async function handleConvert() {
    if (!file) return;
    const controller = new AbortController();
    abortRef.current = controller;
    setError(null);
    setResult(null);
    setProgress(0);
    setPhase(isVideoConverterLoaded() ? "converting" : "loading");
    setStartedAt(Date.now());

    try {
      const blob = await convertVideo(
        file,
        format,
        {
          onLoaded: () => {
            setPhase("converting");
            setStartedAt(Date.now());
          },
          onProgress: setProgress,
        },
        controller.signal,
      );
      const converted = setResult({ blob, name: renameExt(file.name, format) });
      setPhase("done");
      if (converted) saveUrl(converted.url, converted.name);
    } catch (err) {
      if (controller.signal.aborted) {
        setPhase("canceled");
      } else {
        setError((err as Error).message);
        setPhase("error");
      }
    }
  }

  return (
    <div className="flex flex-col gap-10">
      <section className="flex flex-col gap-3">
        <Prompt cmd="choose_video" note="mp4, mov, mkv, webm, avi…" />
        <FilePicker accept={accept} file={file} disabled={busy} onFile={handleFile} />
      </section>

      {file && file.size <= maxVideoBytes && (
        <section className="flex flex-col gap-4">
          <Prompt cmd="convert_to" />
          <ol className="flex flex-col border-t border-border/30" aria-label="output format">
            {(Object.keys(videoFormats) as VideoFormat[]).map((f) => (
              <li key={f}>
                <button
                  type="button"
                  onClick={() => setFormat(f)}
                  disabled={busy}
                  aria-pressed={format === f}
                  className={cn(
                    "flex min-h-11 w-full cursor-pointer items-center justify-between gap-3 border-b border-border/30 px-2 py-2 text-left text-sm disabled:cursor-not-allowed",
                    format === f ? "bg-foreground text-background" : "hover:bg-muted",
                  )}
                >
                  <span className="font-semibold">{f}</span>
                  <span className="text-right opacity-75">{videoFormats[f].note}</span>
                </button>
              </li>
            ))}
          </ol>

          <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
            <TermButton
              variant="primary"
              onClick={handleConvert}
              disabled={busy}
              className="w-full sm:w-fit"
            >
              <ArrowsClockwiseIcon size={18} weight="bold" />
              {busy ? "converting…" : `convert to ${format}`}
            </TermButton>
            {busy && (
              <TermButton onClick={() => abortRef.current?.abort()} className="self-start">
                <XIcon size={16} />
                cancel
              </TermButton>
            )}
          </div>
        </section>
      )}

      {(phase !== "idle" || error) && (
        <section className="flex flex-col gap-3">
          <Prompt cmd="status" />
          {phase === "loading" && <LoadingReadout />}
          {phase === "converting" && (
            <ConvertingReadout progress={progress} startedAt={startedAt} />
          )}
          {busy && (
            <Hint>runs on your device — keep this tab open and in front until it's done</Hint>
          )}
          {phase === "canceled" && <Hint>canceled</Hint>}
          {error && <ErrorLine>{error}</ErrorLine>}
          {phase === "done" && result && file && (
            <ConvertedResult result={result} original={file} />
          )}
        </section>
      )}
    </div>
  );
}

function useNow(intervalMs: number): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return now;
}

function LoadingReadout() {
  const now = useNow(80);
  return (
    <div className="flex flex-col gap-1 text-sm">
      <p className="font-mono">{scannerBar(now)}</p>
      <p className="text-sm text-muted-foreground">
        loading the converter (~32 MB, first time only)…
      </p>
    </div>
  );
}

function ConvertingReadout({ progress, startedAt }: { progress: number; startedAt: number }) {
  const now = useNow(500);
  const elapsed = (now - startedAt) / 1000;
  const pct = Math.round(progress * 100);
  // Too noisy to be worth showing until a little progress has been made.
  const eta = progress > 0.02 ? formatDuration((elapsed * (1 - progress)) / progress) : "--:--";
  return (
    <div className="flex flex-col gap-1 text-sm">
      <p className="font-mono">
        {asciiBar(pct)} {pct}%
      </p>
      <p className="text-sm text-muted-foreground">
        {formatDuration(elapsed)} elapsed
        <Dot />
        eta {eta}
      </p>
    </div>
  );
}
