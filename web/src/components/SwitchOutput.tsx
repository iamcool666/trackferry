import { useEffect, useRef, useState } from 'react';
import type { Device } from '../api';
import { deviceTitle } from '../copy';
import { connected, HERE, lookAt } from '../ear';
import { checkedSpeaker, give, take, type Taken } from '../handoff';
import { hereName, hereSong, useHere } from '../here';
import { useLive } from '../live';
import { tvID, tvTarget, useTVOutputs } from '../tv';
import { Sheet } from './Sheet';
import { hereIcon, outputIcon, speakerIcon } from './SpeakerChip';
import { useToast } from './Toast';

export const switchIcon = <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M3 9.5V6.5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2h-5.5M3 13.5a6 6 0 0 1 6 6M3 17a2.5 2.5 0 0 1 2.5 2.5" /></svg>;

/** Whether source has something that can be moved to another output. */
export function useMovable(source: string) {
  const { flows } = useLive();
  const h = useHere();
  const tv = useTVOutputs().find((d) => d.online && d.id === tvID(source));
  if (source === HERE) return !!hereSong(h) && !h.waiting;
  if (tvID(source)) return !!tv && !!tv.playback.current.title && tv.playback.phase !== 'buffering' && tv.playback.phase !== 'idle';
  return !!checkedSpeaker(flows, source);
}

/** Moves what source plays to any other output: this device, a speaker or a television. */
export function SwitchOutput({ source, devices, className = 'foot-btn' }: { source: string; devices: Device[]; className?: string }) {
  const televisions = useTVOutputs().filter((d) => d.online);
  const movable = useMovable(source);
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState('');
  const [taken, setTaken] = useState<Taken | null>(null);
  const [toast, say] = useToast();
  const op = useRef<AbortController>(undefined);
  useEffect(() => () => op.current?.abort(), []);
  // Read ahead: this device may only start playing inside the tap itself.
  useEffect(() => {
    if (!open || source === HERE) return;
    const c = new AbortController();
    take(source, c.signal).then(setTaken, (e) => { if (!c.signal.aborted) { setOpen(false); say({ tone: 'bad', text: (e as Error).message }); } });
    // The source may be paused, moved or changed while the sheet stays open: read it again.
    const again = setInterval(() => { take(source, c.signal).then(setTaken, () => {}); }, 4000);
    return () => { clearInterval(again); c.abort(); };
  }, [open, source, say]);
  if (!movable) return null;
  const choices = [
    ...(source !== HERE ? [{ id: HERE, name: hereName, icon: hereIcon }] : []),
    ...connected(devices).filter((d) => d.device_id !== source).map((d) => ({ id: d.device_id, name: deviceTitle(d), icon: speakerIcon })),
    ...televisions.filter((d) => tvTarget(d.id) !== source).map((d) => ({ id: tvTarget(d.id), name: d.name, icon: outputIcon(d.kind) })),
  ];
  if (!choices.length) return null;
  const blocked = (id: string) => {
    if (id === HERE) return taken ? '' : '正在读取进度…';
    return '';
  };
  const move = (target: string) => {
    if (op.current) return;
    const job = op.current = new AbortController();
    const signal = job.signal;
    setBusy(target);
    // Toward this device nothing may be awaited before give: the tap has to start the sound.
    const run = target === HERE ? give(taken!, target, signal) : take(source, signal).then((t) => give(t, target, signal));
    run.then(() => { setOpen(false); lookAt(target); }, (e) => {
      if (signal.aborted) return;
      setOpen(false);
      say({ tone: 'bad', text: (e as Error).message });
    }).finally(() => {
      op.current = undefined;
      if (!signal.aborted) { setBusy(''); setTaken(null); }
    });
  };
  return <>
    <button type="button" className={className} title="切换输出" aria-label="切换输出" aria-haspopup="dialog" onClick={() => setOpen(true)}>{switchIcon}</button>
    {open && <Sheet title="切换输出" onClose={() => { if (!busy) { setOpen(false); setTaken(null); } }}>
      <div className="picks">{choices.map((d) => {
        const why = blocked(d.id);
        return <button type="button" key={d.id} disabled={!!busy || !!why} aria-busy={busy === d.id || undefined} onClick={() => move(d.id)}>
          <i>{d.icon}</i><span><b>{d.name}</b>{(why || busy === d.id) && <small>{busy === d.id ? '正在等它接上…' : why}</small>}</span>
          {busy === d.id && <span className="spinner spinner-tiny" />}
        </button>;
      })}</div>
    </Sheet>}
    {toast}
  </>;
}
