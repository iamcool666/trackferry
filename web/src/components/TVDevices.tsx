import { useEffect, useState, type FormEvent } from 'react';
import { api, type TVDevice } from '../api';
import { refreshTVOutputs, useTVOutputs } from '../tv';
import { tvIcon } from './SpeakerChip';
import { Sheet } from './Sheet';
import { CastDevices } from './CastDevices';

export function TVDevices({ onPair }: { onPair: () => void }) {
  const states = useTVOutputs();
  const [saved, setSaved] = useState<TVDevice[] | null>(null);
  const [available, setAvailable] = useState(true);
  const [revoked, setRevoked] = useState<string[]>([]);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const [editing, setEditing] = useState<{ id: string; name: string }>();
  useEffect(() => {
    const job = new AbortController();
    api.tvDevices(job.signal).then((result) => setSaved(result.devices), (e) => {
      if (job.signal.aborted) return;
      if (e.status === 404) setAvailable(false);
      else setError(e.message);
    });
    return () => job.abort();
  }, []);
  const devices = new Map((saved ?? []).map((d) => [d.id, d]));
  for (const item of states) if (!item.kind) devices.set(item.id, item);
  for (const id of revoked) devices.delete(id);

  const revoke = async (device: TVDevice) => {
    setError(''); setBusy(device.id);
    try {
      await api.revokeTV(device.id);
      setRevoked((old) => [...old, device.id]);
    } catch (e) { setError((e as Error).message); }
    finally { setBusy(''); }
  };
  const rename = async (event: FormEvent) => {
    event.preventDefault();
    if (!editing || busy || !editing.name.trim()) return;
    setBusy(editing.id); setError('');
    try {
      const renamed = await api.renameTV(editing.id, editing.name);
      setSaved((old) => [renamed, ...(old ?? []).filter((d) => d.id !== renamed.id)]);
      setEditing(undefined);
      void refreshTVOutputs().catch(() => {});
    } catch (e) { setError((e as Error).message); }
    finally { setBusy(''); }
  };
  return <section className="tv-settings">
    <header><h1>电视</h1>{available && <button className="btn btn-primary" onClick={onPair}>配对电视</button>}</header>
    {!available && <p className="muted">这台服务器没有启用电视服务。</p>}
    {available && !devices.size && saved && <p className="muted">打开电视上的 TrackFerry，选择服务器后输入屏幕上的配对码。</p>}
    {!!devices.size && <ul>{[...devices.values()].map((d) => {
      const online = states.some((item) => item.id === d.id && item.online);
      return <li key={d.id}>{tvIcon}<span><b>{d.name}</b><small data-online={online}>{online ? '在线' : '离线'}</small></span>
        <button type="button" className="btn btn-quiet" disabled={!!busy} onClick={() => { setError(''); setEditing({ id: d.id, name: d.name }); }} aria-label={`重命名 ${d.name}`}>重命名</button>
        <button type="button" className="btn btn-quiet" disabled={!!busy} onClick={() => void revoke(d)} aria-label={`解除 ${d.name} 的配对`}>解除配对</button>
      </li>;
    })}</ul>}
    {!editing && error && <p className="field-error" role="alert">{error}</p>}
    {editing && <Sheet title="电视名称" onClose={() => { if (!busy) { setEditing(undefined); setError(''); } }}>
      <form className="tv-name-form" onSubmit={rename}>
        <input aria-label="电视名称" placeholder="例如 客厅电视" maxLength={20} value={editing.name} onChange={(e) => setEditing({ ...editing, name: e.target.value })} />
        <button className="btn btn-primary" disabled={!!busy || !editing.name.trim()}>保存</button>
        {error && <p className="field-error" role="alert">{error}</p>}
      </form>
    </Sheet>}
    <CastDevices />
  </section>;
}
