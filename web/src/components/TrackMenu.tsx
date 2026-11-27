import { useState } from 'react';
import type { Device, Track } from '../api';
import { deviceTitle, length, singer } from '../copy';
import { connected } from '../ear';
import { useTarget } from '../outputs';
import { hereName, trackHere } from '../here';
import type { Place } from '../nav';
import { playOn, type Playback } from '../playback';
import { AddToSheet } from './AddTo';
import { Sheet } from './Sheet';
import { SpeakerChip } from './SpeakerChip';
import { activeTV, useTVOutputs } from '../tv';
import { useToast } from './Toast';
import { tookNote } from '../took';

const icon = (d: string) => <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d={d} /></svg>;

/** What else can be done with a song of the library. */
export function TrackMenu({ track, from, devices, navigate }: { track: Track; from: Playback; devices: Device[]; navigate: (p: Place) => void }) {
  const [open, setOpen] = useState(false);
  const [adding, setAdding] = useState(false);
  const [target, setTarget] = useTarget(devices);
  const [toast, say] = useToast();
  const list = connected(devices);
  const tvs = useTVOutputs();
  const tv = activeTV(target);
  const send = async (how: 'now' | 'next' | 'last') => {
    const output = tv?.name ?? (d ? deviceTitle(d) : hereName);
    const what: Playback = how === 'now' ? from
      : { single: { source: 'track', track_id: track.id }, song: trackHere(track) };
    setOpen(false);
    say(null);
    try {
      const result = await playOn(target, what, how);
      if (result === 'queued') say({ tone: 'ok', text: `《${track.title}》已排到${output}${how === 'next' ? '的下一首' : '的队尾'}` });
      else if (result === 'started' && (d || tv)) say({ tone: 'ok', text: `已送往${output}${tookNote()}` });
    } catch (e) { say({ tone: 'bad', text: (e as Error).message }); }
  };
  const d = list.find((x) => x.device_id === target);
  return (
    <>
      <button type="button" className="track-more" onClick={() => setOpen(true)} aria-haspopup="dialog" aria-label={`${track.title}，更多操作`}><svg viewBox="0 0 20 20" width="20" height="20" fill="currentColor" aria-hidden="true"><circle cx="4" cy="10" r="1.7" /><circle cx="10" cy="10" r="1.7" /><circle cx="16" cy="10" r="1.7" /></svg></button>
      {open && (
        <Sheet title={track.title} className="track-menu-sheet" onClose={() => setOpen(false)}>
          <p className="sheet-by">{[singer(track.artist), length(track.duration_ms)].filter(Boolean).join('　')}</p>
          {(list.length > 0 || tvs.some((item) => item.online)) && <div className="track-menu-output"><SpeakerChip devices={devices} value={target} onChange={setTarget} title="播放到" showTitle televisions inline /></div>}
          <div className="send">
            <button type="button" onClick={() => send('now')}><svg viewBox="0 0 24 24" width="22" height="22" fill="currentColor" aria-hidden="true"><path d="M8 5.2v13.6L19 12z" /></svg>从这首开始</button>
            <button type="button" onClick={() => send('next')}>{icon('M4 7h10M4 12h7M4 17h7M15 11.5v7l5.5-3.5z')}下一首放</button>
          </div>
          <div className="picks plain">
            <button type="button" onClick={() => send('last')}><i>{icon('M4 6h16M4 11h16M4 16h6M17 14v7M13.5 17.5h7')}</i><span><b>放到队尾</b></span></button>
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
