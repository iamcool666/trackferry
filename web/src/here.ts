import { useSyncExternalStore } from 'react';
import { api, type Collection, type HereListed, type OnlineSong, type PlayMode, type Track } from './api';
import { singer } from './copy';
import { lookAt } from './ear';
import { coverURL } from './stage/art';
import { serverNow, type Sounding } from './live';
import { arrange, putNext, step, takeOff, type Slot } from './playlist';
import { mediaRanges, sameRanges, seekPoint, type MediaRange } from './media-clock';
import { EQ_BANDS, eqPreamp, eqPreset, eqWidth, type EQName } from './eq';

// Songs played by the browser itself, on the phone or computer that shows this page.

export interface HereSong extends Slot { title: string; artist: string; album?: string; cover?: string; duration_ms: number; track_id?: number; online?: string; source?: OnlineSong['source']; automatic?: boolean }
type New = Omit<HereSong, 'n'>;
export interface Here { songs: HereSong[]; i: number; list: string; mode: PlayMode; on: boolean; waiting: boolean; at: number; since: number; total: number; drift: boolean; seeking: number | null; seekable: MediaRange[]; buffered: MediaRange[]; volume: number; canVolume: boolean; canEQ: boolean; eq: EQName; said: { text: string; n: number } | null }

const coarse = matchMedia('(pointer: coarse)').matches;
export const hereName = '本机';
export const handheld = coarse;

const KEY = 'trackferry.here';
const QUEUE_KEY = `${KEY}.queue`;
const KEPT = 300; // fallback if the whole list won't fit in browser storage
let savedSongs: HereSong[] | undefined;
let savedRevision: string | undefined;
let s: Here = { songs: [], i: 0, list: '', mode: 'loop', on: false, waiting: false, at: 0, since: 0, total: 0, drift: false, seeking: null, seekable: [], buffered: [], volume: 100, canVolume: false, canEQ: false, eq: 'flat', said: null };
try {
  const k = JSON.parse(localStorage.getItem(KEY) ?? 'null');
  const queue = k?.revision && JSON.parse(localStorage.getItem(QUEUE_KEY) ?? 'null');
  const songs = queue?.revision === k?.revision && queue ? queue.songs : k?.songs;
  if (Array.isArray(songs) && songs[k.i]) {
    s = { ...s, songs, i: k.i, list: k.list ?? '', drift: k.drift ?? songs.length < 2, mode: k.mode ?? 'loop', at: Number(k.at) || 0, total: songs[k.i].duration_ms ?? 0, volume: k.volume ?? 100 };
    if (songs === queue?.songs) { savedSongs = songs; savedRevision = queue.revision; }
  }
} catch { /* starts empty */ }

const subs = new Set<() => void>();
const set = (x: Partial<Here>) => { s = { ...s, ...x }; subs.forEach((f) => f()); };
const say = (text: string) => set({ said: { text, n: (s.said?.n ?? 0) + 1 } });
export const useHere = () => useSyncExternalStore((f) => { subs.add(f); return () => { subs.delete(f); }; }, () => s);
export const hereSong = (x: Here): HereSong | null => x.songs[x.i] ?? null;
/** The state as it is, outside React. */
export const hereNow = () => s;
export const hereSounding = (x: Here): Sounding | null => hereSong(x) && { phase: x.seeking !== null || x.waiting ? 'resuming' : !x.on ? 'paused' : 'playing', at: x.at, since: x.since, seek: x.seekable.length > 0, read: where };

function keep() {
  const state = { i: s.i, list: s.list, drift: s.drift, mode: s.mode, volume: s.volume, at: Math.round(where()) };
  try {
    if (savedSongs !== s.songs) {
      const revision = `${Date.now()}-${Math.random()}`;
      localStorage.setItem(QUEUE_KEY, JSON.stringify({ revision, songs: s.songs }));
      savedRevision = revision;
      savedSongs = s.songs;
    }
    localStorage.setItem(KEY, JSON.stringify({ ...state, revision: savedRevision }));
  } catch {
    const from = Math.max(0, Math.min(s.i - 20, s.songs.length - KEPT));
    try { localStorage.setItem(KEY, JSON.stringify({ ...state, songs: s.songs.slice(from, from + KEPT), i: s.i - from })); } catch { /* for this visit only */ }
  }
}

