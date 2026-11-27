import { useEffect, useRef, useState } from 'react';
import { api, type CastListing } from '../api';
import { refreshTVOutputs } from '../tv';
import { canSendOn, sendOn } from '../here';
import { castIcon } from './SpeakerChip';

const kindName = { dlna: 'DLNA', chromecast: 'Chromecast' } as const;

/** DLNA and Chromecast devices on the network; one turned on shows up as an output like a TV. */
export function CastDevices() {
  const [list, setList] = useState<CastListing | null>(null);
  const [available, setAvailable] = useState(true);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const [note, setNote] = useState('');
  const timer = useRef(0);
  useEffect(() => {
    let alive = true;
    const load = () => api.castList().then((l) => {
      if (!alive) return;
      setList(l);
      // Ask again soon while a search runs, else now and then for devices going on and off line.
      timer.current = window.setTimeout(load, l.scanning ? 1200 : 8000);
    }, (e) => {
      if (!alive) return;
      if (e.status === 404) { setAvailable(false); return; }
      setError(e.message);
      timer.current = window.setTimeout(load, 8000);
    });
    void load();
    return () => { alive = false; clearTimeout(timer.current); };
  }, []);
  const scan = async () => {
    setError('');
    try { setList(await api.castScan()); } catch (e) { setError((e as Error).message); }
  };
  const set = async (id: string, on: boolean) => {
    setBusy(id); setError('');
    try { setList(await api.castSet(id, on)); void refreshTVOutputs().catch(() => {}); } catch (e) { setError((e as Error).message); }
    finally { setBusy(''); }
  };
  const local = canSendOn();
  if (!available && !local) return null;
  return <>
    <header className="cast-head"><h2>投放</h2>{available && <button type="button" className="btn btn-quiet" disabled={!!list?.scanning} onClick={() => void scan()}>{list?.scanning ? '正在搜索…' : '重新搜索'}</button>}</header>
    {available && <>
      <p className="muted">打开的设备会和电视一样出现在“切换到”里：放歌、暂停、切歌、跳进度、调音量都由服务器直接控制，手机关了也照样放。</p>
      {list && !list.devices.length && <p className="muted">{list.scanning ? '正在局域网里找 DLNA 和 Chromecast 设备…' : list.error || '局域网里没找到 DLNA 或 Chromecast 设备。确认设备已开机、和服务器在同一个网段。'}</p>}
      {!!list?.devices.length && <ul>{list.devices.map((d) => {
        const state = d.on ? (d.online ? '在线' : d.present ? '连接中' : '不在网络上') : d.present ? '未打开' : '';
        return <li key={d.id}>{castIcon}<span><b>{d.name}</b><small data-online={d.on && d.online}>{kindName[d.kind]}{d.model ? ` · ${d.model}` : ''} · {state}</small></span>
          <label className="switch"><input type="checkbox" role="switch" checked={d.on} disabled={busy === d.id} onChange={(e) => void set(d.id, e.target.checked)} aria-label={`投放到 ${d.name}`} /><span /></label>
        </li>;
      })}</ul>}
      {list?.error && !!list.devices.length && <p className="muted">{list.error}</p>}
    </>}
    {local && <div className="cast-local"><span><b>从这台设备投放</b><small>用浏览器自带的 AirPlay 或 Cast，把本机正在放的声音送出去。</small></span>
      <button type="button" className="btn btn-quiet" onClick={() => { setNote(''); void sendOn().then(setNote); }}>选择设备</button></div>}
    {note && <p className="muted" role="status">{note}</p>}
    {error && <p className="field-error" role="alert">{error}</p>}
  </>;
}
