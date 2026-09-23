import { DesktopIcon, MoonIcon, SunIcon } from "@phosphor-icons/react";
import { type Theme, useTheme } from "@/hooks/use-theme";
import { cn } from "@/lib/utils";

const OPTIONS: { value: Theme; label: string; Icon: typeof SunIcon }[] = [
  { value: "light", label: "light", Icon: SunIcon },
  { value: "dark", label: "dark", Icon: MoonIcon },
  { value: "system", label: "system", Icon: DesktopIcon },
];

export function ThemeSelector() {
  const { theme, setTheme } = useTheme();

  return (
    <div className="flex border border-border/40">
      {OPTIONS.map((opt) => {
        const active = theme === opt.value;
        return (
          <button
            key={opt.value}
            type="button"
            onClick={() => setTheme(opt.value)}
            aria-pressed={active}
            title={opt.label}
            className={cn(
              "flex h-5.5 cursor-pointer items-center justify-center px-2 py-1 transition-colors not-first:border-l not-first:border-border/40",
              active
                ? "bg-foreground text-background"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            <opt.Icon size={13} weight={active ? "fill" : "regular"} />
          </button>
        );
      })}
    </div>
  );
}