// iOS leaves the volume to the buttons of the phone. On any phone those buttons are the volume:
// a page cannot read them or follow them, and a second slider of its own would only fight them
// (the keys at full, the page at a third). The page's slider is for computers.
{ const a = new Audio(); a.volume = .5; const own = a.volume === .5; s = { ...s, canEQ: own, canVolume: own && !coarse }; }
/** The page's own level: on a phone always full, so the phone's buttons alone decide. */
const level = () => s.canVolume ? s.volume / 100 : 1;
/** Sets the level where it is heard: through the sound route once it exists (Chrome ignores the element's volume then). */
function applyLevel(a: HTMLAudioElement) {
  if (graph) { a.volume = 1; graph.level.gain.value = level(); } else a.volume = level();
}

// Sound presets (eq.ts). Only where the volume is the page's: iOS stops Web Audio in the pocket.
const EQ_KEY = 'trackferry.eq';
if (s.canEQ) { try { s = { ...s, eq: eqPreset(localStorage.getItem(EQ_KEY) ?? 'flat').name }; } catch { /* 原声 */ } }
let graph: { ctx: AudioContext; pre: GainNode; bands: BiquadFilterNode[]; a: GainNode[]; b: GainNode[]; level: GainNode } | null = null;

/** Routes the sound through the preset. The route stays once made; 原声 then sets it flat. */
function applyEQ(a: HTMLAudioElement) {
  const p = eqPreset(s.eq);
  if (!graph) {
    if (p.name === 'flat') return true;
    try {
      const ctx = new AudioContext();
      const src = ctx.createMediaElementSource(a);
      const pre = ctx.createGain();
      const bands = EQ_BANDS.map((f, i) => {
        const b = ctx.createBiquadFilter();
        b.type = i === 0 ? 'lowshelf' : i === EQ_BANDS.length - 1 ? 'highshelf' : 'peaking';
        b.frequency.value = f;
        b.Q.value = 1;
        return b;
      });
      const split = ctx.createChannelSplitter(2), merge = ctx.createChannelMerger(2);
      const ga = [ctx.createGain(), ctx.createGain()], gb = [ctx.createGain(), ctx.createGain()];
      src.connect(pre);
      bands.reduce<AudioNode>((from, b) => { from.connect(b); return b; }, pre).connect(split);
      // L' = a·L + b·R and R' = a·R + b·L
      split.connect(ga[0], 0); ga[0].connect(merge, 0, 0);
      split.connect(gb[0], 1); gb[0].connect(merge, 0, 0);
      split.connect(ga[1], 1); ga[1].connect(merge, 0, 1);
      split.connect(gb[1], 0); gb[1].connect(merge, 0, 1);
      const lvl = ctx.createGain();
      merge.connect(lvl).connect(ctx.destination);
      graph = { ctx, pre, bands, a: ga, b: gb, level: lvl };
      applyLevel(a);
      // Android suspends the route when the phone locks, iOS marks it interrupted: the element
      // would go on and nothing be heard. While the music should play, the route comes back.
      ctx.addEventListener('statechange', () => { if (s.on && ctx.state !== 'running' && ctx.state !== 'closed') void ctx.resume().catch(() => {}); });
    } catch {
      return false;
    }
  }
  const { a: wa, b: wb } = eqWidth(p.width);
  graph.pre.gain.value = 10 ** (eqPreamp(p) / 20);
  graph.bands.forEach((b, i) => { b.gain.value = p.gains[i] ?? 0; });
  graph.a.forEach((g) => { g.gain.value = wa; });
  graph.b.forEach((g) => { g.gain.value = wb; });
  if (graph.ctx.state !== 'running') void graph.ctx.resume().catch(() => {});
  return true;
}

