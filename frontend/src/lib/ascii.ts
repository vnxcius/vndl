export function scannerBar(now: number, width = 24, segment = 4): string {
  const travel = width - segment;
  const cycle = travel * 2;
  const pos = Math.floor(now / 80) % cycle;
  const offset = pos < travel ? pos : cycle - pos;
  return "░".repeat(offset) + "█".repeat(segment) + "░".repeat(width - segment - offset);
}

export function asciiBar(percent: number, width = 24): string {
  const clamped = Math.max(0, Math.min(100, percent));
  const filled = Math.round((clamped / 100) * width);
  return "█".repeat(filled) + "░".repeat(width - filled);
}
