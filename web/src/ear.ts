import { useSyncExternalStore } from 'react';
import type { Device } from './api';

/** The phone or computer that shows this page, among the speakers. */
export const HERE = 'here';

const KEY = 'trackferry.speaker';
let chosen = (() => { try { return localStorage.getItem(KEY) ?? ''; } catch { return ''; } })();
const subs = new Set<() => void>();

function choose(id: string) {
  chosen = id;
  try { localStorage.setItem(KEY, id); } catch { /* remembered for this visit only */ }
  subs.forEach((f) => f());
}

/** Keep playback and the current device view in sync. */
export function lookAt(id: string) {
  if (id !== aimed) aim(id);
  if (id !== chosen) choose(id);
}

/** Speakers that are connected. */
export const connected = (devices: Device[]) => devices.filter((d) => d.in_account && d.mode !== 'off');

/** The pick of 流转's list: an output, or '' for all of them. */
export const useChosen = () => useSyncExternalStore(subscribe, () => chosen);
/** The output picked to play on and to show. Hooks that resolve it live in outputs.ts. */
export const usePick = () => useSyncExternalStore(subscribe, () => aimed);
export { choose };

function subscribe(f: () => void) { subs.add(f); return () => { subs.delete(f); }; }

// A pick lasts beyond this visit: whoever picked the living room speaker finds it there tomorrow.
const TARGET = 'trackferry.play-output';
let aimed = (() => {
  try { return localStorage.getItem(TARGET) ?? sessionStorage.getItem(TARGET) ?? HERE; } catch { return HERE; }
})();
const aim = (id: string) => {
  aimed = id;
  try { localStorage.setItem(TARGET, id); } catch { /* for this visit only */ }
  subs.forEach((f) => f());
};

/** A television that came on is picked by itself only when the rule says so. */
export function selectNewTV(id: string, takes: boolean) {
  if (takes) lookAt(`tv:${id}`);
}
