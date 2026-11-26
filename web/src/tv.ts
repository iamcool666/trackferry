import { useSyncExternalStore } from 'react';
import { api, ApiError, type TVCommand, type TVOutput, type TVPlayback } from './api';

export const tvTarget = (id: string) => `tv:${id}`;
export const tvID = (target: string) => target.startsWith('tv:') ? target.slice(3) : '';
let devices: TVOutput[] = [];
let pairing: Awaited<ReturnType<typeof api.tvOutputs>>['pairing'];
let received = false;
let clockDelta = 0;
let latestClock = 0;
const listeners = new Set<() => void>();
const subscribe = (f: () => void) => { listeners.add(f); return () => { listeners.delete(f); }; };
const value = () => devices;
function put(next: TVOutput[], prompt?: typeof pairing) { devices = next; pairing = prompt; listeners.forEach((f) => f()); }
function apply(next: Awaited<ReturnType<typeof api.tvOutputs>>, sentAt: number) {
  if (next.now_ms !== undefined && next.now_ms < latestClock) return;
  latestClock = next.now_ms ?? latestClock;
  if (next.now_ms) clockDelta = next.now_ms - (sentAt + Date.now()) / 2;
  received = true;
  put(next.devices, next.pairing);
}
export const useTVOutputs = () => useSyncExternalStore(subscribe, value);
export const useTVPairing = () => useSyncExternalStore(subscribe, () => pairing);
export const useTVReady = () => useSyncExternalStore(subscribe, () => received);
export const activeTV = (target: string) => devices.find((d) => d.id === tvID(target) && d.online);

export async function refreshTVOutputs() {
  const started = Date.now();
  apply(await api.tvOutputs(), started);
}

/** One monitor for the authenticated PWA, independent of the page being visited. */
export function monitorTV() {
  let stopped = false, running = false, lastSeen = performance.now(), enabled = true;
  let timer: ReturnType<typeof setTimeout>;
  let query: AbortController | undefined;
  latestClock = 0;
  const poll = async () => {
    if (stopped || running || !enabled) return;
    running = true;
    query = new AbortController();
    const timeout = setTimeout(() => query?.abort(), 5000);
    try {
      const started = Date.now();
      const result = await api.tvOutputs(query.signal);
      if (stopped) return;
      lastSeen = performance.now();
      apply(result, started);
    } catch (e) {
      if (stopped) return;
      if (e instanceof ApiError && e.status === 404) { enabled = false; received = true; put([]); }
      else if (performance.now() - lastSeen >= 8_000) put(devices.map((d) => ({ ...d, online: false })));
    } finally {
      clearTimeout(timeout);
      running = false;
      if (!stopped && enabled) timer = setTimeout(poll, document.hidden ? 6500 : 1900);
    }
  };
  const wake = () => { if (!document.hidden) { clearTimeout(timer); void poll(); } };
  document.addEventListener('visibilitychange', wake);
  void poll();
  return () => {
    stopped = true;
    document.removeEventListener('visibilitychange', wake);
    clearTimeout(timer);
    query?.abort();
    received = false;
    put([]);
  };
}

export const tvPhase: Record<TVPlayback['phase'], string> = { idle: '待播', playing: '正在播放', paused: '已暂停', buffering: '正在缓冲', ended: '已播完', error: '播放失败' };
export const tvPosition = (p: TVPlayback) => Math.max(0, Math.min(p.duration_ms || Infinity,
  p.position_ms + (p.phase === 'playing' ? Math.max(0, Math.min(Date.now() + clockDelta - p.at_ms, 3500)) : 0)));

function wait(ms: number, signal?: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    signal?.throwIfAborted();
    const cancel = () => { clearTimeout(timer); reject(signal?.reason); };
    const timer = setTimeout(() => { signal?.removeEventListener('abort', cancel); resolve(); }, ms);
    signal?.addEventListener('abort', cancel, { once: true });
  });
}

/** Only a TV report completes a command. HTTP acceptance is just the request being queued. */
export async function commandTV(id: string, command: TVCommand, signal?: AbortSignal) {
  const deadline = new AbortController();
  const stop = () => deadline.abort(signal?.reason);
  signal?.throwIfAborted();
  signal?.addEventListener('abort', stop, { once: true });
  const timeout = setTimeout(() => deadline.abort(new Error('电视没有及时确认，源设备继续播放')), 26_000);
  let pending: string | undefined;
  try {
    const first = await api.tvCommand(id, command, deadline.signal);
    pending = first.id;
    for (;;) {
      await wait(260, deadline.signal);
      const r = await api.tvCommandStatus(id, pending, deadline.signal);
      if (r.state === 'pending') continue;
      pending = undefined;
      if (r.state !== 'done') throw new Error(r.message || (r.state === 'superseded' ? '已被更新的操作替代' : '电视未完成此操作'));
      const started = Date.now();
      void api.tvOutputs(signal).then((result) => apply(result, started), () => {});
      return;
    }
  } catch (error) {
    if (deadline.signal.aborted) throw deadline.signal.reason;
    throw error;
  } finally {
    clearTimeout(timeout);
    signal?.removeEventListener('abort', stop);
    if (pending) void api.tvCancel(id, pending).catch(() => {});
  }
}
