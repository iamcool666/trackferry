import { useEffect, useRef, useState, type ReactNode } from 'react';
import type { PlayMode } from '../api';

const draw = (d: string, fill = true) => <svg viewBox="0 0 24 24" width="22" height="22" fill={fill ? 'currentColor' : 'none'} stroke={fill ? 'none' : 'currentColor'} strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d={d} /></svg>;
export const icons = {
  play: draw('M8 5.2v13.6L19 12z'), pause: draw('M7 5h3.6v14H7zM13.4 5H17v14h-3.6z'), stop: draw('M6.5 6.5h11v11h-11z'),
  previous: draw('M6.2 5.5h2.2v13H6.2zM18.6 5.8v12.4L9.4 12z'), next: draw('M15.6 5.5h2.2v13h-2.2zM5.4 5.8v12.4L14.6 12z'),
  wake: draw('M12 4a2.6 2.6 0 0 1 2.6 2.6v4.8a2.6 2.6 0 0 1-5.2 0V6.6A2.6 2.6 0 0 1 12 4zM6 11.5a6 6 0 0 0 12 0M12 17.5V21', false),
  volume: draw('M4 9.5v5h3.5L12 18.5v-13L7.5 9.5zM15.5 9a4.2 4.2 0 0 1 0 6M18 6.5a7.8 7.8 0 0 1 0 11', false),
  mute: draw('M4 9.5v5h3.5L12 18.5v-13L7.5 9.5zM16 9.5l5 5M21 9.5l-5 5', false),
  list: draw('M4 6.5h16M4 12h10M4 17.5h10M17.5 11.5v6.2M17.5 11.5l3 1.4M17.5 17.8a1.7 1.7 0 1 1-3.4 0 1.7 1.7 0 0 1 3.4 0', false),
};
export const playModes: Record<PlayMode, { name: string; icon: ReactNode; next: PlayMode }> = {
  shuffle: { name: '随机播放', next: 'loop', icon: draw('M4 7h2.6c1.5 0 2.6.8 3.5 2.2l3.8 5.6c.9 1.4 2 2.2 3.5 2.2H20M4 17h2.6c1.5 0 2.6-.8 3.5-2.2M13.9 9.2C14.8 7.8 15.9 7 17.4 7H20M17.5 4.5 20 7l-2.5 2.5M17.5 14.5 20 17l-2.5 2.5', false) },
  loop: { name: '顺序循环', next: 'one', icon: draw('M4.5 11.5V10A3.5 3.5 0 0 1 8 6.5h11.5M17 4l2.5 2.5L17 9M19.5 12.5V14a3.5 3.5 0 0 1-3.5 3.5H4.5M7 15l-2.5 2.5L7 20', false) },
  one: { name: '单曲循环', next: 'shuffle', icon: draw('M4.5 11.5V10A3.5 3.5 0 0 1 8 6.5h11.5M17 4l2.5 2.5L17 9M19.5 12.5V14a3.5 3.5 0 0 1-3.5 3.5H4.5M7 15l-2.5 2.5L7 20M11 10.6l1.4-.9v4.8', false) },
};

export interface Vol { value: number | null; /** While it is dragged. */ live?: (v: number) => void; set: (v: number) => void }

