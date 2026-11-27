import { useCallback, useEffect, useState } from 'react';
import { api, ApiError, type Device, type TakeoverRun } from '../api';
import { deviceTitle } from '../copy';
import { Notice } from './Notice';
import { RunRow } from './Run';

/** Interrupts Xiao AI on one speaker through the same loop a real takeover uses. */
export function Takeover({ devices, one }: { devices: Device[]; /** Inside one speaker's settings: no picking, only its own runs. */ one?: boolean }) {
  const [runs, setRuns] = useState<TakeoverRun[] | null>(null);
  const [ready, setReady] = useState(true);
  const [device, setDevice] = useState('');
  const [source, setSource] = useState<'tone' | 'library'>('tone');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const r = await api.takeoverRuns(30);
      setRuns(r.runs.filter((x) => x.kind === 'test' && (!one || x.device_id === devices[0]?.device_id)).slice(0, one ? 3 : 8));
      setReady(r.stream_ready);
    } catch {
      /* transient */
    }
  }, [one, devices[0]?.device_id]); // eslint-disable-line react-hooks/exhaustive-deps

  const running = runs?.some((r) => r.outcome === 'running') ?? false;
  useEffect(() => {
    refresh();
    const t = setInterval(() => { if (!document.hidden) refresh(); }, running ? 700 : 4000);
    return () => clearInterval(t);
  }, [refresh, running]);

  const target = devices.find((d) => d.device_id === device) ?? devices[0];
  const start = async () => {
    if (!target) return;
    setBusy(true);
    setError(null);
    try {
      const r = await api.takeoverTest(target.device_id, source);
      setRuns((cur) => [r, ...(cur ?? [])]);
    } catch (e) {
      setError((e as ApiError).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className={`takeover ${one ? 'takeover-one' : ''}`}>
      <div className="takeover-head">
        <h2>{one ? '试一下接管' : '接管测试'}</h2>
        <p>会打断音箱并出声，在家时再试。</p>
      </div>
      {!ready && <Notice tone="error" title="还没有设置 public_base_url"><p>在 config.yaml 写上音箱能访问的地址，再重启。</p></Notice>}
      {error && <Notice tone="error" title={error} />}
      {devices.length === 0 ? (
        <p className="muted">先把一台音箱设为观察。</p>
      ) : (
        <div className="takeover-form">
          {!one && <div className="segmented" role="group" aria-label="音箱">
            {devices.map((d) => <button key={d.device_id} type="button" className={`seg ${d.device_id === target?.device_id ? 'on' : ''}`} aria-pressed={d.device_id === target?.device_id} onClick={() => setDevice(d.device_id)}>{deviceTitle(d)}</button>)}
          </div>}
          <div className="segmented" role="group" aria-label="测试音频">
            {([['tone', '提示音'], ['library', '曲库随机一首']] as const).map(([v, l]) => <button key={v} type="button" className={`seg ${source === v ? 'on' : ''}`} aria-pressed={source === v} onClick={() => setSource(v)}>{l}</button>)}
          </div>
          <button className="btn" onClick={start} disabled={busy || !ready || running}>
            {busy || running ? <><span className="spinner spinner-tiny" />正在接管…</> : '开始测试'}
          </button>
        </div>
      )}
      {runs && runs.length > 0 && <ul className="run-list">{runs.map((r) => <RunRow key={r.id} run={r} />)}</ul>}
    </section>
  );
}
