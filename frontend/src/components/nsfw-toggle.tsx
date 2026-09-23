import { InfoIcon } from "@phosphor-icons/react";
import { useState } from "react";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useNsfwMode } from "@/hooks/use-nsfw-mode";
import { NSFW_HOSTS } from "@/lib/nsfw";
import { cn } from "@/lib/utils";

export function NsfwToggle() {
  const { enabled, setEnabled } = useNsfwMode();
  const [infoOpen, setInfoOpen] = useState(false);

  return (
    <div className="inline-flex h-6 items-center gap-1">
      <button
        type="button"
        onClick={() => setEnabled(!enabled)}
        aria-pressed={enabled}
        className={cn(
          "inline-flex h-6 w-max cursor-pointer items-center gap-1 border border-border/40 px-2 py-1 font-mono text-xs transition-colors",
          enabled ? "bg-foreground text-background" : "text-muted-foreground hover:text-foreground",
        )}
      >
        nsfw: {enabled ? "on" : "off"}
      </button>
      {/* Tap-to-open (not just hover) so mobile users, who can't hover, can
          still see which sites this unlocks. */}
      <Tooltip open={infoOpen} onOpenChange={setInfoOpen}>
        <TooltipTrigger
          onClick={() => setInfoOpen((v) => !v)}
          aria-label="which sites this unlocks"
          className="flex h-6 w-6 cursor-pointer items-center justify-center text-muted-foreground transition-colors hover:text-foreground"
        >
          <InfoIcon size={13} />
        </TooltipTrigger>
        <TooltipContent>{NSFW_HOSTS.join(", ")}</TooltipContent>
      </Tooltip>
    </div>
  );
}
