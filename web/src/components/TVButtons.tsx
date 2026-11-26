import { useEffect, useRef, useState, type ReactNode } from 'react';
import { api, type TVCommand, type TVOutput, type TVTrack } from '../api';
import { commandTV } from '../tv';
import { Deck, playModes } from './Deck';
import { TVQueue } from './Queue';
import { useToast } from './Toast';

/** Metadata for the current queue, scoped to the television and the queue revision. */
export function useTVItems(device: TVOutput | undefined, retry: number) {
  const id = device?.id, revision = device?.playback.revision, count = device?.playback.count;
  const [result, setResult] = useState<{ id: string; revision: string; items?: TVTrack[]; error?: string }>();
  useEffect(() => {
    if (!id || !revision || !count) return;
    const job = new AbortController();
    api.tvSnapshot(id, job.signal).then((snapshot) => {
      if (job.signal.aborted || snapshot.playback.revision !== revision) return;
      setResult(snapshot.items.length === count ? { id, revision, items: snapshot.items } : { id, revision, error: '暂时读不到电视的播放列表' });
    }, (e: Error) => {
      if (!job.signal.aborted) setResult({ id, revision, error: e.message });
    });
    return () => job.abort();
  }, [id, revision, count, retry]);
  return count && result?.id === id && result?.revision === revision ? result : undefined;
}

export function TVButtons({ device, items, queueError, onQueue, foot, tail }: {
  device: TVOutput; items?: TVTrack[]; queueError?: string; onQueue: () => void; foot: ReactNode; tail: ReactNode;
}) {
  const p = device.playback;
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [toast, say] = useToast();
  const op = useRef<AbortController>(undefined);
  useEffect(() => () => op.current?.abort(), []);
  const active = p.phase === 'playing' || p.phase === 'buffering';
  const send = async (cmd: TVCommand, row?: string) => {
    if (op.current) return;
    const job = op.current = new AbortController();
    setBusy(row ?? cmd.action);
    setError(null);
    const guard = cmd.action === 'volume' ? {} : { revision: p.revision, entry: p.entry };
    try {
      await commandTV(device.id, { ...guard, ...cmd }, job.signal);
      if (cmd.mode) say({ tone: 'ok', text: playModes[cmd.mode].name });
    } catch (e) {
      if (!job.signal.aborted) {
        const text = (e as Error).message;
        if (row !== undefined) setError(text);
        else say({ tone: 'bad', text });
      }
    } finally {
      if (op.current === job) {
        op.current = undefined;
        if (!job.signal.aborted) setBusy('');
      }
    }
  };
  return <>
    <Deck name={device.name} state={active ? 'playing' : p.count ? 'paused' : 'idle'} waiting={p.phase === 'buffering'} busy={busy}
      off={!p.count} alone={p.count < 2} mode={p.mode} count={p.count}
      onMode={(mode) => void send({ action: 'mode', mode })} onToggle={() => void send({ action: active ? 'pause' : 'play' })}
      onPrevious={() => void send({ action: 'previous' })} onNext={() => void send({ action: 'next' })}
      onList={() => { setError(null); onQueue(); setOpen(true); }}
      vol={typeof p.volume === 'number' ? { value: p.volume, set: (volume) => void send({ action: 'volume', volume }) } : null} foot={foot} tail={tail} />
    {open && <TVQueue device={device} items={items} busy={busy} error={error ?? queueError ?? null} send={send} onClose={() => setOpen(false)} />}
    {toast}
  </>;
}