let audio: HTMLAudioElement | null = null;
// A pause this page asked for. Any other pause came from the system or the browser.
let pausing = false;
// When the page last went out of sight: going home or locking the phone.
let hiddenAt = -Infinity;
let revived = 0; // times in a row music came back after the system stopped it
// The system stopped the music as the page left and it could not be started again there.
let stolen: { intent: number; load: number } | null = null;
const pause = (a: HTMLAudioElement) => { if (!a.paused) { pausing = true; a.pause(); } };
let loaded = ''; // the song the element has
let wanted: number | null = null;
let missed = 0; // songs in a row that could not be played
let intent = 0;
let pendingList = 0;
let loadID = 0;
let seekTimer: ReturnType<typeof setTimeout> | undefined;
let lastSaved = 0;
let lastMediaTime = 0;
let cancelReceive: (() => void) | undefined;

// WebKit goes by bytes to a point of a FLAC and ends up half a minute off; it gets an index.
const webkit = 'GestureEvent' in window;
const url = (x: HereSong) => `/api/here/audio?${x.online ? new URLSearchParams({ online: x.online, title: x.title, artist: x.artist }).toString() : `track=${x.track_id}${webkit ? '&index=1' : ''}`}`;
let warm = ''; // the next song, made ready on the server
/** Has the next song made ready while this one ends. */
function ahead() {
  if (!webkit || !s.total || s.total - where() > 40_000 || s.mode === 'one') return;
  const x = s.songs[step(s.songs.length, s.i, 1, s.mode, false) ?? -1];
  if (!x || x.online || warm === x.key) return;
  warm = x.key;
  fetch(url(x), { headers: { Range: 'bytes=0-1' }, credentials: 'same-origin' }).then((r) => r.body?.cancel()).catch(() => {});
}
const nativePosition = (a: HTMLAudioElement) => Number.isFinite(a.currentTime) ? a.currentTime * 1000 : s.at;
const where = () => audio && loaded && wanted === null && s.seeking === null && !audio.seeking ? nativePosition(audio) : s.at;
const mark = (x: Partial<Here> = {}) => { set({ at: where(), since: serverNow(), ...x }); position(); };

function syncRanges(a: HTMLAudioElement) {
  const seekable = mediaRanges(a.seekable), buffered = mediaRanges(a.buffered);
  if (!sameRanges(s.seekable, seekable) || !sameRanges(s.buffered, buffered)) set({ seekable, buffered });
  if (wanted !== null && seekable.length) {
    const to = wanted;
    wanted = null;
    beginSeek(a, to, seekable);
  }
}

function beginSeek(a: HTMLAudioElement, ms: number, ranges = mediaRanges(a.seekable)) {
  const point = seekPoint(ranges, ms);
  if (point === null) { say('这首歌暂时不能定位'); return; }
  clearTimeout(seekTimer);
  const at = where();
  if (!a.seeking && Math.abs(point - nativePosition(a)) < 1) { mark({ seeking: null }); return; }
  set({ at, seeking: point });
  try {
    a.currentTime = point / 1000;
  } catch {
    mark({ at, seeking: null });
    say('没能跳到那里，稍后再试');
    return;
  }
  const load = loadID;
  seekTimer = setTimeout(() => {
    if (load !== loadID || s.seeking === null) return;
    say('还没跳到那里，正在等音频');
  }, 5000);
}

