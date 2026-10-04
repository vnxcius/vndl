import { Dot } from "@/components/dot";
import { DownloadConsole } from "@/components/download-console";
import { PageShell } from "@/components/page-shell";

export function HomePage() {
  return (
    <PageShell
      intro={
        <>
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
        </>
      }
    >
      <DownloadConsole />
    </PageShell>
  );
}
