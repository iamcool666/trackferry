import { useState } from 'react';
import { api, ApiError, type Device, type Track } from '../api';
import { deviceTitle, length, singer } from '../copy';
import { connected, HERE, lookAt, useTarget } from '../ear';
import { hereName, playHere, trackHere } from '../here';
import type { Place } from '../nav';
import { AddToSheet } from './AddTo';
import { Sheet } from './Sheet';
import { useToast } from './Toast';

const icon = (d: string) => <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d={d} /></svg>;

/** What else can be done with a song of the library. */
export function TrackMenu({ track, from, devices, navigate }: { track: Track; from?: () => { list: string; songs: ReturnType<typeof trackHere>[] }; devices: Device[]; navigate: (p: Place) => void }) {
  const [open, setOpen] = useState(false);
  const [adding, setAdding] = useState(false);
  const [target, setTarget] = useTarget(devices);
  const [toast, say] = useToast();
  const list = connected(devices);
  const send = async (d: Device | undefined, next: boolean) => {
    setOpen(false);
    if (target === HERE) { playHere(trackHere(track), next ? 'next' : 'now', from?.()); say({ tone: 'ok', text: next ? `《${track.title}》在${hereName}排到下一首` : `在${hereName}播放` }); return; }
    if (!d) { say({ tone: 'bad', text: '没有接入的音箱，先到音箱页接入一台' }); return; }
    say({ tone: 'busy', text: `正在送到${deviceTitle(d)}` });
    try {
      const r = next ? await api.playNext(d.device_id, { source: 'track', track_id: track.id, title: track.title }) : (await api.playTrack(d.device_id, track.id), {} as { queued?: boolean });
      lookAt(d.device_id);
      say({ tone: 'ok', text: r.queued ? `《${track.title}》在${deviceTitle(d)}排到下一首` : `已送往${deviceTitle(d)}` });
    } catch (e) { say({ tone: 'bad', text: (e as ApiError).message }); }
  };
  const d = list.find((x) => x.device_id === target) ?? list[0];
  return (
    <>
      <button type="button" className="track-more" onClick={() => setOpen(true)} aria-haspopup="dialog" aria-label={`${track.title}，更多操作`}><svg viewBox="0 0 20 20" width="20" height="20" fill="currentColor" aria-hidden="true"><circle cx="4" cy="10" r="1.7" /><circle cx="10" cy="10" r="1.7" /><circle cx="16" cy="10" r="1.7" /></svg></button>
      {open && (
        <Sheet title={track.title} onClose={() => setOpen(false)}>
          <p className="sheet-by">{[singer(track.artist), length(track.duration_ms)].filter(Boolean).join('　')}</p>
          {list.length > 0 && <div className="send-to" role="radiogroup" aria-label="放在哪里"><button type="button" role="radio" aria-checked={target === HERE} onClick={() => setTarget(HERE)}>{hereName}</button>{list.map((x) => <button key={x.device_id} type="button" role="radio" aria-checked={target !== HERE && x === d} onClick={() => setTarget(x.device_id)}>{deviceTitle(x)}</button>)}</div>}
          <div className="send">
            <button type="button" onClick={() => send(d, false)}><svg viewBox="0 0 24 24" width="22" height="22" fill="currentColor" aria-hidden="true"><path d="M8 5.2v13.6L19 12z" /></svg>现在放</button>
            <button type="button" onClick={() => send(d, true)}>{icon('M4 7h10M4 12h7M4 17h7M15 11.5v7l5.5-3.5z')}下一首放</button>
          </div>
          <div className="picks plain">
            <button type="button" onClick={() => { setOpen(false); setAdding(true); }}><i>{icon('M4 7h14M4 12h9M4 17h8M18 12v8M14 16h8')}</i><span><b>加入歌单</b></span></button>
            {track.album_id && <button type="button" onClick={() => { setOpen(false); navigate({ page: 'library', kind: 'album', id: track.album_id! }); }}><i>{icon('M12 3.5a8.5 8.5 0 1 1 0 17 8.5 8.5 0 0 1 0-17zM12 9.6a2.4 2.4 0 1 1 0 4.8 2.4 2.4 0 0 1 0-4.8z')}</i><span><b>看专辑</b><small>{track.album}</small></span></button>}
            {singer(track.artist) && <button type="button" onClick={() => { setOpen(false); navigate({ page: 'library', kind: 'artist', id: track.artist }); }}><i>{icon('M12 4a4 4 0 1 1 0 8 4 4 0 0 1 0-8zM4.5 20c1-4.4 4-6 7.5-6s6.5 1.6 7.5 6')}</i><span><b>看歌手</b><small>{track.artist}</small></span></button>}
          </div>
        </Sheet>
      )}
      {adding && <AddToSheet trackID={track.id} title={track.title} onClose={() => setAdding(false)} say={say} />}
      {toast}
    </>
  );
}
