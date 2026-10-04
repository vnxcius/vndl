import { DownloadSimpleIcon, GitlabLogoIcon, type Icon, SwapIcon } from "@phosphor-icons/react";
import { Dot } from "@/components/dot";
import { NsfwToggle } from "@/components/nsfw-toggle";
import { ThemeSelector } from "@/components/theme-selector";
import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { Prompt } from "./prompt";

const REPO_URL = "https://gitlab.com/vncius/vndl";
const CHANGELOG_URL = `${REPO_URL}/-/blob/main/CHANGELOG.md`;

// Header, nav and footer shared by every page; intro is the readme blurb.
export function PageShell({ intro, children }: { intro: ReactNode; children: ReactNode }) {
  return (
    <div className="mx-auto flex min-h-svh max-w-2xl flex-col px-4 py-12 sm:py-20">
      <header className="mb-14 flex flex-col gap-4">
        <div>
          <p className="font-mono text-sm font-semibold">vndl</p>
          <p className="font-mono text-xs text-muted-foreground">vncius' downloader</p>
        </div>
        <nav className="flex items-center gap-3 font-mono text-sm">
          <NavLink to="/" icon={DownloadSimpleIcon}>
            download
          </NavLink>
          <span className="text-muted-foreground">•</span>
          <NavLink to="/convert" icon={SwapIcon}>
            convert
          </NavLink>
        </nav>
        <div className="mt-4 text-sm text-muted-foreground">
          <Prompt cmd="cat ~/readme" />
          {intro}
        </div>
      </header>

      <main className="flex-1">{children}</main>

      {/* Controls get their own row: next to the text they squeezed it into
          mid-phrase line breaks. */}
      <footer className="mt-20 flex flex-col gap-4 border-t border-border/40 pt-6 text-xs text-muted-foreground">
        <div className="flex flex-col gap-1 sm:flex-row sm:flex-wrap sm:items-center [&>span]:whitespace-nowrap">
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
            </a>{" "}
            &amp;{" "}
            <a className="text-primary underline underline-offset-3" href="https://ffmpeg.org">
              ffmpeg
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
            className="flex h-9 w-9 cursor-pointer items-center justify-center border border-border/40 text-muted-foreground transition-colors hover:text-foreground"
          >
            <GitlabLogoIcon size={18} />
          </a>
          <NsfwToggle />
          <ThemeSelector />
        </div>
      </footer>
    </div>
  );
}

function NavLink({
  to,
  icon: NavIcon,
  children,
}: {
  to: "/" | "/convert";
  icon: Icon;
  children: ReactNode;
}) {
  return (
    <Link
      to={to}
      className="inline-flex min-h-10 items-center gap-2 text-muted-foreground transition-colors hover:text-foreground"
      activeProps={{ className: "text-green-700! dark:text-green-500! underline font-semibold" }}
      activeOptions={{ exact: true, includeSearch: false }}
    >
      <NavIcon size={18} />
      {children}
    </Link>
  );
}
