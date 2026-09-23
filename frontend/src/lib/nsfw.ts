// Client-side only — used to show a friendly message instead of a raw
// backend error when the NSFW toggle is off.
export const NSFW_HOSTS = ["pornhub.com", "xvideos.com", "xnxx.com", "xhamster.com", "redtube.com"];

export function isNsfwUrl(rawUrl: string): boolean {
  try {
    const host = new URL(rawUrl).hostname.toLowerCase();
    return NSFW_HOSTS.some((h) => host === h || host.endsWith(`.${h}`));
  } catch {
    return false;
  }
}