function element() {
  if (audio) return audio;
  const a = audio = new Audio();
  a.preload = 'auto';
  a.setAttribute('playsinline', '');
  a.setAttribute('x-webkit-airplay', 'allow');
  // iOS lets music go on in the pocket for the playback kind of sound (Safari 17); with the
  // default it may stop it when the app leaves the screen. An element in the page is kept too.
  try { const session = (navigator as { audioSession?: { type: string } }).audioSession; if (session) session.type = 'playback'; } catch { /* older browsers */ }
  a.hidden = true;
  (document.body ?? document.documentElement).appendChild(a);
  applyLevel(a);
  a.addEventListener('play', () => {
    // A route that the browser stopped (another tab, a call) starts again with the music.
    if (graph && graph.ctx.state !== 'running') void graph.ctx.resume().catch(() => {});
    else if (!graph && s.eq !== 'flat' && !applyEQ(a)) set({ eq: 'flat' }); // a preset kept from before, made inside a tap
    if (loaded && !a.paused) mark({ on: true, waiting: true });
    stolen = null;
  });
  a.addEventListener('pause', () => {
    const asked = pausing;
    pausing = false;
    if (!loaded || !a.paused || a.ended || a.error) { keep(); return; }
    // Stopped by the system just as the page left the screen (home, lock): carry on. Later pauses
    // of its own, such as headphones taken out, are left alone.
    if (!asked && s.on && performance.now() - hiddenAt < 1500 && revived < 3) {
      revived++;
      const load = loadID, action = intent;
      if (graph && graph.ctx.state !== 'running') void graph.ctx.resume().catch(() => {});
      a.play().catch(() => {
        if (load !== loadID || action !== intent) return;
        stolen = { intent: action, load };
        mark({ on: false, waiting: false });
        keep();
      });
      return;
    }
    mark({ on: false, waiting: false });
    keep();
  });
  a.addEventListener('waiting', () => { if (loaded && !a.paused) mark({ waiting: true }); });
  a.addEventListener('playing', () => {
    if (!loaded || a.paused) return;
    missed = 0;
    syncRanges(a);
    if (wanted !== null) {
      wanted = null;
      say('这首歌不能从上次的位置接着放，已从头播放');
    }
    mark({ on: true, waiting: false });
    if (!document.hidden) revived = 0;
  });
  a.addEventListener('seeking', () => { if (loaded && a.seeking && s.seeking === null) set({ at: lastMediaTime, seeking: nativePosition(a) }); });
  a.addEventListener('seeked', () => {
    if (!loaded || a.seeking || wanted !== null) return;
    clearTimeout(seekTimer);
    lastMediaTime = nativePosition(a);
    mark({ at: lastMediaTime, seeking: null, waiting: !a.paused && a.readyState < 3 });
    keep();
  });
  for (const name of ['canplay', 'progress']) a.addEventListener(name, () => { if (loaded) syncRanges(a); });
  a.addEventListener('loadedmetadata', () => {
    if (!loaded) return;
    if (Number.isFinite(a.duration) && a.duration > 0) set({ total: a.duration * 1000 });
    syncRanges(a);
    mark();
  });
  a.addEventListener('durationchange', () => { if (loaded && Number.isFinite(a.duration) && a.duration > 0) { set({ total: a.duration * 1000 }); position(); } });
  let last = -1, lastLoad = -1;
  a.addEventListener('timeupdate', () => {
    if (!loaded) return;
    const t = a.currentTime;
    // Self-healing: the element plays on from a jump whose end was never told (it happens on
    // iOS); the clock goes by the element again, so the words do not stay stuck or run off.
    if (s.seeking !== null && wanted === null && !a.seeking && !a.paused && a.readyState >= HTMLMediaElement.HAVE_FUTURE_DATA && lastLoad === loadID && t > last && t - last < 1) {
      clearTimeout(seekTimer);
      mark({ at: nativePosition(a), seeking: null, waiting: false });
    }
    if (!a.seeking && s.seeking === null && wanted === null) lastMediaTime = nativePosition(a);
    if (lastLoad === loadID && s.waiting && s.seeking === null && !a.paused && !a.seeking && wanted === null && t > last && t - last < 1) mark({ waiting: false });
    last = t;
    lastLoad = loadID;
    syncRanges(a);
    ahead();
    if (Date.now() - lastSaved > 5000 && s.seeking === null && wanted === null) {
      lastSaved = Date.now();
      keep();
    }
  });
  a.addEventListener('ended', () => go(1, false));
  a.addEventListener('error', broken);
  addEventListener('pagehide', keep);
  document.addEventListener('visibilitychange', () => {
    if (document.hidden) { hiddenAt = performance.now(); if (s.on) keep(); return; }
    revived = 0;
    // Back on the screen after the system stopped it there: it starts where it was, once.
    const was = stolen;
    stolen = null;
    if (was && was.intent === intent && was.load === loadID && loaded && a.paused && hereSong(s)) {
      set({ on: true, waiting: true });
      a.play().catch(() => { if (was.load === loadID && was.intent === intent) { mark({ on: false, waiting: false }); say('系统停下了播放，点一下继续'); } });
    }
    if (s.on && graph && graph.ctx.state !== 'running') void graph.ctx.resume().catch(() => {});
  });
  return a;
}

