import { useCallback, useEffect, useRef, useState } from 'react';
import { api, ApiError } from '../api';
import { length } from '../copy';
import { position, type Sounding } from '../live';
import { lineAt, parseLRC, plainLyrics, restAt, spanOf, type Line } from '../lyrics';
import { Fish } from './Fish';
import { useToast } from './Toast';
import { Blast, nextKind, type BlastKind } from './BlastLine';
import type { MediaRange } from '../media-clock';

export interface Words { lines: Line[]; timed: boolean; /** How much sooner the lines come, in ms. */ offset: number; setOffset: (ms: number) => void; saveError?: boolean; /** The platform they came from, when the song has none of its own. */ from?: string }

/** The words of a library or online song; null until they are known, or when there are none. */
/** Sets a custom property on one element, only when it changed. */
function mark(el: HTMLElement | undefined, name: string, value: string) {
  if (el && el.style.getPropertyValue(name) !== value) el.style.setProperty(name, value);
}

export function useWords(trackID?: number, online?: string): Words | null {
  const key = trackID ? `t${trackID}` : online ?? '';
  const [got, setGot] = useState<{ id: string; lines: Line[]; timed: boolean; offset: number; from: string } | null>(null);
  const writes = useRef(new Map<number, { timer?: ReturnType<typeof setTimeout>; pending?: number; saving: boolean }>());
  const [saveError, setSaveError] = useState<number>();
  useEffect(() => {
    if (!key) return;
    let on = true;
    (trackID ? api.lyrics(trackID) : api.onlineLyrics(online!)).then((r) => {
      if (!on) return;
      const lines = parseLRC(r.lyrics ?? '');
      setGot({ id: key, lines: lines.length ? lines : plainLyrics(r.lyrics ?? ''), timed: lines.length > 0, offset: r.offset_ms ?? 0, from: r.from ?? '' });
    }).catch(() => {});
    return () => { on = false; };
  }, [key]); // eslint-disable-line react-hooks/exhaustive-deps
  const setOffset = useCallback((ms: number) => {
    const to = Math.max(-60000, Math.min(60000, ms));
    setGot((g) => g?.id === key ? { ...g, offset: to } : g);
    // An online song's lyrics move for this listen only: there is no song of the library to keep it on.
    if (!trackID) return;
    setSaveError(undefined);
    const jobs = writes.current;
    const job = jobs.get(trackID) ?? { saving: false };
    jobs.set(trackID, job);
    if (job.timer) clearTimeout(job.timer);
    job.pending = to;
    const flush = async () => {
      job.timer = undefined;
      if (job.saving || job.pending === undefined) return;
      const value = job.pending;
      job.pending = undefined;
      job.saving = true;
      try {
        await api.setLyricsOffset(trackID, value);
      } catch {
        if (job.pending === undefined) setSaveError(trackID);
      } finally {
        job.saving = false;
        if (job.pending === undefined) jobs.delete(trackID);
        else if (!job.timer) void flush();
      }
    };
    job.timer = setTimeout(flush, 350);
  }, [trackID, key]);
  return key && got?.id === key && got.lines.length ? { lines: got.lines, timed: got.timed, offset: got.offset, from: got.from, setOffset, saveError: !!trackID && saveError === trackID } : null;
}

const SIZE = 'trackferry.lyrics.size';
const fromName: Record<string, string> = { kuwo: '酷我', kugou: '酷狗', migu: '咪咕', netease: '网易云' };
const sizes = [{ v: 'small', label: '小' }, { v: '', label: '中' }, { v: 'large', label: '大' }];

