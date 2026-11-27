import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { api, ApiError, type Coming, type Device, type PlayMode, type Queue as Q, type TVCommand, type TVOutput, type TVTrack } from '../api';
import { deviceTitle, length } from '../copy';
import { here, hereName, hereSong, useHere } from '../here';
import { onDeck, songOf, useLive } from '../live';
import { Cover } from './Cover';
import { icons, playModes } from './Deck';
import { Sheet } from './Sheet';

interface Row { key: string; title: string; artist: string; cover?: string; duration_ms?: number; automatic?: boolean }

/** Comes near the end of what is shown: more is asked for. */
function End({ onSeen }: { onSeen: () => void }) {
  const box = useRef<HTMLLIElement>(null);
  const f = useRef(onSeen);
  f.current = onSeen;
  useEffect(() => {
    const o = new IntersectionObserver((e) => { if (e.some((x) => x.isIntersecting)) f.current(); }, { rootMargin: '300px' });
    o.observe(box.current!);
    return () => o.disconnect();
  }, []);
  return <li className="q-end" ref={box}><span className="spinner spinner-tiny" /></li>;
}

function Songs({ rows, busy, onPlay, onRemove, order }: { rows: Row[]; busy: string; onPlay: (r: Row) => void; onRemove: (r: Row) => void; order?: (row: Row) => ReactNode }) {
  return <>{rows.map((r) => (
    <li key={r.key} className={busy === r.key ? 'q-busy' : ''}>
      <button type="button" className="q-song" disabled={!!busy} onClick={() => onPlay(r)} aria-label={`现在放${r.title}`}><Cover cover={r.cover} title={r.title} artist={r.artist} /><span><b>{r.title}</b><small>{[r.artist, length(r.duration_ms), r.automatic && '随机接上'].filter(Boolean).join('　')}</small></span></button>
      {order?.(r)}
      <button type="button" className="q-x" disabled={!!busy} onClick={() => onRemove(r)} aria-label={`移出${r.title}`}>{busy === r.key ? <span className="spinner spinner-tiny" /> : <svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true"><path d="M2 2l12 12M14 2L2 14" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" /></svg>}</button>
    </li>
  ))}</>;
}

/** What is on, what comes, and the way out of either. */
function Frame({ where, list, mode, now, onRemoveNow, wishes, songs, total, busy, error, loading, lost, playing, onPlay, onRemove, onMore, onStop, onClose, tools, order }: {
  where: string; list: string; mode: PlayMode | ''; now: Row | null; onRemoveNow?: () => void; wishes: Row[]; songs: Row[]; total: number; busy: string; error: string | null; loading: boolean; lost?: boolean; playing: boolean;
  onPlay: (r: Row) => void; onRemove: (r: Row) => void; onMore?: () => void; onStop: () => void; onClose: () => void;
  tools?: ReactNode; order?: (row: Row) => ReactNode;
}) {
  return (
    <Sheet title="播放列表" onClose={onClose}>
      <div className="q">
        <p className="q-of"><span>{[where, list].filter(Boolean).join('　')}</span><span className="num">{[mode && playModes[mode].name, total ? `${total} 首` : ''].filter(Boolean).join('　')}</span></p>
        {tools}
        {error && <p className="field-error" role="alert">{error}</p>}
        {now && <ol className="q-now"><li className={busy === 'now' ? 'q-busy' : ''}><span className="q-song"><Cover cover={now.cover} title={now.title} artist={now.artist} />{playing && <em className="eq" aria-hidden="true"><u /><u /><u /></em>}<span><b>{now.title}</b><small>{now.artist}</small></span></span>
          {onRemoveNow && <button type="button" className="q-x" disabled={!!busy} onClick={onRemoveNow} aria-label={`移出${now.title}，放下一首`}>{busy === 'now' ? <span className="spinner spinner-tiny" /> : <svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true"><path d="M2 2l12 12M14 2L2 14" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" /></svg>}</button>}</li></ol>}
        {wishes.length > 0 && <><h3>下一首放</h3><ol><Songs rows={wishes} busy={busy} onPlay={onPlay} onRemove={onRemove} /></ol></>}
        {songs.length > 0 && <><h3>接下来</h3><ol><Songs rows={songs} busy={busy} onPlay={onPlay} onRemove={onRemove} order={order} />{onMore && <End onSeen={onMore} />}</ol></>}
        {loading && <p className="center"><span className="spinner" /></p>}
        {!loading && !lost && !now && !wishes.length && !songs.length && <p className="q-none">没有在放的歌</p>}
        {!loading && !lost && now && !wishes.length && !songs.length && <p className="q-none">后面没有歌了</p>}
        {(now || songs.length > 0) && <button type="button" className="q-stop" onClick={() => { onStop(); onClose(); }}>{icons.stop}停止播放</button>}
        {(songs.length > 0 || wishes.length > 0) && <p className="q-tip">移出只影响这次播放，不会动歌单和曲库。</p>}
      </div>
    </Sheet>
  );
}

const keyOf = (s: Coming) => (s.wish ? `w${s.wish}` : `${s.at}/${s.id}`);

