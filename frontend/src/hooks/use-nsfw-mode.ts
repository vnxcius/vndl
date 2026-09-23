import { useSyncExternalStore } from "react";

// useSyncExternalStore instead of per-component useState, so the toggle
// (footer) and the consumer (DownloadConsole) stay in sync.
const STORAGE_KEY = "vndl-nsfw";
const listeners = new Set<() => void>();

function readStored(): boolean {
  try {
    return localStorage.getItem(STORAGE_KEY) === "1";
  } catch {
    return false;
  }
}

let cached = readStored();

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function getSnapshot() {
  return cached;
}

function setNsfwEnabled(next: boolean) {
  cached = next;
  try {
    if (next) localStorage.setItem(STORAGE_KEY, "1");
    else localStorage.removeItem(STORAGE_KEY);
  } catch {
    // storage unavailable — state still updates for this session
  }
  for (const listener of listeners) listener();
}

export function useNsfwMode() {
  const enabled = useSyncExternalStore(subscribe, getSnapshot);
  return { enabled, setEnabled: setNsfwEnabled };
}
