import { CheckCircleIcon, WarningCircleIcon } from "@phosphor-icons/react";
import type { ButtonHTMLAttributes, ReactNode } from "react";
import { cn } from "@/lib/utils";

export function ErrorLine({ children }: { children: ReactNode }) {
  return (
    <p role="alert" className="flex items-start gap-2 text-sm text-red-600 dark:text-red-400">
      <WarningCircleIcon size={18} className="mt-0.5 shrink-0" />
      <span>{children}</span>
    </p>
  );
}

export function OkLine({ children }: { children: ReactNode }) {
  return (
    <p className="flex items-start gap-2 text-sm text-green-700 dark:text-green-400">
      <CheckCircleIcon size={18} className="mt-0.5 shrink-0" />
      <span className="min-w-0 break-words">{children}</span>
    </p>
  );
}

// Secondary explanation under a control, in plain words.
export function Hint({ children }: { children: ReactNode }) {
  return <p className="text-sm text-muted-foreground">{children}</p>;
}

const focusRing =
  "outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-foreground";

// "primary" is the one main action of a step: a hard-shadowed box. The
// default keeps the [bracketed] terminal look for everything else. Both are
// at least 40px tall, a comfortable touch target.
export function TermButton({
  variant = "default",
  className,
  children,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "default" | "primary" }) {
  if (variant === "primary") {
    return (
      <button
        {...props}
        className={cn(
          "inline-flex min-h-11 w-fit cursor-pointer items-center justify-center gap-2 border-2 border-foreground bg-background px-4 font-mono text-sm font-semibold shadow-brutal-sm transition-[transform,box-shadow,background-color,color]",
          "hover:bg-foreground hover:text-background active:translate-x-0.5 active:translate-y-0.5 active:shadow-none",
          "disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-40 disabled:shadow-none",
          focusRing,
          className,
        )}
      >
        {children}
      </button>
    );
  }
  return (
    <button
      {...props}
      className={cn(
        "inline-flex min-h-10 w-fit cursor-pointer items-center gap-1.5 px-1 font-mono text-sm transition-colors",
        "hover:bg-foreground hover:text-background",
        "disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-40",
        focusRing,
        className,
      )}
    >
      <span aria-hidden="true">[</span>
      {children}
      <span aria-hidden="true">]</span>
    </button>
  );
}
