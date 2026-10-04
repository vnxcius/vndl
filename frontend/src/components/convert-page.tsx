import { FilmStripIcon, ImageIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import { Dot } from "@/components/dot";
import { ImageConverter } from "@/components/image-converter";
import { PageShell } from "@/components/page-shell";
import { VideoConverter } from "@/components/video-converter";
import { cn } from "@/lib/utils";

export type ConvertKind = "image" | "video";

const tabs = [
  { kind: "image", icon: ImageIcon },
  { kind: "video", icon: FilmStripIcon },
] as const;

export function ConvertPage({ kind }: { kind: ConvertKind }) {
  return (
    <PageShell
      intro={
        <>
          <p className="mt-3">image &amp; video converter</p>
          <p className="mt-2 flex flex-wrap leading-relaxed">
            jpg
            <Dot />
            png
            <Dot />
            webp
            <Dot />
            mp4
            <Dot />
            webm
            <Dot />
            gif
            <Dot />
            mp3
          </p>
          <p>
            <br />
            files are converted on your device — they're never uploaded
          </p>
        </>
      }
    >
      <div className="mb-10 grid grid-cols-2 border-2 border-foreground font-mono text-sm sm:w-fit">
        {tabs.map(({ kind: k, icon: Icon }) => (
          <Link
            key={k}
            to="/convert"
            search={{ type: k }}
            aria-current={kind === k ? "page" : undefined}
            className={cn(
              "inline-flex min-h-11 items-center justify-center gap-2 px-5 transition-colors not-first:border-l-2 not-first:border-foreground",
              kind === k ? "bg-foreground font-semibold text-background" : "hover:bg-muted",
            )}
          >
            <Icon size={18} weight={kind === k ? "fill" : "regular"} />
            {k}
          </Link>
        ))}
      </div>
      {/* keyed so switching tabs starts each converter fresh */}
      {kind === "image" ? <ImageConverter key="image" /> : <VideoConverter key="video" />}
    </PageShell>
  );
}