/** The same slider can stay visible on a settings page or close on the player. */
function Volume({ vol, name, onClose }: { vol: Vol; name: string; onClose?: () => void }) {
  const [drag, setDrag] = useState<number | null>(null);
  const box = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLInputElement>(null);
  const v = drag ?? vol.value ?? 0;
  const transient = !!onClose;
  const close = useRef(onClose);
  close.current = onClose;
  useEffect(() => { if (transient) input.current?.focus({ preventScroll: true }); }, [transient]);
  useEffect(() => {
    if (!transient) return;
    const t = setTimeout(() => close.current?.(), 5000);
    return () => clearTimeout(t);
  }, [v, transient]);
  useEffect(() => {
    if (!transient) return;
    const off = (e: Event) => { if (e instanceof KeyboardEvent ? e.key === 'Escape' : !box.current?.contains(e.target as Node)) close.current?.(); };
    addEventListener('pointerdown', off);
    addEventListener('keydown', off);
    return () => { removeEventListener('pointerdown', off); removeEventListener('keydown', off); };
  }, [transient]);
  useEffect(() => { if (drag !== null && vol.value === drag) setDrag(null); }, [vol.value, drag]);
  const commit = () => { if (drag !== null && drag !== vol.value) vol.set(drag); };
  return (
    <div className={`ctl-vol${transient ? '' : ' ctl-vol-inline'}`} ref={box}>
      {onClose ? <button type="button" className="foot-btn" onClick={onClose} aria-label="收起音量">{v ? icons.volume : icons.mute}</button> : <span aria-hidden="true">{icons.volume}</span>}
      <input ref={input} type="range" min={0} max={100} value={v} disabled={vol.value === null} style={{ '--v': `${v}%` } as React.CSSProperties} aria-label={`${name}音量`} onChange={(e) => { const n = Number(e.target.value); setDrag(n); vol.live?.(n); }} onPointerUp={commit} onKeyUp={commit} onBlur={commit} />
      <small className="num">{vol.value === null ? '–' : v}</small>
    </div>
  );
}

export interface Hands {
  name: string; state: 'playing' | 'paused' | 'idle'; waiting?: boolean; busy?: string | null; off?: boolean; alone?: boolean;
  mode: PlayMode | ''; onMode: (m: PlayMode) => void; onToggle: () => void; onPrevious: () => void; onNext: () => void;
  onList: () => void; count: number; vol: Vol | null; inlineVolume?: boolean; onWake?: () => void;
  /** The left of the second line, and more signs at its right. */
  foot?: ReactNode; tail?: ReactNode; note?: ReactNode;
}

/** Two lines of hands: the song's on the first; whose, how loud and where it came from on the second. */
export function Deck(h: Hands) {
  const [loud, setLoud] = useState(false);
  const mode = h.mode ? playModes[h.mode] : null;
  const b = (k: string, label: string, icon: ReactNode, f: () => void, off?: boolean, cls = '') => <button type="button" className={`ctl ${cls}`} onClick={f} disabled={off || h.busy === k} title={label} aria-label={`${h.name}${label}`}>{h.busy === k ? <span className="spinner spinner-tiny" /> : icon}</button>;
  return (
    <div className="ctls">
      <div className="ctl-play" role="group" aria-label="播放控制">
        {mode ? b('mode', `播放方式：${mode.name}，点一下换成${playModes[mode.next].name}`, mode.icon, () => h.onMode(mode.next), false, 'ctl-bare') : <span className="ctl ctl-none" />}
        {b('previous', '上一首', icons.previous, h.onPrevious, h.off || h.alone)}
        {h.state === 'playing' ? b('pause', '暂停', h.waiting ? <span className="spinner" /> : icons.pause, h.onToggle, h.off, 'ctl-main') : b('play', '播放', icons.play, h.onToggle, h.off, 'ctl-main')}
        {b('next', '下一首', icons.next, h.onNext, h.off || h.alone)}
        {b('list', h.count ? `播放列表，${h.count} 首` : '播放列表', <>{icons.list}{h.count > 1 && <small className="num">{h.count > 999 ? '999+' : h.count}</small>}</>, h.onList, false, 'ctl-bare ctl-list')}
      </div>
      <div className="deck-foot">
        {loud && h.vol ? <Volume vol={h.vol} name={h.name} onClose={() => setLoud(false)} /> : <>
          {h.inlineVolume && h.vol ? <Volume vol={h.vol} name={h.name} /> : <span className="foot-who">{h.foot}</span>}
          <span className="foot-acts">
            {h.vol && !h.inlineVolume && <button type="button" className="foot-btn" disabled={h.vol.value === null} onClick={() => setLoud(true)} title="音量" aria-label={`${h.name}音量 ${h.vol.value ?? ''}`}>{h.vol.value === 0 ? icons.mute : icons.volume}</button>}
            {h.onWake && <button type="button" className="foot-btn" disabled={h.busy === 'wake'} onClick={h.onWake} title="唤醒" aria-label={`${h.name}唤醒`}>{h.busy === 'wake' ? <span className="spinner spinner-tiny" /> : icons.wake}</button>}
            {h.tail}
          </span>
        </>}
      </div>
      {h.note}
    </div>
  );
}
