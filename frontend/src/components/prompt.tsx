export function Prompt({ cmd }: { cmd: string }) {
  return (
    <p className="text-xs text-muted-foreground">
      <span className="text-green-600 dark:text-green-400">$</span>{" "}
      <span className="text-neutral-500">{cmd}</span>
    </p>
  );
}
