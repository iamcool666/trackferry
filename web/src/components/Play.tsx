import { Sheet } from './Sheet';
import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { ApiError, type Device } from '../api';
import { deviceTitle } from '../copy';
import { lookAt } from '../ear';
import { hereName } from '../here';
import { hereIcon } from './SpeakerChip';

/** Plays something on a speaker that is connected. */
/** here plays it on this device. */
export function Play({ send, here, title, devices, label }: { send: (device: string) => Promise<unknown>; here: () => Promise<unknown> | void; title: string; devices: Device[]; label?: string }) {
  const targets = devices.filter((d) => d.in_account && d.mode !== 'off');
  const [open, setOpen] = useState(false);
  const [down, setDown] = useState(false);
  const [state, setState] = useState<{ tone: 'busy' | 'ok' | 'bad'; text: string } | null>(null);
  const box = useRef<HTMLSpanElement>(null);
  useEffect(() => {
    if (!open) return;
    const off = (e: Event) => { if (document.querySelector('dialog[open]')) return; if (e instanceof KeyboardEvent ? e.key === 'Escape' : !box.current?.contains(e.target as Node)) setOpen(false); };
    addEventListener('pointerdown', off);
    addEventListener('keydown', off);
    return () => { removeEventListener('pointerdown', off); removeEventListener('keydown', off); };
  }, [open]);
  useEffect(() => {
    if (!state || state.tone === 'busy') return;
    const t = setTimeout(() => setState(null), state.tone === 'bad' ? 12000 : 5000);
    return () => clearTimeout(t);
  }, [state]);

  const play = async (d: Device) => {
    setOpen(false);
    setState({ tone: 'busy', text: `正在送到${deviceTitle(d)}` });
    try {
      await send(d.device_id);
      lookAt(d.device_id);
      setState({ tone: 'ok', text: `已送往${deviceTitle(d)}` });
    } catch (e) {
      setState({ tone: 'bad', text: (e as ApiError).message });
    }
  };

  const playHere = async () => {
    setOpen(false);
    try { await here(); } catch (e) { setState({ tone: 'bad', text: (e as Error).message }); }
  };

  return (
    <span className={`play ${label ? 'play-label' : ''}`} ref={box}>
      <button type="button" className={label ? 'btn btn-small' : 'play-btn'} disabled={state?.tone === 'busy'} aria-label={`播放${title}`} aria-expanded={open}
        onClick={(e) => { setDown(e.currentTarget.getBoundingClientRect().top < innerHeight / 2); setOpen(!open); }}>
        <svg viewBox="0 0 12 14" width="11" height="13" aria-hidden="true"><path d="M1 1l10 6-10 6z" fill="currentColor" /></svg>
        {label}
      </button>
      {open && matchMedia('(max-width: 760px)').matches ? (
        <Sheet title="在哪里播放" onClose={() => setOpen(false)}>
          <div className="picks"><button type="button" onClick={playHere}><i>{hereIcon}</i><span><b>{hereName}</b></span></button>{targets.map((d) => <button key={d.device_id} type="button" onClick={() => play(d)}><i><svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true"><rect x="6" y="2.5" width="12" height="19" rx="5" fill="none" stroke="currentColor" strokeWidth="1.5" /><path d="M9 6.5h6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" /><circle cx="12" cy="14.5" r="2.6" fill="none" stroke="currentColor" strokeWidth="1.5" /></svg></i><span><b>{deviceTitle(d)}</b></span></button>)}</div>
        </Sheet>
      ) : open && (
        <span className={`menu ${down ? 'menu-down' : ''}`} role="menu">
          <span className="menu-head">在哪里播放</span>
          <button role="menuitem" type="button" onClick={playHere}>{hereIcon}<span>{hereName}</span></button>
          {targets.map((d) => <button key={d.device_id} role="menuitem" type="button" onClick={() => play(d)}><svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true"><rect x="6" y="2.5" width="12" height="19" rx="5" fill="none" stroke="currentColor" strokeWidth="1.5" /><path d="M9 6.5h6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" /><circle cx="12" cy="14.5" r="2.6" fill="none" stroke="currentColor" strokeWidth="1.5" /></svg><span>{deviceTitle(d)}</span></button>)}
        </span>
      )}
      {/* out of the row: that fades away with the pointer, and would take this along */}
      {state && createPortal(<button type="button" className={`play-state ${state.tone}`} title="点一下关闭" onClick={(e) => { e.stopPropagation(); if (state.tone !== 'busy') setState(null); }}><span role={state.tone === 'bad' ? 'alert' : 'status'}>{state.text}</span></button>, document.body)}
    </span>
  );
}
