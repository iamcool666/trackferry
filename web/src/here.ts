import { useSyncExternalStore } from 'react';
import { api, type HereListed, type OnlineSong, type PlayMode, type Sendable, type Track } from './api';
import { singer } from './copy';
import { lookAt } from './ear';
import { coverURL } from './stage/art';
import { serverNow, type Sounding } from './live';
import { arrange, putNext, step, takeOff, type Slot } from './playlist';

// Songs played by the browser itself, on the phone or computer that shows this page.

export interface HereSong extends Slot { title: string; artist: string; album?: string; cover?: string; duration_ms: number; track_id?: number; online?: string; source?: OnlineSong['source'] }
type New = Omit<HereSong, 'n'>;
export interface Here { songs: HereSong[]; i: number; list: string; mode: PlayMode; on: boolean; waiting: boolean; at: number; since: number; total: number; /** It was put on alone: songs of the library come after it. */ drift: boolean; volume: number; canVolume: boolean; said: { text: string; n: number } | null }

const coarse = matchMedia('(pointer: coarse)').matches;
export const hereName = '本机';
export const handheld = coarse;

const KEY = 'trackferry.here';
const KEPT = 300; // songs remembered for the next visit
let s: Here = { songs: [], i: 0, list: '', mode: 'loop', on: false, waiting: false, at: 0, since: 0, total: 0, drift: false, volume: 100, canVolume: false, said: null };
try {
  const k = JSON.parse(localStorage.getItem(KEY) ?? 'null');
  if (Array.isArray(k?.songs) && k.songs[k.i]) s = { ...s, songs: k.songs, i: k.i, list: k.list ?? '', drift: k.drift ?? k.songs.length < 2, mode: k.mode ?? 'loop', at: Number(k.at) || 0, total: k.songs[k.i].duration_ms ?? 0, volume: k.volume ?? 100 };
} catch { /* starts empty */ }

const subs = new Set<() => void>();
const set = (x: Partial<Here>) => { s = { ...s, ...x }; subs.forEach((f) => f()); };
const say = (text: string) => set({ said: { text, n: (s.said?.n ?? 0) + 1 } });
export const useHere = () => useSyncExternalStore((f) => { subs.add(f); return () => { subs.delete(f); }; }, () => s);
export const hereSong = (x: Here): HereSong | null => x.songs[x.i] ?? null;
export const hereSounding = (x: Here): Sounding | null => hereSong(x) && { phase: !x.on ? 'paused' : x.waiting ? 'resuming' : 'playing', at: x.at, since: x.since, seek: true };

function keep() {
  const from = Math.max(0, Math.min(s.i - 20, s.songs.length - KEPT));
  try { localStorage.setItem(KEY, JSON.stringify({ songs: s.songs.slice(from, from + KEPT), i: s.i - from, list: s.list, drift: s.drift, mode: s.mode, volume: s.volume, at: Math.round(where()) })); } catch { /* for this visit only */ }
}

{ const a = new Audio(); a.volume = .5; s = { ...s, canVolume: a.volume === .5 }; } // iOS leaves it to the buttons of the phone

let audio: HTMLAudioElement | null = null;
let loaded = ''; // the song the element has
let wanted = 0; // where to start, until the element knows how long it is
let missed = 0; // songs in a row that could not be played

// WebKit goes by bytes to a point of a FLAC and ends up half a minute off; it gets an index.
const webkit = 'GestureEvent' in window;
const url = (x: HereSong) => `/api/here/audio?${x.online ? `online=${encodeURIComponent(x.online)}` : `track=${x.track_id}${webkit ? '&index=1' : ''}`}`;
let warm = ''; // the next song, made ready on the server
/** Has the next song made ready while this one ends. */
function ahead() {
  if (!webkit || !s.total || s.total - where() > 40_000 || s.mode === 'one') return;
  const x = s.songs[step(s.songs.length, s.i, 1, s.mode, false) ?? -1];
  if (!x || x.online || warm === x.key) return;
  warm = x.key;
  fetch(url(x), { headers: { Range: 'bytes=0-1' }, credentials: 'same-origin' }).then((r) => r.body?.cancel()).catch(() => {});
}
const where = () => (audio && loaded && !wanted ? audio.currentTime * 1000 : s.at);
/** Says where the song is now; between these the page counts by itself. */
const mark = (x: Partial<Here> = {}) => { set({ at: where(), since: serverNow(), ...x }); position(); };

