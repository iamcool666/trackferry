import { useEffect, useState } from 'react';
import { api, ApiError, type SongTech, type SongVersion } from '../api';
import { length, singer } from '../copy';
import { stopLabel } from '../route';
import { Sheet } from './Sheet';
import { SourceIcon } from './SourceIcon';

export interface QualityRef { track?: number; online?: string; title: string; artist: string }

const tierName: Record<SongTech['tier'], string> = { hires: '高解析', lossless: '无损', high: '高品质', standard: '标准', '': '' };

/** The quality the song really plays at; a tap lists the other copies there are and plays one from here. */
export function Quality({ song, onPick }: { song: QualityRef; onPick?: (v: SongVersion) => Promise<void> | void }) {
  const [tech, setTech] = useState<SongTech | null>(null);
  const [open, setOpen] = useState(false);
  const key = song.track ? `t${song.track}` : song.online ?? '';
  useEffect(() => {
    setTech(null);
    if (!key) return;
    const ctl = new AbortController();
    let tries = 0, t: ReturnType<typeof setTimeout> | undefined;
    const read = () => api.tech({ track: song.track, online: song.online }, ctl.signal).then((x) => {
      setTech(x);
      // An online song's bit rate is known once its stream has been fetched.
      if (!x.known && song.online && ++tries < 4) t = setTimeout(read, 4000);
    }, () => {});
    read();
    return () => { ctl.abort(); clearTimeout(t); };
  }, [key]); // eslint-disable-line react-hooks/exhaustive-deps
  if (!key || !tech) return null;
  return (
    <>
      <button type="button" className={`quality q-${tech.tier || 'plain'}`} onClick={() => setOpen(true)} aria-haspopup="dialog" title={tech.known ? '换一个版本' : '播放后才读得到码率；点开换一个版本'}>
        {tech.tier === 'hires' || tech.tier === 'lossless' ? <b>{tierName[tech.tier]}</b> : null}{tech.label}
      </button>
      {open && <Versions song={song} current={key} onPick={onPick} onClose={() => setOpen(false)} />}
    </>
  );
}

function Versions({ song, current, onPick, onClose }: { song: QualityRef; current: string; onPick?: (v: SongVersion) => Promise<void> | void; onClose: () => void }) {
  const [list, setList] = useState<SongVersion[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [fresh, setFresh] = useState(0);
  useEffect(() => {
    const ctl = new AbortController();
    setList(null);
    api.versions(song.title, song.artist, fresh > 0, ctl.signal).then((x) => { setList(x.versions); setError(null); }, (e) => { if (!ctl.signal.aborted) setError((e as ApiError).message); });
    return () => ctl.abort();
  }, [song.title, song.artist, fresh]);
  const keyOf = (v: SongVersion) => v.track_id ? `t${v.track_id}` : v.online ?? '';
  const pick = async (v: SongVersion) => {
    if (!onPick) return;
    setBusy(keyOf(v));
    try { await onPick(v); onClose(); } catch (e) { setError((e as Error).message); } finally { setBusy(null); }
  };
  return (
    <Sheet title="换个版本" onClose={onClose} className="versions">
      <p className="hint">每个版本都实际取过一次，标的是真实格式和码率。{onPick ? '点一下从当前进度接着放，后面排着的歌不变。' : '等这首开始播放后才能换。'}</p>
      {!list && !error && <p className="hint"><span className="spinner spinner-tiny" />正在问曲库和各个平台</p>}
      {error && <p className="field-error" role="alert">{error}</p>}
      {list && list.length === 0 && <p className="hint">曲库和平台都没有同名同歌手的版本</p>}
      {list && list.length > 0 && (
        <ul className="version-list">
          {list.map((v) => {
            const k = keyOf(v), on = k === current;
            return (
              <li key={k || v.source}>
                <button type="button" disabled={on || !!v.error || !onPick || busy !== null} aria-current={on || undefined} onClick={() => pick(v)}>
                  <SourceIcon name={v.source} />
                  <span className="v-main"><b>{stopLabel[v.source]}{v.album ? `　${v.album}` : ''}</b><small>{[singer(v.artist), v.duration_ms ? length(v.duration_ms) : ''].filter(Boolean).join('　')}{v.error ? `　${v.error}` : ''}</small></span>
                  <span className={`quality q-${v.tech.tier || 'plain'}`}>{v.error ? '放不了' : v.tech.label || '—'}</span>
                  {on ? <i className="v-now">在放</i> : busy === k ? <span className="spinner spinner-tiny" /> : null}
                </button>
              </li>
            );
          })}
        </ul>
      )}
      {list && <button type="button" className="link" onClick={() => setFresh((n) => n + 1)}>重新查一遍</button>}
    </Sheet>
  );
}
