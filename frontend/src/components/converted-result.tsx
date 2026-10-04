import { DownloadSimpleIcon } from "@phosphor-icons/react";
import { Dot } from "@/components/dot";
import { OkLine } from "@/components/term";
import type { ConvertedFile } from "@/hooks/use-converted-file";
import { formatBytes } from "@/lib/format";
import { sizeChange } from "@/lib/save";

// The finished file: what was saved, how its size changed, and a way to save
// it again if the automatic download was blocked or dismissed.
export function ConvertedResult({ result, original }: { result: ConvertedFile; original: File }) {
  return (
    <>
      <OkLine>
        saved {result.name}
        <Dot />
        {formatBytes(result.blob.size)} ({sizeChange(original.size, result.blob.size)})
      </OkLine>
      <a
        href={result.url}
        download={result.name}
        className="inline-flex min-h-10 w-fit items-center gap-1.5 px-1 font-mono text-sm transition-colors outline-none hover:bg-foreground hover:text-background focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-foreground"
      >
        <span aria-hidden="true">[</span>
        <DownloadSimpleIcon size={16} />
        save again
        <span aria-hidden="true">]</span>
      </a>
    </>
  );
}
