import { FFFSType, FFmpeg } from "@ffmpeg/ffmpeg";
// Self-hosted, never a CDN: the core (~32 MB) is fetched only when someone
// first converts a video, then cached by the browser.
import coreURL from "@ffmpeg/core?url";
import wasmURL from "@ffmpeg/core/wasm?url";

// Split on spaces — no argument below contains one.
const argv = (cmd: string) => cmd.split(" ");

// Encoder settings favor speed: this runs single-threaded in WebAssembly,
// several times slower than native ffmpeg.
export const videoFormats = {
  mp4: {
    mime: "video/mp4",
    note: "h.264 + aac, plays everywhere",
    // The scale rounds odd dimensions down to even, which yuv420p requires.
    args: argv(
      "-c:v libx264 -preset veryfast -crf 23 -pix_fmt yuv420p -vf scale=trunc(iw/2)*2:trunc(ih/2)*2 " +
        "-c:a aac -b:a 128k -movflags +faststart",
    ),
  },
  webm: {
    mime: "video/webm",
    // VP9 is too slow to encode here; VP8 is still standard WebM.
    note: "vp8 + opus",
    args: argv("-c:v libvpx -deadline realtime -cpu-used 8 -crf 10 -b:v 2M -c:a libopus -b:a 128k"),
  },
  gif: {
    mime: "image/gif",
    note: "12 fps, up to 480px wide, no sound",
    args: argv(
      "-vf fps=12,scale='min(480,iw)':-1:flags=lanczos,split[a][b];[a]palettegen[p];[b][p]paletteuse " +
        "-loop 0 -an",
    ),
  },
  mp3: {
    mime: "audio/mpeg",
    note: "audio only",
    args: argv("-vn -c:a libmp3lame -q:a 2"),
  },
} as const;

export type VideoFormat = keyof typeof videoFormats;

// WebAssembly is 32-bit: input is read from the file in place, but the
// output has to fit in its memory alongside ffmpeg itself.
export const maxVideoBytes = 2 * 1024 ** 3;

const inputDir = "/input";

let loading: Promise<FFmpeg> | null = null;

// One ffmpeg instance, loaded on first use. Canceling terminates its worker,
// so the next conversion loads a fresh one.
function getFFmpeg(): Promise<FFmpeg> {
  loading ??= (async () => {
    const ffmpeg = new FFmpeg();
    await ffmpeg.load({ coreURL, wasmURL });
    await ffmpeg.createDir(inputDir);
    return ffmpeg;
  })().catch((err: unknown) => {
    loading = null;
    throw err;
  });
  return loading;
}

export function isVideoConverterLoaded(): boolean {
  return loading !== null;
}

export interface ConvertCallbacks {
  onLoaded?: () => void;
  // 0..1; ffmpeg's estimate, rough for some inputs.
  onProgress?: (ratio: number) => void;
}

export async function convertVideo(
  file: File,
  format: VideoFormat,
  { onLoaded, onProgress }: ConvertCallbacks,
  signal: AbortSignal,
): Promise<Blob> {
  if (file.size > maxVideoBytes) {
    throw new Error("this file is too large to convert in the browser (max 2 GiB)");
  }

  let ffmpeg: FFmpeg;
  try {
    ffmpeg = await getFFmpeg();
  } catch {
    throw new Error("couldn't load the converter — check your connection and try again");
  }
  onLoaded?.();

  const abort = () => {
    ffmpeg.terminate();
    loading = null;
  };
  if (signal.aborted) throw new DOMException("canceled", "AbortError");
  signal.addEventListener("abort", abort, { once: true });

  // ffmpeg's last few log lines explain a failure better than its exit code.
  const logTail: string[] = [];
  const onLog = ({ message }: { message: string }) => {
    logTail.push(message);
    if (logTail.length > 8) logTail.shift();
  };
  const onFFmpegProgress = ({ progress }: { progress: number }) => {
    if (Number.isFinite(progress)) onProgress?.(Math.max(0, Math.min(1, progress)));
  };
  ffmpeg.on("log", onLog);
  ffmpeg.on("progress", onFFmpegProgress);

  const output = `output.${format}`;
  let mounted = false;
  try {
    // WORKERFS reads the file where it is instead of copying it into memory.
    await ffmpeg.mount(FFFSType.WORKERFS, { files: [file] }, inputDir);
    mounted = true;
    const code = await ffmpeg.exec(
      ["-hide_banner", "-i", `${inputDir}/${file.name}`, ...videoFormats[format].args, output],
      -1,
      { signal },
    );
    if (code !== 0) throw new ConversionError(logTail);
    const data = await ffmpeg.readFile(output, "binary", { signal });
    if (!(data instanceof Uint8Array) || data.length === 0) throw new ConversionError(logTail);
    return new Blob([data as Uint8Array<ArrayBuffer>], { type: videoFormats[format].mime });
  } catch (err) {
    if (signal.aborted) throw new DOMException("canceled", "AbortError");
    if (err instanceof ConversionError) throw err;
    throw new ConversionError(logTail);
  } finally {
    signal.removeEventListener("abort", abort);
    if (!signal.aborted) {
      ffmpeg.off("log", onLog);
      ffmpeg.off("progress", onFFmpegProgress);
      await ffmpeg.deleteFile(output).catch(() => {});
      if (mounted) await ffmpeg.unmount(inputDir).catch(() => {});
    }
  }
}

class ConversionError extends Error {
  constructor(logTail: string[]) {
    const log = logTail.join("\n").toLowerCase();
    super(
      log.includes("does not contain any stream") || log.includes("output file is empty")
        ? "this file has nothing to convert for that format (no video, or no audio for mp3)"
        : log.includes("invalid data found") || log.includes("moov atom not found")
          ? "this file isn't a video ffmpeg can read, or it's damaged"
          : log.includes("memory") || log.includes("oom")
            ? "ran out of memory — try a shorter or smaller video"
            : "the conversion failed — try another format",
    );
  }
}