/** All of the words. The line being sung stays in view unless the reader has scrolled away; a tap on a line goes there. */
export function LyricSheet({ words, now, big, seek, blast }: { words: Words; now: Sounding | null; big: boolean; seek?: (ms: number) => void; blast?: boolean }) {
  const box = useRef<HTMLOListElement>(null);
  const [i, setI] = useState(-1);
  const line = useRef(-1);
  // The line the music is heading for during an interlude: its dots fill as the interlude passes.
  const [rest, setRest] = useState(-1);
  const resting = useRef(-1);
  const flights = useRef<Animation[]>([]);
  const [free, setFree] = useState(false);
  const [tools, setTools] = useState(false);
  const [size, setSize] = useState(() => { try { return localStorage.getItem(SIZE) ?? ''; } catch { return ''; } });
  const { lines, timed, offset } = words;
  const follow = !!now && timed;
  // Fireworks on the line being sung: the next kind is drawn once per new line, never the same twice.
  const [kind, setKind] = useState<BlastKind>('shatter');
  const lastKind = useRef<BlastKind | null>(null);
  const live = !!blast && big && i >= 0 && rest < 0 && !!lines[i];
  useEffect(() => {
    if (i < 0) return;
    const k = nextKind(lastKind.current);
    lastKind.current = k;
    setKind(k);
  }, [i]);
  useEffect(() => {
    if (!now || !timed) { line.current = -1; setI(-1); resting.current = -1; setRest(-1); return; }
    let frame = 0;
    let last = -Infinity;
    const sample = (time: number) => {
      if (time - last >= 33) {
        last = time;
        const at = position(now) + offset;
        const next = lineAt(lines, at), r = restAt(lines, next, at), rk = r ? r.next : -1;
        if (next !== line.current) { line.current = next; setI(next); }
        if (rk !== resting.current) { resting.current = rk; setRest(rk); }
        // How far the singing is, as two numbers the styles read: no React work on every frame.
        // Written on the one line that uses each, never on the list: a value set on the list
        // restyles every line thirty times a second, which is what made phones stutter.
        const b = box.current;
        if (b) {
          mark(b.children[rk] as HTMLElement | undefined, '--rest', r ? ((at - r.from) / (r.to - r.from)).toFixed(2) : '0');
          mark(b.children[next] as HTMLElement | undefined, '--p', next >= 0 ? Math.min(1, Math.max(0, (at - lines[next].at) / spanOf(lines, next))).toFixed(2) : '0');
        }
      }
      if (now.phase === 'playing' && !document.hidden) frame = requestAnimationFrame(sample);
    };
    const resume = () => { cancelAnimationFrame(frame); last = -Infinity; sample(performance.now()); };
    resume();
    document.addEventListener('visibilitychange', resume);
    return () => { cancelAnimationFrame(frame); document.removeEventListener('visibilitychange', resume); };
  }, [lines, offset, now?.phase, now?.at, now?.since, now?.read]); // eslint-disable-line react-hooks/exhaustive-deps
  const returnTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(returnTimer.current), []);
  // During an interlude the coming line takes the middle, with its dots.
  const focus = rest >= 0 ? rest : i;
  useEffect(() => () => flights.current.forEach((a) => a.cancel()), []);
  useEffect(() => {
    const b = box.current, el = b?.children[Math.max(focus, 0)] as HTMLElement | undefined;
    if (!b || !el || !follow || free) return;
    const want = focus < 0 ? 0 : el.offsetTop + el.offsetHeight / 2 - b.clientHeight * .42;
    const calm = matchMedia('(prefers-reduced-motion: reduce)').matches;
    const delta = Math.max(0, Math.min(want, b.scrollHeight - b.clientHeight)) - b.scrollTop;
    flights.current.forEach((a) => a.cancel());
    flights.current = [];
    if (calm || Math.abs(delta) < 1 || Math.abs(delta) > b.clientHeight || typeof el.animate !== 'function') {
      b.scrollTo({ top: want, behavior: calm ? 'auto' : 'smooth' });
      return;
    }
    // Lines move one after another, the upper ones first, like a sheet of paper settling:
    // the box jumps at once and every line in sight is carried from where it was.
    b.scrollTo({ top: want, behavior: 'auto' });
    const from = b.scrollTop - Math.abs(delta) - 40, to = b.scrollTop + b.clientHeight + Math.abs(delta) + 40, at = Math.max(focus, 0);
    for (let k = 0; k < b.children.length; k++) {
      const li = b.children[k] as HTMLElement;
      if (li.offsetTop + li.offsetHeight < from) continue;
      if (li.offsetTop > to) break;
      const lag = Math.max(0, Math.min(9, k - at + 2)) * 42;
      flights.current.push(li.animate([{ translate: `0 ${delta}px` }, { translate: '0 0' }], { duration: 680, delay: lag, easing: 'cubic-bezier(.2, 1.1, .32, 1)', fill: 'backwards' }));
    }
  }, [focus, big, size, follow, free]);
  useEffect(() => { clearTimeout(returnTimer.current); setFree(false); }, [lines]);
  // Which character of the line is being sung: the line's own span, walked by the clock.
  const [sung, setSung] = useState(-1);
  const sungNow = () => {
    if (!live || !follow) return -1;
    const cur = lines[i];
    const span = spanOf(lines, i);
    if (!(span > 0)) return -1;
    const f = (position(now!) + offset - cur.at) / span;
    if (f < 0) return -1;
    const n = Array.from(cur.text).length;
    return Math.min(n - 1, Math.floor(f * n));
  };
  useEffect(() => {
    if (!live) { setSung(-1); return; }
    const t = setInterval(() => setSung(sungNow()), 90);
    setSung(sungNow());
    return () => clearInterval(t);
  }, [live, i, now?.phase, now?.at, now?.since]); // eslint-disable-line react-hooks/exhaustive-deps
  const mine = () => {
    if (!follow) return;
    setFree(true);
    clearTimeout(returnTimer.current);
    returnTimer.current = setTimeout(() => setFree(false), 4000);
  };
  const go = (l: Line) => { clearTimeout(returnTimer.current); setFree(false); seek!(Math.max(0, l.at - offset)); };
  const jump = timed && seek;
  const sized = (v: string) => { setSize(v); try { localStorage.setItem(SIZE, v); } catch { /* for this visit only */ } };
  return (
    <div className={`lyric ${size ? `lyric-${size}` : ''}`}>
      <ol ref={box} className={`sheet-words ${follow ? 'follow' : ''} ${free ? 'free' : ''} ${jump ? 'jump' : ''} ${live ? 'blasting' : ''}`} aria-label="歌词" tabIndex={0} onWheel={mine} onTouchMove={mine} onKeyDown={mine}>
        {lines.map((l, k) => {
          const now0 = k === i && rest < 0, words = <span className="w">{live && k === i ? <Blast text={l.text} seed={k} kind={kind} sung={sung} /> : l.text}</span>;
          return (
            <li key={k} className={now0 ? 'now' : k === rest ? 'next' : ''} aria-current={k === i || undefined} style={{ '--d': follow ? Math.min(5, Math.abs(k - Math.max(focus, 0))) : 0 } as React.CSSProperties}>
              {k === rest && <span className="rest" aria-hidden="true"><i /><i /><i /></span>}
              {jump ? <button type="button" onClick={() => go(l)} aria-label={`跳到 ${length(l.at) || '0:00'}：${l.text}`}>{words}</button> : words}
            </li>
          );
        })}
      </ol>
      {words.from && <p className="lyric-from">歌词来自{fromName[words.from] ?? words.from}，这首歌自己没有</p>}
      {tools ? (
        <div className="lyric-tools" role="group" aria-label="调整歌词">
          {timed && <div><button type="button" onClick={() => words.setOffset(offset - 100)} aria-label="歌词延后 0.1 秒">−0.1 秒</button><button type="button" className="num" onClick={() => words.setOffset(words.saveError ? offset : 0)} disabled={!offset && !words.saveError} aria-label={words.saveError ? '重试保存歌词校准' : '歌词时间还原'}>{words.saveError ? '重试保存' : offset ? `${offset > 0 ? '提前' : '延后'} ${Math.abs(offset / 1000).toFixed(1)} 秒` : '还原'}</button><button type="button" onClick={() => words.setOffset(offset + 100)} aria-label="歌词提前 0.1 秒">+0.1 秒</button></div>}
          <div>{sizes.map((x) => <button key={x.v} type="button" aria-pressed={size === x.v} onClick={() => sized(x.v)} aria-label={`${x.label}号字`}>{x.label}</button>)}</div>
          <button type="button" className="lyric-x" onClick={() => setTools(false)} aria-label="收起"><svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true"><path d="M2 2l12 12M14 2L2 14" stroke="currentColor" strokeWidth="2" strokeLinecap="round" /></svg></button>
        </div>
      ) : <button type="button" className="lyric-open" onClick={() => setTools(true)} aria-label="调整歌词" title="调整歌词"><svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" aria-hidden="true"><path d="M4 7h9M19 7h1M4 17h1M11 17h9" /><circle cx="16" cy="7" r="2.4" /><circle cx="8" cy="17" r="2.4" /></svg></button>}
    </div>
  );
}