async function broken() {
  const x = hereSong(s);
  if (!x || loaded !== x.key) return;
  const load = loadID;
  const action = intent;
  const at = s.seeking === null ? lastMediaTime : s.at;
  loaded = '';
  wanted = null;
  clearTimeout(seekTimer);
  set({ at, seeking: null, seekable: [], buffered: [] });
  let why = '放不了';
  try {
    const r = await fetch(url(x), { headers: { Range: 'bytes=0-1' }, credentials: 'same-origin' });
    if (!r.ok) why = (await r.json()).message ?? why; else r.body?.cancel();
  } catch { why = '连不上 TrackFerry'; }
  if (load !== loadID || action !== intent || hereSong(s)?.key !== x.key) return;
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
  loadID++;
  clearTimeout(seekTimer);
  wanted = at > 0 ? at : null;
  lastMediaTime = at;
  lastSaved = Date.now();
  loaded = x.key;
  a.src = url(x);
  set({ i, at, since: serverNow(), total: x.duration_ms, on: sound, waiting: sound, seeking: null, seekable: [], buffered: [] });
  tell(x);
  position();
  keep();
  if (sound) sing(a);
  if (sound) drift();
}

function sing(a: HTMLAudioElement) {
  const load = loadID;
  const action = intent;
  a.play().catch((e: DOMException) => {
    if (load !== loadID || action !== intent || e.name === 'AbortError') return;
    if (e.name === 'NotAllowedError') { mark({ on: false, waiting: false }); say('浏览器拦下了声音，再点一次播放'); }
    else if (!a.error) { mark({ on: false, waiting: false }); say('没能开始播放，点一下重试'); }
  });
}

let filling: object | null = null;
/** Songs of the library come after one that was put on alone, and after those. */
async function drift() {
  const x = hereSong(s);
  if (!s.drift || !x || s.songs.length - s.i > 4 || filling) return;
  const f = filling = {};
  const action = intent;
  try {
    const r = await api.hereList({ source: 'random' });
    if (!s.drift || !hereSong(s) || action !== intent || f !== filling) return;
    const had = new Set(s.songs.map((y) => y.key));
    let n = Math.max(0, ...s.songs.map((y) => y.n));
    const more = r.songs.map(ofListed).filter((y) => !had.has(y.key) && had.add(y.key)).map((y) => ({ ...y, n: ++n, automatic: true }));
    const songs = [...s.songs, ...more], cut = Math.max(0, s.i - 100); // what was heard long ago may go
    set({ songs: songs.slice(cut), i: s.i - cut, list: r.list });
    keep();
  } catch { /* tried again with the next song */ } finally { if (filling === f) filling = null; }
}

function newIntent() { cancelReceive?.(); cancelPendingList(); filling = null; return ++intent; }

function go(dir: 1 | -1, by: boolean) {
  if (by) newIntent();
  let to = step(s.songs.length, s.i, dir, s.mode, by);
  if (to === null) {
    mark({ on: false, waiting: false });
    if (audio && s.seekable.length) beginSeek(audio, 0);
    else keep();
    return;
  }
  if (!by && s.mode === 'one' && audio) { audio.currentTime = 0; sing(audio); return; }
  if (dir === 1 && to === 0 && s.mode === 'shuffle' && s.songs.length > 2) { // round again, in another order
    const on = hereSong(s)!;
    const next = arrange(s, 'shuffle');
    set(next);
    to = next.songs.findIndex((x) => x !== on);
  }
  start(to);
}

type Picker = HTMLAudioElement & { webkitShowPlaybackTargetPicker?: () => void; remote?: { prompt(): Promise<void> } };
/** Whether this browser can send this device's sound on (AirPlay in Safari, Cast in Chrome). */
export const canSendOn = () => typeof window !== 'undefined' && ('WebKitPlaybackTargetAvailabilityEvent' in window || ('remote' in HTMLMediaElement.prototype));
/** Opens the browser's own list of AirPlay or Cast devices. The equalizer sends sound through
 * the page itself, which those devices do not get, so it is said rather than left to puzzle. */
