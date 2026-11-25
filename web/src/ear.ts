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

/** Something was put on there: 此刻 shows it, unless it looks at another speaker. */
export function lookAt(id: string) {
  if (id !== chosen && (id === HERE || chosen === HERE)) choose(id);
}

/** Speakers that are connected. */
export const connected = (devices: Device[]) => devices.filter((d) => d.in_account && d.mode !== 'off');

/** The speaker 此刻 and 流转 look at; '' is all of them. */
export function useEar(devices: Device[]): [string, (id: string) => void] {
  const id = useSyncExternalStore((f) => { subs.add(f); return () => { subs.delete(f); }; }, () => chosen);
  return [id === HERE || connected(devices).some((d) => d.device_id === id) ? id : '', choose];
}

const TARGET = 'trackferry.target';
let aimed = (() => { try { return localStorage.getItem(TARGET) ?? ''; } catch { return ''; } })();

/** What a tap sends to: the one chosen, else the speaker 此刻 looks at, else here. */
export function useTarget(devices: Device[]): [string, (id: string) => void] {
  const id = useSyncExternalStore((f) => { subs.add(f); return () => { subs.delete(f); }; }, () => aimed || chosen || HERE);
  const at = id === HERE ? undefined : connected(devices).find((d) => d.device_id === id) ?? connected(devices).find((d) => d.device_id === chosen);
  return [at?.device_id ?? HERE, (x) => {
    aimed = x;
    try { localStorage.setItem(TARGET, x); } catch { /* for this visit only */ }
    subs.forEach((f) => f());
  }];
}
