import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { api, ApiError, type Command, type Device, type TakeoverRun } from '../api';
import { SourceIcon } from '../components/SourceIcon';
import { Cover } from '../components/Cover';
import { Ears } from '../components/Ears';
import { Notice } from '../components/Notice';
import { routeHere } from '../here';
import { Play, PlaybackTarget } from '../components/Play';
import { River } from '../components/River';
import { Basis, RouteDetail } from '../components/RouteLine';
import { RunSteps } from '../components/Run';
import { watchText } from '../components/Speaker';
import { clock, secs, singer } from '../copy';
import { HERE } from '../ear';
import { useEar } from '../outputs';
import { serverNow, useLive } from '../live';
import { stepLabel, stopLabel } from '../route';
import { fishPicture, songPicture, useBackdrop } from '../stage/Stage';
import { after, fromCommand, fromFlow, fromPush, phase, type Story } from '../story';

/** Where time went, and where to look. */
function Scrub({ story, t, now, playing, live, onSeek, onToggle }: { story: Story; t: number; now: () => number; playing: boolean; live: boolean; onSeek: (t: number) => void; onToggle: () => void }) {
  const bar = useRef<HTMLDivElement>(null);
  const head = useRef<HTMLElement>(null);
  const span = Math.max(1, story.end - story.start);
  // The playhead glides; React need not know.
  useEffect(() => {
    let raf = 0;
    const f = () => {
      if (head.current) head.current.style.transform = `translateX(${Math.max(0, Math.min(1, (now() - story.start) / span)) * bar.current!.clientWidth}px)`;
      raf = playing ? requestAnimationFrame(f) : 0;
    };
    f();
    const o = new ResizeObserver(() => { if (!raf) f(); });
    o.observe(bar.current!);
    return () => { cancelAnimationFrame(raf); o.disconnect(); };
  }, [now, playing, span, story.start]);
  const x = (v: number) => `${Math.max(0, Math.min(100, (v - story.start) / span * 100))}%`;
  const runAt = story.run?.started_ms ?? null;
  const parts = [
    story.seen > story.start ? { from: story.start, to: story.seen, label: '听到', c: 'a' } : null,
    story.decided !== null ? { from: story.seen, to: story.decided, label: '查找', c: 'b' } : null,
    runAt !== null ? { from: runAt, to: story.landed ?? story.end - 900, label: '接管', c: 'c' } : null,
    story.landed ? { from: story.landed, to: story.end, label: '出声', c: 'd' } : null,
  ].filter((p) => p && p.to > p.from) as { from: number; to: number; label: string; c: string }[];
  const seek = (e: React.PointerEvent) => {
    const b = bar.current!.getBoundingClientRect();
    onSeek(story.start + Math.max(0, Math.min(1, (e.clientX - b.left) / b.width)) * span);
  };
  return (
    <div className="scrub">
      <button className="scrub-btn" onClick={onToggle} aria-label={playing ? '暂停' : '重放'}>{playing ? <svg viewBox="0 0 24 24" width="20" height="20"><path d="M8 5v14M16 5v14" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" /></svg> : <svg viewBox="0 0 24 24" width="20" height="20"><path d="M8 5.2v13.6L19 12z" fill="currentColor" /></svg>}</button>
      <div className="scrub-bar" ref={bar} role="slider" tabIndex={0} aria-label="经过的时间" aria-valuemin={0} aria-valuemax={Math.round(span)} aria-valuenow={Math.round(t - story.start)} aria-valuetext={`${secs(t - story.start)} 秒`}
        onPointerDown={(e) => { e.currentTarget.setPointerCapture(e.pointerId); seek(e); }} onPointerMove={(e) => { if (e.buttons) seek(e); }}
        onKeyDown={(e) => { if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') { e.preventDefault(); onSeek(t + (e.key === 'ArrowLeft' ? -200 : 200)); } }}>
        {parts.map((p) => <span key={p.c} className={`seg-${p.c}`} style={{ left: x(p.from), width: `calc(${x(p.to)} - ${x(p.from)} - 2px)` }}>{(p.to - p.from) / span > .07 && <small>{p.label}<b className="num">{secs(p.to - p.from)}</b></small>}</span>)}
        {(story.run?.steps ?? []).filter((s) => !after.has(s.step) && story.run!.started_ms + s.at_ms <= story.end).map((s, i) => <i key={i} style={{ left: x(story.run!.started_ms + s.at_ms) }} aria-label={stepLabel(s.step)} />)}
        <u ref={head} />
      </div>
      <span className="scrub-t num">{live ? <em>实时</em> : `${secs(Math.max(0, Math.min(t, story.end) - story.start))} 秒`}</span>
    </div>
  );
}

export function FlowPage({ id, devices }: { id?: string; devices: Device[] }) {
  const live = useLive();
  const [at, setEar] = useEar(devices);
  const ear = at === HERE ? '' : at; // nothing of this device is followed here
  const [cmds, setCmds] = useState<Command[] | null>(null);
  const [runs, setRuns] = useState<TakeoverRun[]>([]);
  const [picked, setPicked] = useState<string | null>(id ? (id[0] === 'r' ? id : `c${id}`) : null);
  const [error, setError] = useState<string | null>(null);
  const [text, setText] = useState('');
  const [trying, setTrying] = useState(false);
  // Replaying: position, and when it was there. null: follow the real clock.
  const [head, setHead] = useState<{ at: number; since: number | null } | null>(null);
  const [, tick] = useState(0);

  const load = useCallback(async () => {
    try {
      const [c, r] = await Promise.all([api.commands(0, 50), api.takeoverRuns(120)]);
      setCmds(c.commands);
      setRuns(r.runs ?? []);
    } catch (e) { setError((e as ApiError).message); }
  }, []);
  const latest = live.flows.at(-1);
  useEffect(() => { load(); }, [load, latest?.id, latest?.done]);
  const newest = live.flows.filter((f) => !ear || f.kind === 'try' || f.device_id === ear).at(-1);

  // A new request takes the stage, if its speaker is the one being looked at.
  const seen = useRef<number | null>(null);
  const started = useRef('');
  useEffect(() => {
    if (!live.on) return;
    if (seen.current !== null && newest && newest.id > seen.current) { setPicked(null); setHead(null); }
    seen.current = newest?.id ?? 0;
  }, [live.on, newest?.id]);
  const look = (d: string) => { setEar(d); setPicked(null); setHead(null); started.current = ''; seen.current = null; };

  const all = useMemo(() => {
    const flows = live.flows.map(fromFlow).reverse();
    const have = new Set(flows.map((s) => s.key));
    const old = (cmds ?? []).map((c) => fromCommand(c, runs.find((r) => r.kind === 'command' && r.command_id === c.id))).filter((s) => !have.has(s.key));
    const sent = runs.filter((r) => r.kind === 'push' && !have.has(`r${r.id}`)).map(fromPush);
    return [...flows, ...old, ...sent].sort((a, b) => b.seen - a.seen);
  }, [live.flows, cmds, runs]);
  const stories = useMemo(() => all.filter((s) => !ear || s.kind === 'try' || s.deviceId === ear).slice(0, 24), [all, ear]);
  // Without a choice: the song that is on, else the newest.
  const story = all.find((s) => s.key === picked) ?? (picked ? null : stories.find((s) => ear && s.open && s.player?.phase) ?? stories[0]) ?? null;
  const isLive = !!story && !head && (story.open || serverNow() < story.end);

  // Coming here, or choosing one, plays it once.
  useEffect(() => {
    if (!story || started.current === story.key) return;
    started.current = story.key;
    if (story.open || serverNow() - story.seen < 4000) { setHead(null); return; }
    const still = matchMedia('(prefers-reduced-motion: reduce)').matches;
    setHead(still ? { at: story.end, since: null } : { at: story.start, since: performance.now() });
  }, [story?.key]); // eslint-disable-line react-hooks/exhaustive-deps

  const now = useCallback(() => {
    if (!story) return 0;
    if (!head) return serverNow();
    return Math.min(story.end, head.since === null ? head.at : head.at + performance.now() - head.since);
  }, [story, head]);
  const t = now();
  // Once the song is out only its clock moves.
  const settled = isLive && !!story?.landed && t > story.landed + 1500;
  const moving = !!story && (isLive || (!!head && head.since !== null && t < story.end));
  useEffect(() => {
    if (!moving) return;
    const i = setInterval(() => tick((n) => n + 1), settled ? 500 : 100);
    return () => clearInterval(i);
  }, [moving, settled]);

  const r = story?.route && story.decided !== null && t >= story.decided ? story.route : null;
  const song = r && r.decision !== 'native' ? r : null;
  useBackdrop(song ? songPicture({ title: song.title, artist: song.artist, cover: song.cover }, 600) : fishPicture);

  const pulses = Object.values(live.pulses);
  const ears = pulses.filter((p) => p.state === 'listening' && (!ear || p.device_id === ear));
  const command = cmds?.find((c) => c.id === story?.commandId);
  const run = story?.run;

  const tryIt = async (e: FormEvent) => {
    e.preventDefault();
    if (!text.trim()) return;
    setTrying(true);
    setError(null);
    try { await api.tryRoute(text.trim()); } catch (err) { setError((err as ApiError).message); } finally { setTrying(false); }
  };
  return (
    <div className="flow">
      <section className="flow-scene">
        <Ears devices={devices} value={ear} onChange={look} />
        <header className="flow-head">
          <h1>{story ? (story.kind === 'test' || story.kind === 'push' ? story.said : `“${story.said}”`) : '等一句话'}</h1>
          <p aria-live="polite">{story ? <><b>{phase(story, t)}</b>{song && story.kind !== 'test' && story.kind !== 'push' && <span>{[song.title, singer(song.artist)].filter(Boolean).join('　')}</span>}{song?.others?.length ? <span className="muted">也有{song.others.join('、')}的版本，说“放某某的”就能换</span> : null}</> : ears.length ? `${ears.map((p) => p.name).join('、')}在听` : '没有音箱在听'}</p>
        </header>
        <River story={story} now={now} listening={ears.map((p) => p.name)} />
        <div className="flow-foot">
          {story && <Scrub story={story} t={t} now={now} live={isLive} playing={moving}
            onSeek={(v) => setHead({ at: Math.max(story.start, Math.min(story.end, v)), since: null })}
            onToggle={() => setHead(moving ? { at: t, since: null } : { at: t >= story.end - 5 ? story.start : t, since: performance.now() })} />}
          <div className="flow-bar">
            <ul className="flow-list">
              {stories.map((s) => {
                const nat = !s.route || s.route.decision === 'native';
                return <li key={s.key}><button type="button" aria-current={s.key === story?.key || undefined} onClick={() => { started.current = ''; setPicked(s.key); }}>{nat ? <span className="cover nat-cover"><SourceIcon name="xiaomi" /></span> : <Cover cover={s.route!.cover} title={s.route!.title ?? ''} artist={s.route!.artist} />}<span><b>{s.said}</b><small>{s.kind === 'try' ? '试一句' : s.kind === 'test' ? '试音' : s.kind === 'push' ? s.route?.list ? `连播${s.route.list} ${s.device}` : `曲库推到${s.device}` : s.device} <span className="num">{clock(s.seen).slice(0, 5)}</span></small></span></button></li>;
              })}
            </ul>
            <form className="ask" onSubmit={tryIt}>
              <input value={text} onChange={(e) => setText(e.target.value)} placeholder="试一句，看它怎么走" aria-label="试一句点歌" maxLength={100} />
              <button disabled={trying || !text.trim()} aria-label="看它怎么走">{trying ? <span className="spinner spinner-tiny" /> : <svg viewBox="0 0 18 14" width="18" height="14" aria-hidden="true"><path d="M1 7h15M10 1l6 6-6 6" fill="none" stroke="currentColor" strokeWidth="2" /></svg>}</button>
            </form>
          </div>
        </div>
      </section>

      <section className="flow-more">
        {error && <Notice tone="error" title={error} />}
        {!live.on && cmds && <p className="muted">实时连接断开，正在重连</p>}
        <div className="flow-cols">
          {story?.route && story.kind !== 'test' && story.kind !== 'push' && <div>{story.route.basis?.length ? <><h2>为什么这样</h2><Basis route={story.route} /></> : null}{story.route.layers.some((l) => l.name !== 'xiaomi') && <><h2>问过谁</h2><RouteDetail route={story.route} /></>}{story.route.track_id && story.route.decision !== 'native' ? <div className="flow-play"><Play key={story.route.track_id} what={{ single: { source: 'track', track_id: story.route.track_id }, song: routeHere(story.route) }} title={story.route.title ?? ''} devices={devices} /><PlaybackTarget devices={devices} /></div> : null}</div>}
          <div>
            {run && <><h2>接管</h2><RunSteps run={run} /></>}
            {command && <><h2>小爱</h2><p>{command.answer || '没有语音回答'}{command.answer_types.includes('AUDIO') && '（播放了音频）'}</p></>}
          </div>
          {pulses.length > 0 && <div><h2>在听</h2><ul className="pulses">{pulses.map((p) => <li key={p.device_id}><b className={p.state === 'listening' ? 'on-dot' : 'off-dot'}>{p.name}</b><span>{watchText[p.state]}</span><span>{p.every_ms ? <>每 <b className="num">{p.every_ms / 1000}</b> 秒看一次，</> : null}上次用了 <span className="num">{p.ms} ms</span></span><span>点歌 <b className="num">{p.recorded}</b>，其他 <b className="num">{p.ignored}</b></span></li>)}</ul></div>}
        </div>
        {song && <p className="sr-only">{stopLabel[song.source ?? 'library']}</p>}
      </section>
    </div>
  );
}
