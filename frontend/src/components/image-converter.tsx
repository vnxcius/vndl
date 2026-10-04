import { ArrowsClockwiseIcon } from "@phosphor-icons/react";
import { useState } from "react";
import { ConvertedResult } from "@/components/converted-result";
import { FilePicker } from "@/components/file-picker";
import { Prompt } from "@/components/prompt";
import { ErrorLine, Hint, TermButton } from "@/components/term";
import { useConvertedFile } from "@/hooks/use-converted-file";
import {
  convertImage,
  type DecodedImage,
  decodeImage,
  type ImageFormat,
  imageFormats,
} from "@/lib/image-convert";
import { renameExt, saveUrl } from "@/lib/save";
import { cn } from "@/lib/utils";

export function ImageConverter() {
  const [file, setFile] = useState<File | null>(null);
  const [decoded, setDecoded] = useState<DecodedImage | null>(null);
  const [format, setFormat] = useState<ImageFormat>("webp");
  const [quality, setQuality] = useState(0.9);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useConvertedFile();

  async function handleFile(picked: File) {
    setFile(picked);
    setDecoded(null);
    setResult(null);
    setError(null);
    try {
      setDecoded(await decodeImage(picked));
    } catch (err) {
      setError((err as Error).message);
    }
  }

  async function handleConvert() {
    if (!file || !decoded) return;
    setBusy(true);
    setError(null);
    setResult(null);
    try {
      const blob = await convertImage(decoded, format, quality);
      const converted = setResult({ blob, name: renameExt(file.name, format) });
      if (converted) saveUrl(converted.url, converted.name);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex flex-col gap-10">
      <section className="flex flex-col gap-3">
        <Prompt cmd="choose_image" note="jpg, png, webp, gif, avif…" />
        <FilePicker
          accept="image/*"
          file={file}
          detail={decoded ? `${decoded.width}×${decoded.height}` : undefined}
          disabled={busy}
          onFile={handleFile}
        />
      </section>

      {decoded && (
        <section className="flex flex-col gap-4">
          <Prompt cmd="convert_to" />
          <div className="flex flex-wrap gap-3" role="group" aria-label="output format">
            {(Object.keys(imageFormats) as ImageFormat[]).map((f) => (
              <TermButton
                key={f}
                onClick={() => setFormat(f)}
                aria-pressed={format === f}
                className={cn("px-2", format === f && "bg-foreground text-background")}
              >
                {f}
              </TermButton>
            ))}
          </div>

          {imageFormats[format].lossy ? (
            <label className="flex min-h-10 items-center gap-3 font-mono text-sm">
              <span>quality</span>
              <input
                type="range"
                min={0.4}
                max={1}
                step={0.05}
                value={quality}
                onChange={(e) => setQuality(Number(e.target.value))}
                className="h-6 w-full max-w-56 min-w-0 flex-1 cursor-pointer accent-foreground"
              />
              <span className="w-8 text-right tabular-nums">{Math.round(quality * 100)}</span>
            </label>
          ) : (
            <Hint>lossless — no quality setting</Hint>
          )}

          <TermButton
            variant="primary"
            onClick={handleConvert}
            disabled={busy}
            className="w-full sm:w-fit"
          >
            <ArrowsClockwiseIcon size={18} weight="bold" />
            {busy ? "converting…" : `convert to ${format}`}
          </TermButton>
        </section>
      )}

      {(error || result) && (
        <section className="flex flex-col gap-3">
          <Prompt cmd="status" />
          {error && <ErrorLine>{error}</ErrorLine>}
          {result && file && <ConvertedResult result={result} original={file} />}
        </section>
      )}
    </div>
  );
}