/** Where the listener has put a song, shown until it is there. One request at a time, the newest place wins. */
export function useSeek(key: string | number | undefined, to: ((ms: number, signal: AbortSignal) => Promise<void>) | null) {
  const [aim, setAim] = useState<{ key: typeof key; ms: number } | null>(null);
  const [toast, say] = useToast();
  const abort = useRef<AbortController | null>(null);
  useEffect(() => () => { abort.current?.abort(); abort.current = null; }, [key]);
  const seek = async (ms: number) => {
    if (!to) return;
    abort.current?.abort();
    const controller = abort.current = new AbortController();
    setAim({ key, ms });
    try {
      await to(ms, controller.signal);
    } catch (e) {
      if (!controller.signal.aborted) say({ tone: 'bad', text: (e as ApiError).message });
    } finally {
      if (abort.current === controller) { abort.current = null; setAim(null); }
    }
  };
  return { seek: to ? seek : undefined, aim: aim && aim.key === key ? aim.ms : undefined, toast };
}

/** How far the song is; it can be dragged. The fish swims at its head. */
export function Progress({ now, total, seek, aim, buffered = [], estimated = false }: { now: Sounding; total?: number; seek?: (ms: number) => void; aim?: number | null; buffered?: MediaRange[]; estimated?: boolean }) {
  const [at, setAt] = useState(() => position(now));
  const [drag, setDrag] = useState<number | null>(null);
  const [hover, setHover] = useState<number | null>(null);
  const bar = useRef<HTMLDivElement>(null);
  const grip = useRef<number | null>(null);
  useEffect(() => {
    const f = () => setAt(position(now));
    f();
    const t = now.phase === 'playing' ? setInterval(f, 100) : undefined;
    document.addEventListener('visibilitychange', f);
    return () => { clearInterval(t); document.removeEventListener('visibilitychange', f); };
  }, [now.phase, now.at, now.since, now.read]); // eslint-disable-line react-hooks/exhaustive-deps
  const here = Math.max(0, total ? Math.min(at, total) : at);
  const can = !!seek && !!total;
  const preview = (can ? drag : null) ?? aim ?? (can ? hover : null);
  const under = (x: number) => { const r = bar.current!.getBoundingClientRect(); return Math.max(0, Math.min(1, (x - r.left) / Math.max(1, r.width))) * total!; };
  const down = (e: React.PointerEvent) => {
    if (!can || e.button) return;
    e.stopPropagation();
    grip.current = e.pointerId;
    e.currentTarget.setPointerCapture(e.pointerId);
    setDrag(under(e.clientX));
  };
  const move = (e: React.PointerEvent) => {
    if (grip.current === e.pointerId) setDrag(under(e.clientX));
    else if (can && e.pointerType === 'mouse') setHover(under(e.clientX));
  };
  const up = (e: React.PointerEvent) => {
    if (grip.current !== e.pointerId) return;
    grip.current = null;
    setDrag(null);
    setHover(null);
    if (e.type === 'pointerup') seek?.(under(e.clientX));
  };
  const key = (e: React.KeyboardEvent) => {
    if (!can) return;
    const from = aim ?? here;
    const to = { ArrowLeft: from - 5000, ArrowRight: from + 5000, ArrowDown: from - 5000, ArrowUp: from + 5000, PageDown: from - 30000, PageUp: from + 30000, Home: 0, End: total! }[e.key];
    if (to === undefined) return;
    e.preventDefault();
    const ms = Math.max(0, Math.min(total!, to));
    seek!(ms);
  };
  return (
    <div className={`swim ${now.phase} ${can ? 'can' : ''} ${drag !== null ? 'held' : ''}`}>
      <span className="num" title={estimated ? '按取流和音箱回读估算，可能有延迟' : undefined}>{estimated && '≈'}{length(here) || '0:00'}</span>
      <div ref={bar} style={{ '--f': total ? here / total : 0 } as React.CSSProperties} onPointerDown={down} onPointerMove={move} onPointerUp={up} onPointerCancel={up} onLostPointerCapture={up} onPointerLeave={() => setHover(null)} onKeyDown={key}
        {...(can ? { role: 'slider', tabIndex: 0, 'aria-label': estimated ? '音箱估算播放进度' : '播放进度', 'aria-valuemin': 0, 'aria-valuemax': Math.round(total! / 1000), 'aria-valuenow': Math.round(here / 1000), 'aria-valuetext': `${length(here) || '0:00'}，共 ${length(total)}` } : {})}>
        {total ? <>{buffered.filter((range) => range.from < total).map((range) => <span key={range.from} className="swim-buffer" style={{ left: `${range.from / total * 100}%`, width: `${(Math.min(total, range.to) - range.from) / total * 100}%` }} aria-hidden="true" />)}<i /><b><Fish mood={now.phase === 'playing' ? 'idle' : 'waiting'} /></b>{preview != null && <span className="swim-preview" style={{ left: `${Math.max(0, Math.min(total, preview)) / total * 100}%` }}><em className="num">{aim != null && drag === null ? '跳到 ' : ''}{length(preview) || '0:00'}</em></span>}</> : null}
      </div>
      <span className="num">{total ? length(total) : ''}</span>
      {aim != null && <span className="sr-only" role="status">正在定位到 {length(aim) || '0:00'}</span>}
    </div>
  );
}