export function SpeakerQueue({ device, onStop, onClose }: { device: Device; onStop: () => void; onClose: () => void }) {
  const id = device.device_id;
  const flow = onDeck(useLive().flows, id);
  const song = flow && songOf(flow);
  const [q, setQ] = useState<Q | null>(null);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [lost, setLost] = useState<string | null>(null); // why the list cannot be read
  const turn = useRef(0), more = useRef(false), seen = useRef(0);
  /** All that was shown, as it is now. */
  const load = useCallback(async () => {
    const t = ++turn.current;
    try {
      let all = await api.queue(id);
      while (all.left && all.songs.length < seen.current) {
        const p = await api.queue(id, all.songs.length);
        if (!p.songs.length) break;
        all = { ...p, songs: [...all.songs, ...p.songs] };
      }
      if (t === turn.current) { setQ(all); setLost(null); seen.current = all.songs.length; }
    } catch (e) { if (t === turn.current) setLost((e as ApiError).message); }
  }, [id]);
  useEffect(() => { load(); }, [load, flow?.id, flow?.player?.phase]);
  useEffect(() => () => { turn.current = -1; }, []);
  const onMore = async () => {
    if (!q?.left || more.current) return;
    more.current = true;
    const t = turn.current;
    try {
      const p = await api.queue(id, q.songs.length);
      if (t === turn.current) setQ((x) => { if (!x) return x; const s = [...x.songs, ...p.songs]; seen.current = s.length; return { ...p, songs: s }; });
    } catch { /* asked again when it is next seen */ } finally { more.current = false; }
  };
  const act = async (key: string, f: () => Promise<void>, gone?: (x: Q) => Q) => {
    setBusy(key);
    setError(null);
    try { await f(); if (gone) setQ((x) => x && gone(x)); } catch (e) {
      setError((e as ApiError).code === 'moved' ? '列表刚刚变了，已经刷新' : (e as ApiError).message);
    }
    await load();
    setBusy('');
  };
  const find = (r: Row) => [...q!.wishes, ...q!.songs].find((s) => keyOf(s) === r.key)!;
  const rows = (l?: Coming[]): Row[] => (l ?? []).map((s) => ({ ...s, key: keyOf(s) }));
  return <Frame where={deviceTitle(device)} list={q?.aside ? '' : q?.list ?? ''} mode={q?.mode ?? ''} total={q?.total ?? 0} busy={busy} error={error ?? lost} loading={!q && !lost} lost={!!lost && !q} playing={flow?.player?.phase === 'playing'}
    now={song ? { key: 'now', title: song.title ?? '', artist: song.artist ?? '', cover: song.cover } : null} wishes={rows(q?.wishes)} songs={rows(q?.songs)}
    onRemoveNow={() => act('now', () => api.queueRemove(id, null))} onPlay={(r) => { const s = find(r); act(r.key, () => api.queuePlay(id, s)); }}
    onRemove={(r) => { const s = find(r); act(r.key, () => api.queueRemove(id, s), (x) => ({ ...x, wishes: x.wishes.filter((y) => y !== s), songs: x.songs.filter((y) => y !== s) })); }}
    onMore={q?.left ? onMore : undefined} onStop={onStop} onClose={onClose} />;
}

/** Rows of the list of this device drawn at once. */
const STEP = 60;

export function HereQueue({ onClose }: { onClose: () => void }) {
  const h = useHere();
  const [n, setN] = useState(STEP);
  const [editing, setEditing] = useState(false);
  const on = hereSong(h);
  const pending = h.songs.slice(h.i + 1);
  const rest = h.mode === 'loop' && !editing ? [...pending, ...h.songs.slice(0, h.i)] : pending;
  return <Frame where={hereName} list={h.list} mode={h.mode} total={h.songs.length} busy="" error={null} loading={false} playing={h.on} now={on} wishes={[]} songs={rest.slice(0, n)}
    tools={<div className="q-tools">{pending.length > 1 && <button type="button" aria-pressed={editing} onClick={() => setEditing(!editing)}>{editing ? '完成排序' : '调整顺序'}</button>}{h.songs.length > 1 && <button type="button" onClick={() => { here.clearUpcoming(); setEditing(false); }}>仅保留当前曲目</button>}</div>}
    order={editing ? (row) => {
      const at = pending.findIndex((item) => item.key === row.key);
      return <span className="q-order"><button type="button" disabled={at <= 0} aria-label={`上移${row.title}`} onClick={() => here.moveUpcoming(row.key, -1)}>↑</button><button type="button" disabled={at >= pending.length - 1} aria-label={`下移${row.title}`} onClick={() => here.moveUpcoming(row.key, 1)}>↓</button></span>;
    } : undefined}
    onRemoveNow={() => on && here.remove(on.key)} onPlay={(r) => here.jump(r.key)} onRemove={(r) => here.remove(r.key)} onMore={rest.length > n ? () => setN((x) => x + STEP) : undefined} onStop={here.stop} onClose={onClose} />;
}

export function TVQueue({ device, items, error, busy, send, onClose }: {
  device: TVOutput; items: TVTrack[] | undefined; error: string | null; busy: string;
  send: (command: TVCommand, key?: string) => void; onClose: () => void;
}) {
  const [n, setN] = useState(STEP);
  const p = device.playback;
  const rows = (items ?? []).map((item, index) => ({ ...item, key: String(index) }));
  const pending = rows.slice(p.index + 1);
  const songs = p.mode === 'one' ? pending : [...pending, ...rows.slice(0, p.index)];
  const now = rows[p.index] ?? (p.current.title ? { key: 'now', title: p.current.title, artist: p.current.artist ?? '' } : null);
  return <Frame where={device.name} list="" mode={p.mode} total={p.count} busy={busy} error={error}
    loading={p.count > 0 && !items && !error} lost={!items && !!error} playing={p.phase === 'playing'} now={now} wishes={[]} songs={songs.slice(0, n)}
    onPlay={(row) => send({ action: 'jump', index: Number(row.key) }, row.key)}
    onRemove={(row) => send({ action: 'remove', index: Number(row.key) }, row.key)}
    onMore={songs.length > n ? () => setN((x) => x + STEP) : undefined} onStop={() => send({ action: 'stop' })} onClose={onClose} />;
}
