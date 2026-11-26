import { api, type PlayMode, type TVRef, type TVTrack } from './api';
import { HERE } from './ear';
import { correctHere, hereSong, hereNow, hereTransfer, receiveHere } from './here';
import { onSpeaker, type Flow } from './live';
import { commandTV, tvID, tvPosition } from './tv';

export function checkedSpeaker(flows: Flow[], id: string) {
  const f = onSpeaker(flows, id);
  return f?.run?.song?.track_id && (f.player?.phase === 'paused' || f.player?.phase === 'playing' && f.player.ours) ? f : null;
}

export async function sendToSpeaker(speaker: string, items: TVRef[], index: number, at: number, _mode: PlayMode, signal?: AbortSignal) {
  if (!items[index]?.id || items.some((item) => !item.id)) throw new Error('小爱接力目前只支持曲库歌曲');
  await api.speakerReceive(speaker, items.map((item) => item.id!), index, Math.round(at), signal);
}

/** What one output plays, ready to be moved; release pauses it once the target plays. */
export interface Taken { source: string; refs: TVRef[]; tracks?: TVTrack[]; index: number; at: number; mode: PlayMode; playing: boolean; read: number; release: (signal?: AbortSignal) => Promise<void> }

const refOf = (t: TVTrack): TVRef => (t.online ? { online: t.online, title: t.title, artist: t.artist } : { id: t.id });

/** Reads what source plays: HERE, a speaker, or a television (tv:…). */
export async function take(source: string, signal?: AbortSignal): Promise<Taken> {
  const read = Date.now();
  if (source === HERE) {
    const t = hereTransfer();
    if (!t) throw new Error('等本机开始播放后再切换');
    return { source, refs: t.items, index: t.index, at: t.position_ms, mode: t.mode, playing: true, read, release: async () => { if (!t.pauseIfCurrent()) throw new Error('本机已经换了歌，两边可能都在放'); } };
  }
  const tv = tvID(source);
  if (tv) {
    const { playback: p, items } = await api.tvSnapshot(tv, signal);
    if (!items[p.index] || p.phase === 'buffering') throw new Error('电视还没有可接续的播放');
    return { source, refs: items.map(refOf), tracks: items, index: p.index, at: tvPosition(p), mode: p.mode, playing: p.phase === 'playing', read, release: (s) => commandTV(tv, { action: 'pause', revision: p.revision, entry: p.entry }, s) };
  }
  const s = await api.speakerHandoff(source, signal);
  return { source, refs: s.items, tracks: s.tracks, index: s.index, at: s.position_ms, mode: s.mode, playing: true, read, release: (sig) => api.speakerHandoffPause(source, s.run_id, sig) };
}

const sameSong = (a?: TVRef, b?: TVRef) => !!a && !!b && (a.id ? a.id === b.id : !!a.online && a.online === b.online);

/** Only songs of the library can go to a speaker. */
export const speakerSafe = (t: Taken) => t.refs.every((r) => !!r.id);

/**
 * Moves what was taken to target; the source pauses only once the target plays.
 * To HERE it must be called inside the tap that asked for it, so taken has to be read before.
 */
export async function give(taken: Taken, target: string, signal: AbortSignal) {
  const { refs, tracks, index, mode } = taken;
  const at = Math.max(0, taken.at + (taken.playing ? Date.now() - taken.read : 0));
  if (target === HERE) {
    if (!tracks) throw new Error('本机已经在放了');
    await receiveHere(tracks, index, at, mode, signal, () => taken.release(signal));
    const released = Date.now(), key = hereSong(hereNow())?.key;
    // Self-healing: the source now stands where it stopped; this device follows if it is far off.
    if (key) await take(taken.source, signal).then((again) => {
      if (again.playing || again.index !== index || !sameSong(again.refs[index], refs[index])) return;
      correctHere(index, again.at + (Date.now() - released), key);
    }, () => {});
    return;
  }
  const tv = tvID(target);
  if (tv) await commandTV(tv, { action: 'load', items: refs, index, position_ms: Math.round(at), mode }, signal);
  else await sendToSpeaker(target, refs, index, at, mode, signal);
  if (!signal.aborted) await taken.release(signal);
}
