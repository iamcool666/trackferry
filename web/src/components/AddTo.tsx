import { useEffect, useState } from 'react';
import { api, ApiError, type Playlist } from '../api';
import { Sheet } from './Sheet';
import { useToast } from './Toast';

/** The playlists of 飞牛音乐, to put a song in. */
export function AddToSheet({ trackID, title, onClose, say }: { trackID: number; title: string; onClose: () => void; say: ReturnType<typeof useToast>[1] }) {
  const [lists, setLists] = useState<Playlist[] | null>(null);
  const [linked, setLinked] = useState(true);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => { api.playlists().then((r) => { setLists(r.playlists); setLinked(r.linked !== false); }, (e) => setError((e as ApiError).message)); }, []);
  const add = async (l: Playlist) => {
    onClose();
    say({ tone: 'busy', text: `正在加入“${l.name}”` });
    try {
      const r = await api.addToPlaylist(l.guid, trackID);
      say({ tone: 'ok', text: r.changed ? `已加入“${l.name}”` : `“${l.name}”里已经有了` });
    } catch (e) {
      say({ tone: 'bad', text: (e as ApiError).message });
    }
  };
  return (
    <Sheet title={`把《${title}》加入`} onClose={onClose}>
      {error && <p className="field-error" role="alert">{error}</p>}
      {!lists && !error && <p className="center"><span className="spinner" /></p>}
      {lists?.length === 0 && <p className="muted">{linked ? '飞牛音乐里还没有歌单，先在飞牛音乐里建一个。' : '歌单来自飞牛音乐；只用音乐文件夹时没有歌单，可以先收藏。'}</p>}
      {lists && lists.length > 0 && <div className="picks">{lists.map((l) => <button key={l.guid} type="button" onClick={() => add(l)}><i><svg viewBox="0 0 20 20" width="20" height="20" aria-hidden="true"><path d="M2 5h11M2 10h11M2 15h7M15.5 11v7M12 14.5h7" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" /></svg></i><span><b>{l.name}</b></span></button>)}</div>}
    </Sheet>
  );
}

/** Puts a song into a playlist of 飞牛音乐. */
export function AddTo({ trackID, title }: { trackID: number; title: string }) {
  const [open, setOpen] = useState(false);
  const [toast, say] = useToast();
  return (
    <>
      <button type="button" className="heart" onClick={() => setOpen(true)} aria-haspopup="dialog" aria-label={`把${title}加入歌单`} title="加入歌单">
        <svg viewBox="0 0 20 20" width="19" height="19" aria-hidden="true"><path d="M2 4.5h13M2 10h9M2 15.5h7M15.5 10.5v8M11.5 14.5h8" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" /></svg>
      </button>
      {open && <AddToSheet trackID={trackID} title={title} onClose={() => setOpen(false)} say={say} />}
      {toast}
    </>
  );
}
