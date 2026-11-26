import { useSyncExternalStore } from 'react';
import type { Intent, Layer, LayerName, Route, TakeoverRun, WatchState } from './api';

export interface Flow {
  id: number; kind: 'command' | 'test' | 'try' | 'push'; device_id?: string; device?: string; command_id?: number;
  said: string; upstream_ms?: number; seen_ms: number; intent?: Intent;
  layers: Partial<Record<LayerName, Layer>>; route?: Route; route_ms?: number; run?: TakeoverRun;
  audio?: { bytes: number; total?: number; requests: number; open: boolean; at_ms: number };
  player?: { status: number; position: number; duration: number; volume: number; ours: boolean; elapsed: number; phase?: 'playing' | 'paused' | 'talking' | 'resuming'; seek?: boolean; at_ms: number };
  done: boolean; note?: string; updated_ms: number;
}
export interface Pulse { device_id: string; name: string; state: WatchState; ok: boolean; ms: number; ignored: number; recorded: number; every_ms?: number; at_ms: number }
export interface Live { on: boolean; flows: Flow[]; pulses: Record<string, Pulse>; skew: number }

let state: Live = { on: false, flows: [], pulses: {}, skew: 0 };
let es: EventSource | null = null;
let timer = 0;
const subs = new Set<() => void>();
const tell = () => { clearTimeout(later); later = 0; subs.forEach((f) => f()); };
let later = 0;
const set = (s: Partial<Live>) => { state = { ...state, ...s }; tell(); };

function open() {
  es = new EventSource('/api/live');
  es.addEventListener('hello', (e) => {
    const d = JSON.parse((e as MessageEvent).data);
    set({ on: true, flows: d.flows ?? [], pulses: d.pulses ?? {}, skew: d.now_ms - Date.now() });
  });
  es.addEventListener('flow', (e) => {
    const f: Flow = JSON.parse((e as MessageEvent).data);
    set({ flows: [...state.flows.filter((x) => x.id !== f.id), f].sort((a, b) => a.id - b.id).filter((x, i, l) => i >= l.length - 12 || !x.done) });
  });
  es.addEventListener('pulse', (e) => {
    const p: Pulse = JSON.parse((e as MessageEvent).data);
    // Heartbeats come every second from every speaker: shown together.
    state = { ...state, pulses: { ...state.pulses, [p.device_id]: p } };
    later ||= window.setTimeout(tell, 500);
  });
  es.onerror = () => {
    set({ on: false });
    if (es?.readyState === EventSource.CLOSED) { es = null; timer = window.setTimeout(() => subs.size && open(), 5000); }
  };
}

function subscribe(f: () => void) {
  subs.add(f);
  if (!es) { clearTimeout(timer); open(); }
  return () => {
    subs.delete(f);
    timer = window.setTimeout(() => { if (!subs.size) { es?.close(); es = null; state = { ...state, on: false }; } }, 500);
  };
}

/** What is happening on the speakers right now. */
export const useLive = () => useSyncExternalStore(subscribe, () => state);
export const serverNow = () => Date.now() + state.skew;

export interface Sounding {
  phase: 'playing' | 'paused' | 'talking' | 'resuming';
  at: number;
  since: number;
  seek?: boolean;
  read?: () => number;
}
/** The song a run has on its speaker now, as followed by the server. null: not playing any more. */
export function sounding(flows: Flow[], runID?: number): Sounding | null {
  const f = runID ? flows.find((x) => x.run?.id === runID) : undefined;
  const p = f?.player;
  if (!f || f.done || !p?.phase) return null;
  return { phase: p.phase, at: p.elapsed, since: p.at_ms, seek: p.seek };
}
export const position = (s: Sounding) => s.read?.() ?? s.at + (s.phase === 'playing' ? Math.max(0, serverNow() - s.since) : 0);
export const phaseText = { playing: '正在播放', paused: '停着', talking: '小爱在说话', resuming: '正在接着放' } as const;

/** The song a speaker has on now. */
export function onSpeaker(flows: Flow[], id: string) {
  return flows.filter((f) => f.device_id === id && !f.done && f.player?.phase).at(-1) ?? null;
}

/** The song of a flow, if it is one of ours. */
export const songOf = (f: Flow) => (f.route?.decision !== 'native' && f.route?.title ? f.route : f.run?.song) ?? null;

/** The song a speaker has on, or is about to: it shows from the moment it is sent. */
export function onDeck(flows: Flow[], id: string) {
  return flows.filter((f) => f.device_id === id && !f.done && songOf(f)?.title && (f.player?.phase || (f.kind !== 'try' && f.kind !== 'test' && f.run?.outcome === 'running'))).at(-1) ?? null;
}
