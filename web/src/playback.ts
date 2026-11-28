import { api, type Collection, type HereListed, type OnlineSong, type Single } from './api';
import { HERE } from './ear';
import { commandTV, tvID } from './tv';
import { setTook } from './took';
import { cancelPendingList, listHere, playHere, playListHere, playOnlineListHere, type HereSong } from './here';

type Snapshot = { list: string; tracks: HereListed[] };
export type ListPlayback =
  | { collection: Collection; start?: number; snapshot?: Snapshot }
  | { picks: Snapshot; start?: number };
export type Playback = ListPlayback | { single: Single; song: Omit<HereSong, 'n'> } | { onlineList: { id: string; list: string; tracks: OnlineSong[] } };

let sending: AbortController | undefined;

/** The same selection drives both outputs. A newer play supersedes an unresolved one. */
export async function playOn(output: string, what: Playback, how: 'now' | 'next' | 'last' = 'now'): Promise<'started' | 'queued' | undefined> {
  sending?.abort();
  setTook();
  sending = undefined;
  cancelPendingList();
  if (output === HERE) {
    if ('single' in what) return playHere(what.song, how) ? 'queued' : 'started';
    if ('onlineList' in what) playOnlineListHere(what.onlineList.list, what.onlineList.tracks);
    else if ('picks' in what) playListHere(what.picks.list, what.picks.tracks, what.start, true);
    else if (what.snapshot) playListHere(what.snapshot.list, what.snapshot.tracks, what.start);
    else if (!await listHere(what.collection, what.start)) return;
    return 'started';
  }

  const controller = new AbortController();
  sending = controller;
  try {
    const tv = tvID(output);
    if (tv) {
      if ('onlineList' in what) {
        await commandTV(tv, { action: 'load', items: what.onlineList.tracks.map((song) => ({ online: song.key, title: song.title, artist: song.artist })) }, controller.signal);
      } else if ('single' in what) {
        const item = what.single.source === 'track' ? { id: what.single.track_id } : { online: what.single.id, title: what.song.title, artist: what.song.artist };
        await commandTV(tv, { action: how === 'now' ? 'load' : 'insert', items: [item], queue_mode: how === 'now' ? undefined : how }, controller.signal);
      } else {
        const selection = 'picks' in what ? { source: 'picks', track_ids: what.picks.tracks.map((t) => t.id) } : {
          source: what.collection.source,
          source_id: 'id' in what.collection ? what.collection.id : undefined,
          name: 'name' in what.collection ? what.collection.name : undefined,
        };
        await commandTV(tv, { action: 'load', ...selection, track_id: what.start }, controller.signal);
      }
      return how === 'now' ? 'started' : 'queued';
    }
    if ('single' in what && how !== 'now') {
      const r = await api.queueSong(output, what.single, how, controller.signal);
      if (controller.signal.aborted) return;
      if (r.queued) return 'queued';
    } else {
      const send = 'onlineList' in what ? { source: 'playlist' as const, id: what.onlineList.id }
        : 'single' in what ? what.single
        : { ...('collection' in what ? what.collection : { source: 'picks' as const, track_ids: what.picks.tracks.map((t) => t.id) }), track_id: what.start };
      const r = await api.play(output, send, controller.signal);
      if (controller.signal.aborted) return;
      setTook(r.took);
    }
    return 'started';
  } catch (error) {
    if (!controller.signal.aborted) throw error;
  } finally {
    if (sending === controller) sending = undefined;
  }
}