function element() {
  if (audio) return audio;
  const a = audio = new Audio();
  a.preload = 'auto';
  a.volume = s.volume / 100;
  a.addEventListener('play', () => mark({ on: true }));
  a.addEventListener('pause', () => { if (!a.ended && !a.error) mark({ on: false, waiting: false }); keep(); });
  a.addEventListener('waiting', () => mark({ waiting: true }));
  a.addEventListener('playing', () => { missed = 0; mark({ on: true, waiting: false }); });
  a.addEventListener('seeked', () => mark(a.paused || a.readyState < 3 ? {} : { waiting: false }));
  a.addEventListener('loadedmetadata', () => {
    if (wanted) { const to = wanted; wanted = 0; a.currentTime = to / 1000; }
    if (Number.isFinite(a.duration)) set({ total: a.duration * 1000 });
    mark();
  });
  a.addEventListener('durationchange', () => { if (Number.isFinite(a.duration) && a.duration) { set({ total: a.duration * 1000 }); position(); } });
  let last = -1;
  a.addEventListener('timeupdate', () => {
    const t = a.currentTime;
    // It sounds, whatever else was said: after a jump some browsers never say that they play.
    if (s.waiting && !a.paused && !a.seeking && loaded && !wanted && t > last && t - last < 1) mark({ waiting: false });
    last = t;
    ahead();
    if (!s.waiting && Math.abs(s.at + (s.on ? serverNow() - s.since : 0) - where()) > 500) mark();
  });
  a.addEventListener('ended', () => go(1, false));
  a.addEventListener('error', broken);
  addEventListener('pagehide', keep);
  document.addEventListener('visibilitychange', () => { if (document.hidden && s.on) keep(); });
  return a;
}

async function broken() {
  const x = hereSong(s);
  if (!x || loaded !== x.key) return;
  loaded = '';
  let why = '放不了';
  try {
    const r = await fetch(url(x), { headers: { Range: 'bytes=0-1' }, credentials: 'same-origin' });
    if (!r.ok) why = (await r.json()).message ?? why; else r.body?.cancel();
  } catch { why = '连不上 TrackFerry'; }
  if (hereSong(s)?.key !== x.key) return;
  missed++;
  say(`《${x.title}》${why}`);
  const last = missed >= Math.min(5, s.songs.length) || why.startsWith('连不上');
  if (!last && s.on) { const to = step(s.songs.length, s.i, 1, 'loop', true); if (to !== null && to !== s.i) return start(to); }
  missed = 0;
  mark({ on: false, waiting: false });
}

/** Puts a song on. Called in the tap itself: a browser lets nothing else make sound. */
function start(i: number, at = 0, sound = true) {
  const x = s.songs[i];
  if (!x) return;
  const a = element();
  wanted = at;
  loaded = x.key;
  a.src = url(x);
  set({ i, at, since: serverNow(), total: x.duration_ms, on: sound, waiting: sound });
  tell(x);
  keep();
  if (sound) sing(a);
  drift();
}

function sing(a: HTMLAudioElement) {
  a.play().catch((e: DOMException) => {
    if (e.name === 'NotAllowedError') { mark({ on: false, waiting: false }); say('浏览器拦下了声音，再点一次播放'); }
  });
}

let filling = '';
/** Songs of the library come after one that was put on alone, and after those. */
async function drift() {
  const x = hereSong(s);
  if (!s.drift || !x || s.songs.length - s.i > 4 || filling === x.key) return;
  const f = filling = x.key;
  try {
    const r = await api.hereList({ source: 'random' });
    if (!s.drift || !hereSong(s)) return;
    const had = new Set(s.songs.map((y) => y.key));
    let n = Math.max(0, ...s.songs.map((y) => y.n));
    const more = r.songs.map(ofListed).filter((y) => !had.has(y.key)).map((y) => ({ ...y, n: ++n }));
    const songs = [...s.songs, ...more], cut = Math.max(0, s.i - 100); // what was heard long ago may go
    set({ songs: songs.slice(cut), i: s.i - cut, list: r.list });
    keep();
  } catch { /* tried again with the next song */ } finally { if (filling === f) filling = ''; }
}

function go(dir: 1 | -1, by: boolean) {
  let to = step(s.songs.length, s.i, dir, s.mode, by);
  if (to === null) { mark({ on: false, waiting: false, at: 0 }); if (audio) audio.currentTime = 0; return; }
  if (!by && s.mode === 'one' && audio) { audio.currentTime = 0; sing(audio); return; }
  if (dir === 1 && to === 0 && s.mode === 'shuffle' && s.songs.length > 2) { // round again, in another order
    const on = hereSong(s)!;
    const next = arrange(s, 'shuffle');
    set(next);
    to = next.songs.findIndex((x) => x !== on);
  }
  start(to);
}

export const here = {
  toggle() {
    const a = element(), x = hereSong(s);
    if (!x) return;
    if (s.on) { a.pause(); mark({ on: false, waiting: false }); } else if (loaded !== x.key) start(s.i, s.at);
    else { set({ on: true }); sing(a); }
  },
  next: () => go(1, true),
  previous() { if (where() > 4000) here.seek(0); else go(-1, true); },
  seek(ms: number) {
    const x = hereSong(s);
    if (!x) return;
    const to = Math.max(0, Math.min(ms, s.total ? s.total - 500 : ms));
    if (loaded !== x.key) start(s.i, to, s.on);
    else if (audio && audio.readyState > 0) { audio.currentTime = to / 1000; mark({ at: to }); } else { wanted = to; set({ at: to, since: serverNow() }); }
  },
  mode(m: PlayMode) { set({ ...(m === 'one' || s.mode === 'one' && m === 'loop' ? {} : arrange(s, m)), mode: m }); keep(); },
  volume(v: number) { const a = element(); a.volume = v / 100; set({ volume: v }); keep(); },
  jump: (key: string) => { const i = s.songs.findIndex((x) => x.key === key); if (i >= 0) start(i); },
  remove(key: string) {
    const was = hereSong(s)?.key, next = takeOff(s, key);
    if (!next.songs.length) { here.stop(); return; }
    set(next);
    if (was === key) start(next.i, 0, s.on); else keep();
  },
  stop() {
    audio?.pause();
    audio?.removeAttribute('src');
    audio?.load();
    loaded = '';
    set({ songs: [], i: 0, list: '', drift: false, on: false, waiting: false, at: 0, total: 0 });
    tell(null);
    keep();
  },
};

