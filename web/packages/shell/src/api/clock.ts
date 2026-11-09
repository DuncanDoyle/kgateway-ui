import { useSyncExternalStore } from "react";

// region Helpers
// One interval for the whole app, started by the first subscriber and stopped by the last.
const listeners = new Set<() => void>();
let timer: ReturnType<typeof setInterval> | undefined;

function subscribe(onTick: () => void): () => void {
  listeners.add(onTick);
  timer ??= setInterval(notify, 1000);
  return () => {
    listeners.delete(onTick);
    if (listeners.size === 0 && timer !== undefined) {
      clearInterval(timer);
      timer = undefined;
    }
  };
}

function notify() {
  for (const l of listeners) l();
}

// Whole seconds, so the snapshot is stable between ticks (useSyncExternalStore requires it).
function wallClock(): number {
  return Math.floor(Date.now() / 1000) * 1000;
}

// region Server clock
// Server minus browser clock, so data age reads the server's clock even when the cluster's clock drifts (4.3).
let offset = 0;
/** Changes within this are ignored: Date has 1 s resolution, plus latency. Keeps equal clocks exact and ages steady. */
const SKEW_TOLERANCE_MS = 2000;

/** Records the server clock from a response's Date header, read when the response arrives. */
export function observeServerDate(date: string | null): void {
  const server = date ? Date.parse(date) : Number.NaN;
  if (Number.isNaN(server)) return;
  // Date is cut to the second; +500 ms centres the estimate.
  const next = server + 500 - Date.now();
  if (Math.abs(next - offset) <= SKEW_TOLERANCE_MS) return;
  offset = next;
  notify();
}

// region Hidden tab
// Polls stop while the tab is hidden (D-063). The clock holds at the moment it was hidden until the first
// requests after it is shown settle, so time spent hidden never counts as staleness on its own.
let heldAt: number | undefined;
let waiting = false;
let inFlight = 0;
let cap: ReturnType<typeof setTimeout> | undefined;
/** The longest the clock holds after the tab is shown when no request settles: two poll intervals. */
const HOLD_MAX_MS = 10_000;

function resume() {
  clearTimeout(cap);
  heldAt = undefined;
  waiting = false;
  notify();
}

function onVisibility() {
  if (document.visibilityState === "hidden") {
    clearTimeout(cap);
    waiting = false;
    // Held on the server's clock, so a skew learnt during the hold does not move it.
    heldAt ??= wallClock() + offset;
  } else if (heldAt !== undefined) {
    waiting = true;
    cap = setTimeout(resume, HOLD_MAX_MS);
  }
}

if (typeof document !== "undefined") document.addEventListener("visibilitychange", onVisibility);

/** Counts one API request; call the returned function when it settles. The clock resumes once none are left. */
export function trackRequest(): () => void {
  inFlight++;
  return () => {
    inFlight--;
    // A macrotask later, so SWR has stored the answer before the clock moves on.
    if (waiting && inFlight === 0) setTimeout(() => waiting && inFlight === 0 && resume(), 0);
  };
}

/** Test hook: back to a running clock with no requests counted. */
export function resetClockForTests(): void {
  clearTimeout(cap);
  heldAt = undefined;
  waiting = false;
  inFlight = 0;
  offset = 0;
}

function snapshot(): number {
  return heldAt ?? wallClock() + offset;
}

// region Component
/**
 * The current time on the server's clock, at 1 s resolution. Renders with it re-run every second, so staleness is
 * re-evaluated even when data is unchanged. After a hidden tab is shown it holds at the hiding time until the first
 * poll answers.
 */
export function useNow(): number {
  return useSyncExternalStore(subscribe, snapshot, snapshot);
}
