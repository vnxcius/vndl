// A section heading styled as a shell command; note is a plain-language
// "# comment" saying what the step is for.
export function Prompt({ cmd, note }: { cmd: string; note?: string }) {
  return (
    <p className="text-sm">
      <span className="font-semibold text-green-700 dark:text-green-400">$</span>{" "}
      <span className="font-medium text-foreground">{cmd}</span>
      {note && <span className="text-muted-foreground"> # {note}</span>}
    </p>
  );
}
