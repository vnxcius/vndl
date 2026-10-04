export function saveUrl(url: string, filename: string): void {
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
}

// "holiday.final.MOV" + "mp4" → "holiday.final.mp4"
export function renameExt(filename: string, ext: string): string {
  const dot = filename.lastIndexOf(".");
  const base = dot > 0 ? filename.slice(0, dot) : filename;
  return `${base || "converted"}.${ext}`;
}

// e.g. "−84%" or "+12%", relative to the original size.
export function sizeChange(before: number, after: number): string {
  if (before <= 0) return "";
  const pct = Math.round((after / before - 1) * 100);
  return pct <= 0 ? `−${Math.abs(pct)}%` : `+${pct}%`;
}