/** Plays a song here, now or after the one that is on. One of a list brings the list; after one alone come others. */
export function playHere(x: New, how: 'now' | 'next' = 'now', from?: { list: string; songs: New[] }) {
  const on = hereSong(s);
  lookAt('here');
  if (how === 'next' && on) { set(putNext(s, x)); keep(); return; }
  if (from && from.songs.length > 1 && from.songs.some((y) => y.key === x.key)) {
    const seen = new Set<string>();
    const songs = from.songs.filter((y) => !seen.has(y.key) && seen.add(y.key)).map((y, n) => ({ ...y, n }));
    const all = { songs, i: songs.findIndex((y) => y.key === x.key) };
    set({ list: from.list, drift: false, ...(s.mode === 'shuffle' ? arrange(all, 'shuffle') : all) });
  } else if (!on || s.drift || s.songs.length < 2) {
    set(on?.key === x.key ? { drift: true } : { ...(on ? jumpTo(putNext(s, x), x.key) : putNext(s, x)), drift: true, list: s.drift ? s.list : '' });
  } else if (on.key !== x.key) set(jumpTo(putNext(s, x), x.key));
  start(s.i);
}
const jumpTo = (l: { songs: HereSong[]; i: number }, key: string) => ({ ...l, i: l.songs.findIndex((y) => y.key === key) });

const ofListed = (t: HereListed): New => ({ key: `t${t.id}`, title: t.title, artist: singer(t.artist), album: singer(t.album), cover: t.cover, duration_ms: t.duration_ms, track_id: t.id });
export const trackHere = (t: Track): New => ofListed({ ...t, duration_ms: t.duration_ms ?? 0 });
export const onlineHere = (o: OnlineSong): New => ({ key: o.key, title: o.title, artist: o.artist, duration_ms: o.duration_ms, online: o.key, source: o.source });

/** A song of a decision that was shown. */
export const routeHere = (r: { track_id?: number; title?: string; artist?: string; album?: string; cover?: string; duration_ms?: number }): New => ofListed({ id: r.track_id!, title: r.title ?? '', artist: r.artist ?? '', album: r.album, cover: r.cover, duration_ms: r.duration_ms ?? 0 });

/** Plays all of something here. */
export async function listHere(what: Sendable) {
  element().load(); // in the tap: what comes later may make sound, too
  const r = await api.hereList(what as { source: string; id?: string; name?: string });
  if (!r.songs.length) throw new Error('这里面没有能放的歌');
  const seen = new Set<number>();
  const songs = r.songs.filter((x) => !seen.has(x.id) && seen.add(x.id)).map((x, n) => ({ ...ofListed(x), n }));
  const i = s.mode === 'shuffle' ? Math.floor(Math.random() * songs.length) : 0;
  set({ list: r.list, drift: false, ...(s.mode === 'shuffle' ? arrange({ songs, i }, 'shuffle') : { songs, i }) });
  lookAt('here');
  start(s.i);
}

// ---- the lock screen and the keys of a keyboard
const ms = 'mediaSession' in navigator ? navigator.mediaSession : null;
let bound = false;
function tell(x: HereSong | null) {
  if (!ms) return;
  if (!x) { ms.metadata = null; ms.playbackState = 'none'; return; }
  const art = coverURL(x.cover, 600);
  ms.metadata = new MediaMetadata({ title: x.title, artist: x.artist, album: x.album ?? '', artwork: art ? [{ src: new URL(art, location.href).href, sizes: '600x600' }] : [{ src: new URL('/icons/512.png', location.href).href, sizes: '512x512', type: 'image/png' }] });
  if (bound) return;
  bound = true;
  const on = (a: MediaSessionAction, f: MediaSessionActionHandler) => { try { ms.setActionHandler(a, f); } catch { /* not known here */ } };
  on('play', () => { if (!s.on) here.toggle(); });
  on('pause', () => { if (s.on) here.toggle(); });
  on('nexttrack', here.next);
  on('previoustrack', here.previous);
  on('seekto', (d) => { if (d.seekTime !== undefined) here.seek(d.seekTime * 1000); });
}
function position() {
  if (!ms || !hereSong(s)) return;
  ms.playbackState = s.on ? 'playing' : 'paused';
  if (!s.total) return;
  try { ms.setPositionState({ duration: s.total / 1000, position: Math.min(s.at, s.total) / 1000, playbackRate: 1 }); } catch { /* older browsers */ }
}
