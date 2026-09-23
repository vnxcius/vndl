import { GitlabLogoIcon } from "@phosphor-icons/react";
import { Dot } from "@/components/dot";
import { DownloadConsole } from "@/components/download-console";
import { NsfwToggle } from "@/components/nsfw-toggle";
import { ThemeSelector } from "@/components/theme-selector";
import { Prompt } from "./prompt";

const REPO_URL = "https://gitlab.com/vncius/vndl";
const CHANGELOG_URL = `${REPO_URL}/-/blob/main/CHANGELOG.md`;

export function HomePage() {
  return (
    <div className="mx-auto flex min-h-svh max-w-2xl flex-col px-4 py-12 sm:py-20">
      <header className="mb-14 flex flex-col gap-4">
        <div>
          <p className="font-mono text-sm font-semibold">vndl</p>
          <p className="font-mono text-xs text-muted-foreground">vncius' downloader</p>
        </div>
        <div className="mt-4 text-sm text-muted-foreground">
          <Prompt cmd="cat ~/readme" />
          <p className="mt-3">privacy-first focused video &amp; audio downloader</p>
          <p className="mt-2 flex flex-wrap leading-relaxed">
            youtube
            <Dot />
            twitter/x
            <Dot />
            tiktok
            <Dot />
            instagram
          </p>
          <p>
            <br />
            nothing is stored server-side
          </p>
        </div>
      </header>

      <main className="flex-1">
        <DownloadConsole />
      </main>

      <footer className="mt-20 flex flex-col gap-3 border-t border-border/40 pt-6 text-xs text-muted-foreground sm:flex-row sm:items-center sm:justify-between">
        <div className="flex flex-col gap-1 sm:flex-row sm:items-center">
          <span>
            <a href="https://github.com/vnxcius">vndl</a>
            <Dot />
            &copy; 2026
          </span>
          <span className="hidden sm:inline">
            <Dot />
          </span>
          <span>
            built with{" "}
            <a
              className="text-primary underline underline-offset-3"
              href="https://github.com/yt-dlp/yt-dlp"
            >
              yt-dlp
            </a>
          </span>
          <span className="hidden sm:inline">
            <Dot />
          </span>
          <a
            href={CHANGELOG_URL}
            target="_blank"
            rel="noopener noreferrer"
            title="changelog"
            className="transition-colors hover:text-foreground"
          >
            v{__APP_VERSION__}
          </a>
        </div>
        <div className="flex items-center gap-2">
          <a
            href={REPO_URL}
            target="_blank"
            rel="noopener noreferrer"
            title="source on gitlab"
            className="flex h-6 cursor-pointer items-center justify-center border border-border/40 px-2 py-1 text-muted-foreground transition-colors hover:text-foreground"
          >
            <GitlabLogoIcon size={13} />
          </a>
          <NsfwToggle />
          <ThemeSelector />
        </div>
      </footer>
    </div>
  );
}
