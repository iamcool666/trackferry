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
  return (
    <Setting id="playing" icon={<NotesIcon />} title="连续播放" says="放完接着放什么，音箱、本机、电视和投放都一样">
      {p && <>
        <div className="kept">
          <div><b>点的歌放完接着随机放</b><small>搜到的一首、在线歌、说“放一首晴天”“来点音乐”、随手翻到的几首：先放点的，再随机接上。关掉就放完即停</small></div>
          <label className="switch"><input type="checkbox" role="switch" checked={p.single} onChange={(e) => set({ single: e.target.checked })} aria-label="点的歌放完接着随机放" /><span /></label>
        </div>
        {p.single && <div className="kept">
          <div><b>随机从哪挑</b><small>选“我的收藏”时只挑收藏过的歌；收藏是空的就从整个曲库挑</small></div>
          <Pick label="随机从哪挑" value={p.pool} onPick={(pool) => set({ pool })} choices={[{ value: 'library' as const, label: '整个曲库' }, { value: 'favorites' as const, label: '我的收藏' }]} />
        </div>}
        <div className="kept"><div><b>不受开关影响的</b><small>点歌手放这位歌手的全部歌，点专辑放整张，收藏和歌单放里面的全部歌，循环放到你换歌或到下面的最长时间为止。换到别处放时带着原来的列表走</small></div></div>
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