export async function sendOn(): Promise<string> {
  const a = element() as Picker;
  if (a.webkitShowPlaybackTargetPicker) { a.webkitShowPlaybackTargetPicker(); return graph ? '用过均衡器后投出去可能没声音，刷新页面再投即可' : ''; }
  if (a.remote) {
    try { await a.remote.prompt(); } catch (e) { const n = (e as DOMException).name; return n === 'NotFoundError' ? '附近没找到能投放的设备' : n === 'AbortError' ? '' : '这首还没开始放，先在本机放起来再投'; }
    return graph ? '用过均衡器后投出去可能没声音，刷新页面再投即可' : '';
  }
  return '这个浏览器不支持从本机投放';
}
export const here = {
  toggle() {
    newIntent();
    const a = element(), x = hereSong(s);
    if (!x) return;
    if (s.on) { pause(a); mark({ on: false, waiting: false }); } else if (loaded !== x.key) start(s.i, s.at);
    else { set({ on: true, waiting: true }); sing(a); void drift(); }
  },
  next: () => go(1, true),
  previous() { if (where() > 4000 && s.seekable.length) here.seek(0); else go(-1, true); },
  seek(ms: number) {
    if (!audio || !loaded || !hereSong(s) || !Number.isFinite(ms)) return;
    newIntent();
    wanted = null;
    const end = s.total > 0 ? s.total - 50 : ms;
    beginSeek(audio, Math.max(0, Math.min(ms, end)));
  },
  mode(m: PlayMode) { set({ ...(m === 'one' ? {} : arrange(s, m)), mode: m }); keep(); },
  clearUpcoming() {
    newIntent();
    const current = hereSong(s);
    if (!current) return;
    set({ songs: [current], i: 0, drift: false, list: '' });
    keep();
  },
  moveUpcoming(key: string, by: -1 | 1) {
    const at = s.songs.findIndex((item) => item.key === key);
    if (at <= s.i || at + by <= s.i || at + by >= s.songs.length) return;
    const songs = [...s.songs];
    [songs[at], songs[at + by]] = [songs[at + by], songs[at]];
    set({ songs: s.mode === 'shuffle' ? songs : songs.map((item, n) => ({ ...item, n })) });
    keep();
  },
  volume(v: number) { if (!s.canVolume) return; set({ volume: v }); applyLevel(element()); keep(); },
  /** Picks a sound preset; false where it cannot be made, and then nothing changed. */
  eq(name: EQName) {
    if (!s.canEQ) return false;
    const was = s.eq;
    set({ eq: eqPreset(name).name });
    if (!applyEQ(element())) { set({ eq: was }); return false; }
    try { localStorage.setItem(EQ_KEY, s.eq); } catch { /* for this visit only */ }
    return true;
  },
  /** Plays another copy of the song on screen from where it is: same place in the list, same moment. */
  swap(x: New) {
    const cur = hereSong(s);
    if (!cur) return;
    newIntent();
    const at = Math.round(where());
    const songs = [...s.songs];
    songs[s.i] = { ...x, key: `${x.key}@v${Date.now()}`, n: cur.n, automatic: cur.automatic };
    set({ songs });
    start(s.i, at, s.on);
  },
  jump: (key: string) => { const i = s.songs.findIndex((x) => x.key === key); if (i >= 0) { newIntent(); start(i); } },
  remove(key: string) {
    const was = hereSong(s)?.key, next = takeOff(s, key);
    if (!next.songs.length) { here.stop(); return; }
    set(next);
    if (was === key) { newIntent(); start(next.i, 0, s.on); } else keep();
  },
  stop() {
    newIntent();
    loadID++;
    wanted = null;
    loaded = '';
    clearTimeout(seekTimer);
    if (audio) pause(audio);
    audio?.removeAttribute('src');
    audio?.load();
    set({ songs: [], i: 0, list: '', drift: false, on: false, waiting: false, at: 0, total: 0, seeking: null, seekable: [], buffered: [] });
    tell(null);
    keep();
  },
};

