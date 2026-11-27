import { useEffect, useState } from 'react';
import { api, ApiError, type Playing as Prefs } from '../api';
import { Pick } from './Pick';
import { Setting, NotesIcon } from './Setting';

const hours = [1, 2, 3, 4, 6, 8, 12, 24];

/** How songs follow one another. */
export function Playing() {
  const [p, setP] = useState<Prefs | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => { api.playing().then(setP).catch((e) => setError((e as ApiError).message)); }, []);
  const set = async (next: Partial<Prefs>) => {
    const was = p;
    if (p) setP({ ...p, ...next });
    try {
      setP(await api.setPlaying(next));
      setError(null);
    } catch (e) {
      setP(was);
      setError((e as ApiError).message);
    }
  };
  const row = (k: 'artist' | 'after', title: string, says: string) => (
    <div className="kept">
      <div><b>{title}</b><small>{says}</small></div>
      <label className="switch"><input type="checkbox" role="switch" checked={p![k]} onChange={(e) => set({ [k]: e.target.checked })} aria-label={title} /><span /></label>
    </div>
  );
  return (
    <Setting id="playing" icon={<NotesIcon />} title="连续播放" says="一首放完之后接着放什么">
      {p && <>
        {row('artist', '点歌手时一直放', '说“播放周杰伦的歌”，曲库里这位歌手的歌随机一首接一首')}
        {row('after', '点完一首接着随机放', '点的歌放完，从曲库里随机接着放')}
        <div className="kept">
          <div><b>最长放多久</b><small>歌手、收藏、歌单和随机连播都按这个时间结束，正在放的那首会放完</small></div>
          <Pick label="最长放多久" value={p.hours} onPick={(h) => set({ hours: h })}
            choices={[...new Set([...hours, ...(p.hours ? [p.hours] : [])])].sort((a, b) => a - b).map((h) => ({ value: h, label: `${h} 小时` })).concat({ value: 0, label: '不限' })} />
        </div>
      </>}
      {error && <p className="field-error">{error}</p>}
    </Setting>
  );
}
