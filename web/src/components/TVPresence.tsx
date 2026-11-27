import { useEffect, useRef, useState, type FormEvent } from 'react';
import { api } from '../api';
import { HERE, lookAt, selectNewTV, usePick } from '../ear';
import { hereNow } from '../here';
import { takesNewTV } from '../output-rule';
import type { Place } from '../nav';
import { tvTarget, useTVOutputs, useTVPairing, useTVReady } from '../tv';
import { outputIcon } from './SpeakerChip';
import { Sheet } from './Sheet';
import '../pages/tv-remote.css';

const seenKey = 'trackferry.tv-seen';
function seenTVs() {
  try {
    const ids: unknown = JSON.parse(sessionStorage.getItem(seenKey) ?? '[]');
    if (Array.isArray(ids)) return new Set(ids.filter((id): id is string => typeof id === 'string'));
  } catch { /* only remember for this visit */ }
  return new Set<string>();
}

export function TVPresence({ place, navigate, pairing, setPairing }: { place: Place; navigate: (place: Place) => void; pairing: boolean; setPairing: (open: boolean) => void }) {
  const outputs = useTVOutputs();
  const pending = useTVPairing();
  const ready = useTVReady();
  const [seen] = useState(seenTVs);
  const ignored = useRef('');
  const [notice, setNotice] = useState('');
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const blocked = place.page === 'welcome';
  const picked = usePick();
  const pick = useRef(picked);
  pick.current = picked;

  useEffect(() => {
    if (!ready) return;
    const ids = new Set(outputs.filter((d) => d.online).map((d) => d.id));
    const arrived = [...ids].find((id) => !seen.has(id));
    setNotice((old) => arrived ?? (ids.has(old) ? old : ''));
    // A television that went off keeps its pick; this page shows another output until it is back.
    if (arrived) { const h = hereNow(); selectNewTV(arrived, takesNewTV(pick.current, [{ id: HERE, song: h.songs.length > 0, playing: h.on }])); }
    if (ids.size !== seen.size || [...ids].some((id) => !seen.has(id))) {
      seen.clear();
      ids.forEach((id) => seen.add(id));
      try { sessionStorage.setItem(seenKey, JSON.stringify([...ids])); } catch { /* only remember for this visit */ }
    }
  }, [outputs, ready, seen]);
  useEffect(() => {
    if (!notice) return;
    const timer = setTimeout(() => setNotice(''), 15_000);
    return () => clearTimeout(timer);
  }, [notice]);
  useEffect(() => {
    if (blocked || !pending || pairing || ignored.current === pending.id || document.querySelector('dialog[open]')) return;
    setPairing(true);
  }, [pending, pairing, blocked, setPairing]);

  const close = () => {
    if (pending) ignored.current = pending.id;
    setCode(''); setError(''); setPairing(false);
  };
  const pair = async (e: FormEvent) => {
    e.preventDefault();
    if (busy || code.length !== 6) return;
    setBusy(true); setError('');
    try { await api.pairTV(code); close(); }
    catch (e) { setError((e as Error).message); }
    finally { setBusy(false); }
  };
  const arrived = outputs.find((d) => d.online && d.id === notice);
  if (blocked) return null;
  return <>
    {!pairing && arrived && <aside className="tv-arrival glass dock-glass" aria-label="电视上线">
      <button type="button" className="tv-arrival-open" onClick={() => { lookAt(tvTarget(arrived.id)); setNotice(''); navigate({ page: 'home' }); }}>
        {outputIcon(arrived.kind)}<b title={arrived.name}>{arrived.name}</b><span>已上线</span>
      </button>
      <button type="button" className="tv-arrival-close" aria-label="关闭电视上线提醒" onClick={() => setNotice('')}>×</button>
    </aside>}
    {pairing && <Sheet title="配对电视" className="device-pair" onClose={close}>
      <form className="device-pair-form" onSubmit={pair}>
        <p>{pending ? `${pending.name} 正在等待配对。` : '打开电视上的 TrackFerry，选择这台服务器。'}</p>
        <label htmlFor="device-code">电视上的 6 位数字</label>
        <input id="device-code" autoComplete="off" inputMode="numeric" pattern="[0-9]{6}" maxLength={6} placeholder="000000" value={code} onChange={(e) => setCode(e.target.value.replace(/\D/g, '').slice(0, 6))} />
        <button className="btn btn-primary" disabled={busy || code.length !== 6}>{busy ? '正在配对…' : '确认配对'}</button>
        {error && <p className="field-error" role="alert">{error}</p>}
      </form>
    </Sheet>}
  </>;
}