/** A single track starts a single-track queue. Queue edits only add the requested track. */
export function playHere(x: New, how: 'now' | 'next' | 'last' = 'now') {
  const on = hereSong(s);
  if (how === 'next' && on) { cancelPendingList(); set(putNext(s, x)); keep(); return true; }
  if (how === 'last' && on) {
    cancelPendingList();
    if (on.key === x.key) return true;
    const songs = s.songs.filter((item) => item.key !== x.key);
    const n = songs.reduce((max, item) => Math.max(max, item.n), 0) + 1;
    const at = songs.findIndex((item, i) => i > songs.indexOf(on) && item.automatic);
    songs.splice(at < 0 ? songs.length : at, 0, { ...x, n });
    set({ songs, i: songs.indexOf(on) });
    keep();
    return true;
  }
  newIntent();
  set({ songs: [{ ...x, n: 0 }], i: 0, drift: true, list: '' });
  lookAt('here');
  start(0);
  return false;
}

const ofListed = (t: HereListed): New => ({ key: `t${t.id}`, title: t.title, artist: singer(t.artist), album: singer(t.album), cover: t.cover, duration_ms: t.duration_ms, track_id: t.id });
export const trackHere = (t: Track): New => ofListed({ ...t, duration_ms: t.duration_ms ?? 0 });
export const onlineHere = (o: OnlineSong): New => ({ key: o.key, title: o.title, artist: o.artist, album: o.album, cover: o.cover, duration_ms: o.duration_ms, online: o.key, source: o.source });

/** A song of a decision that was shown. */
export const routeHere = (r: { track_id?: number; title?: string; artist?: string; album?: string; cover?: string; duration_ms?: number }): New => ofListed({ id: r.track_id!, title: r.title ?? '', artist: r.artist ?? '', album: r.album, cover: r.cover, duration_ms: r.duration_ms ?? 0 });

/** drift: the library at random follows the list, for songs that were not picked as a whole. */
export function playListHere(list: string, tracks: HereListed[], trackID?: number, drift = false) {
  newIntent();
  const seen = new Set<number>();
  const songs = tracks.filter((x) => !seen.has(x.id) && seen.add(x.id)).map((x, n) => ({ ...ofListed(x), n }));
  if (!songs.length) throw new Error('这里面没有能放的歌');
  const i = trackID === undefined ? (s.mode === 'shuffle' ? Math.floor(Math.random() * songs.length) : 0) : songs.findIndex((x) => x.track_id === trackID);
  if (i < 0) throw new Error('这首已不在播放列表里，刷新后再试');
  set({ list, drift, ...(s.mode === 'shuffle' ? arrange({ songs, i }, 'shuffle') : { songs, i }) });
  lookAt('here');
  start(s.i);
}

/** Starts a complete LX online playlist in the browser's own queue. */
export function playOnlineListHere(list: string, tracks: OnlineSong[]) {
  const seen = new Set<string>();
  const songs = tracks.filter((x) => x.key && !seen.has(x.key) && seen.add(x.key)).map((x, n) => ({ ...onlineHere(x), n }));
  if (!songs.length) throw new Error('这个在线歌单里没有能播放的歌');
  newIntent();
  const i = s.mode === 'shuffle' ? Math.floor(Math.random() * songs.length) : 0;
  set({ list, drift: false, ...(s.mode === 'shuffle' ? arrange({ songs, i }, 'shuffle') : { songs, i }) });
  lookAt('here');
  start(s.i);
}

/** Reads the complete list when the page has only loaded a preview. */
export async function listHere(what: Collection, trackID?: number) {
  const action = ++pendingList;
  let r;
  try {
    r = await api.hereList(what);
  } catch (error) {
    if (action !== pendingList) return;
    throw error;
  }
  if (action !== pendingList) return;
  playListHere(r.list, r.songs, trackID);
  return true;
}

