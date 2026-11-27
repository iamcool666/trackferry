import { useEffect, useState, type FormEvent } from 'react';
import { api, type ApiError } from '../api';
import { PhoneIcon, Setting } from './Setting';

/** What this browser is called. The family shares one password; each phone keeps its own name. */
export function ThisDevice() {
  const [name, setName] = useState('');
  const [draft, setDraft] = useState('');
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [unread, setUnread] = useState('');
  useEffect(() => {
    api.me().then((m) => { setName(m.name); setDraft(m.name); setUnread(m.name ? '' : '服务器没给这台设备起名字，在下面填一个保存即可'); },
      (err: ApiError) => setUnread(err.status === 404 ? '服务器还是旧版本，升级后才有设备名字' : `读不到这台设备的名字：${err.message}`));
  }, []);
  const save = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setMsg(null);
    try {
      const m = await api.rename(draft);
      setName(m.name);
      setDraft(m.name);
      setMsg({ ok: true, text: '已改名' });
    } catch (err) {
      setMsg({ ok: false, text: (err as ApiError).message });
    } finally {
      setBusy(false);
    }
  };
  return (
    <Setting id="me" icon={<PhoneIcon />} title="这台设备" state={name || undefined} light="ok"
      says="全家共用一个密码，每台设备各有名字。往音箱送歌时会写上是谁点的">
      <p className="device-now">{name ? <>这台设备现在叫 <b>{name}</b></> : unread || '正在读取名字…'}</p>
      <form className="form form-inline" onSubmit={save}>
        <label className="field"><span>改名</span><input value={draft} onChange={(e) => setDraft(e.target.value)} maxLength={20} placeholder="比如：客厅 iPad" required /></label>
        <div className="card-actions"><button className="btn" disabled={busy || !draft.trim() || draft.trim() === name}>保存</button></div>
      </form>
      {msg && <p className={msg.ok ? 'ok-text' : 'field-error'}>{msg.text}</p>}
      <div className="kept"><div><b>各台设备自己记着的</b><small>上次选的播放位置（本机、哪台音箱或电视），本机的播放列表、均衡器和歌词字号，都只在这台设备上，不影响家里其他人。设置、收藏和歌单全家共用</small></div></div>
    </Setting>
  );
}