export function cancelPendingList() { pendingList++; }

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
  on('seekbackward', (d) => here.seek(where() - (d.seekOffset ?? 10) * 1000));
  on('seekforward', (d) => here.seek(where() + (d.seekOffset ?? 10) * 1000));
  on('stop', here.stop);
}
function position() {
  if (!ms || !hereSong(s)) return;
  ms.playbackState = s.on && !s.waiting && s.seeking === null ? 'playing' : 'paused';
  if (!s.total) return;
  try { ms.setPositionState({ duration: s.total / 1000, position: Math.max(0, Math.min(where(), s.total)) / 1000, playbackRate: audio?.playbackRate || 1 }); } catch { /* older browsers */ }
}

/** Capture the current entry and the last confirmed media position for handoff. */
export function hereTransfer() {
  const song = hereSong(s);
  if (!song || s.waiting || s.seeking !== null) return null;
  const stamp = intent, queue = s.songs, index = s.i, load = loadID, mode = s.mode;
  return {
    items: queue.map((item) => ({ id: item.track_id, online: item.online, title: item.title, artist: item.artist })),
    index, mode, position_ms: Math.round(where()),
    pauseIfCurrent: () => {
      if (intent !== stamp || loadID !== load || s.songs !== queue || s.mode !== mode || s.i !== index) return false;
      if (s.on) here.toggle();
      return true;
    },
  };
}

/**
 * After a handoff the source has stopped where it was: if this device is more than a moment away
 * from there, it moves. index is the song both were on; nothing moves once anything else changed.
 */
export function correctHere(index: number, ms: number, key: string) {
  const song = hereSong(s);
  if (!song || s.i !== index || song.key !== key || !s.on || s.waiting || s.seeking !== null || !s.seekable.length) return false;
  if (Math.abs(where() - ms) < 1500) return false;
  here.seek(ms);
  return true;
}

/** The call starts in the user's click. Confirmation comes from the media element. */
export function receiveHere(items: import('./api').TVTrack[], index: number, at: number, mode: PlayMode, signal: AbortSignal, confirmed: () => Promise<void>) {
  if (signal.aborted) return Promise.reject(signal.reason);
  if (!items[index] || items.some((item) => !item.id && !item.online)) return Promise.reject(new Error('队列中有无法识别的曲目'));
  newIntent();
  const songs = items.map((item, n) => ({
    ...(item.online ? { key: item.online, online: item.online, title: item.title, artist: item.artist, duration_ms: item.duration_ms } : ofListed(item)),
    key: `${item.online || `t${item.id}`}@${n}`, n,
  }));
  set({ songs, i: index, list: '', mode, drift: songs.length < 2 });
  const current = element();
  return new Promise<void>((resolve, reject) => {
    let firstPosition: number | undefined;
    const cleanup = () => {
      clearTimeout(timeout);
      current.removeEventListener('timeupdate', playing);
      current.removeEventListener('error', failed);
      signal.removeEventListener('abort', failed);
      if (cancelReceive === superseded) cancelReceive = undefined;
    };
    const failed = () => {
      cleanup();
      if (load === loadID && s.on) here.toggle();
      reject(new Error('本机没有确认接续，电视继续播放'));
    };
    const superseded = () => { cleanup(); reject(new Error('本机播放已变化，电视继续播放')); };
    const playing = () => {
      if (load !== loadID) { superseded(); return; }
      if (current.paused || current.seeking || s.waiting || wanted !== null || s.seeking !== null || current.readyState < HTMLMediaElement.HAVE_FUTURE_DATA) {
        firstPosition = undefined;
        return;
      }
      const now = nativePosition(current);
      if (firstPosition === undefined) {
        if (Math.abs(now - at) > 2500) { failed(); return; }
        firstPosition = now;
        return;
      }
      if (now - firstPosition < 40) return;
      cleanup();
      confirmed().then(resolve, reject);
    };
    const load = loadID + 1;
    const timeout = setTimeout(failed, 11000);
    current.addEventListener('timeupdate', playing);
    current.addEventListener('error', failed);
    signal.addEventListener('abort', failed, { once: true });
    cancelReceive = superseded;
    start(index, at);
  });
}
