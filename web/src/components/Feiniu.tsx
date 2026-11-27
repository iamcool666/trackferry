import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { api, ApiError, type FeiniuInfo, type FeiniuServer, type LibraryStatus } from '../api';
import { Notice } from './Notice';
import { Pick } from './Pick';
import { Setting, type Light } from './Setting';
import { SourceIcon } from './SourceIcon';
import { clock, day, timeAgo } from '../copy';

/** Connects 飞牛音乐, where the library lives. */
export function FeiniuCard({ onLight }: { onLight: (l: Light) => void }) {
  const [info, setInfo] = useState<FeiniuInfo | null>(null);
  const [lib, setLib] = useState<LibraryStatus | null>(null);
  const [editing, setEditing] = useState(false);
  const [base, setBase] = useState('');
  const [found, setFound] = useState<{ servers: FeiniuServer[]; blocked: string[]; relay: boolean } | null>(null);
  const [looking, setLooking] = useState(false);
  const [manual, setManual] = useState(false);
  const [user, setUser] = useState('');
  const [pw, setPw] = useState('');
  const [code, setCode] = useState('');
  const [gate, setGate] = useState(false);
  const [busy, setBusy] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const [f, l] = await Promise.all([api.feiniu(), api.library()]);
      setInfo(f);
      setLib({ ...l.library, every_minutes: l.every_minutes });
    } catch (e) {
      setError((e as ApiError).message);
    }
  }, []);
  const running = lib?.running ?? false;
  useEffect(() => {
    load();
    const t = setInterval(() => { if (!document.hidden) load(); }, running ? 1200 : 15000);
    return () => clearInterval(t);
  }, [load, running]);

  const act = async (f: () => Promise<unknown>) => {
    setBusy(true);
    setError(null);
    try {
      await f();
      await load();
      return true;
    } catch (e) {
      setError((e as ApiError).message);
      if ((e as ApiError).code === 'feiniu_gate') setGate(true);
      return false;
    } finally {
      setBusy(false);
    }
  };
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (await act(() => api.linkFeiniu(base.trim() || info!.feiniu.base || '', user.trim(), pw, code.trim()))) { setEditing(false); setCode(''); }
    setPw('');
  };

  // Open the form and 飞牛音乐 is looked for; nobody should have to type an address.
  const wantsForm = !!info && (editing || !info.feiniu.linked || info.feiniu.expired);
  const look = useCallback(async () => {
    setLooking(true);
    try {
      const d = await api.discoverFeiniu();
      setFound(d);
      setBase((b) => b || d.servers[0]?.base || '');
    } catch (e) {
      setFound({ servers: [], blocked: [], relay: true });
      setError((e as ApiError).message);
    } finally {
      setLooking(false);
    }
  }, []);
  useEffect(() => { if (wantsForm) look(); }, [wantsForm, look]);

  const every = async (h: number) => {
    setLib((x) => x && { ...x, every_minutes: h });
    try { await api.setEvery(h); } catch (e) { setError((e as ApiError).message); }
  };
  const light: Light | null = !info ? null : info.feiniu.expired ? 'bad' : info.feiniu.linked ? 'ok' : 'warn';
  useEffect(() => { if (light) onLight(light); }, [light, onLight]);

  if (!info || !lib) return <Setting id="feiniu" icon={<SourceIcon name="feiniu" />} title="飞牛音乐">{error ? <Notice tone="error" title={error} /> : <span className="spinner" />}</Setting>;
  const f = info.feiniu;
  const form = editing || !f.linked || f.expired;
  const asksCode = gate || f.code || !!code || !!found?.servers.some((s) => s.gate && s.base === base);
  const n = (v: number) => v.toLocaleString('zh-CN');
  const last = lib.last;

  return (
    <Setting id="feiniu" icon={<SourceIcon name="feiniu" />} title="飞牛音乐" light={light ?? 'off'} state={f.expired ? '登录已失效' : f.linked ? '已连接' : '未连接'}
      says={f.linked ? `${f.server || '飞牛'}，${f.user}` : '曲库、封面、歌词和收藏都来自这里'}
      actions={f.linked && !form ? <><button className="btn" disabled={running || busy} onClick={() => act(api.scanLibrary)}>{running ? <><span className="spinner spinner-tiny" />正在同步</> : '立即同步'}</button><button className="btn btn-quiet" onClick={() => setEditing(true)}>换个用户</button><button className="btn btn-quiet" onClick={() => setConfirm(true)}>断开</button></> : undefined}>
        {f.expired && <Notice tone="error" title="飞牛音乐的登录已失效"><p>重新登录后继续使用，曲库不用重建。</p></Notice>}
        {f.linked && (
          <>
            <dl className="tally">
              <div><dd className="num">{n(running ? lib.seen : lib.tracks)}</dd><dt>{running ? '首已读' : '首歌'}</dt></div>
              <div><dd className="num">{n(lib.artists)}</dd><dt>位歌手</dt></div>
              {!running && last?.finished_ms ? <div><dd className="num">{clock(last.finished_ms).slice(0, 5)}</dd><dt>{day(last.finished_ms)}同步</dt></div> : null}
              {!running && last?.skipped ? <div><dd className="num">{n(last.skipped)}</dd><dt>首音箱放不了</dt></div> : null}
            </dl>
            {(last?.roots.length ?? 0) > 1 && <div className="shares" aria-label="目录">{last!.roots.map((x) => <span key={x.name} style={{ flexGrow: x.tracks }}><b>{x.name || '根目录'}</b><small className="num">{n(x.tracks)}</small></span>)}</div>}
            <div className="kept"><div><b>自动同步</b><small>飞牛里新加的歌、收藏和歌单隔多久读一次，没变化时几乎不费资源</small></div>
              <Pick label="自动同步" value={lib.every_minutes ?? 360} choices={[5, 15, 30, 60, 180, 360, 720, 1440].map((m) => ({ value: m, label: m < 60 ? `每 ${m} 分钟` : `每 ${m / 60} 小时` })).concat({ value: 0, label: '关' })} onPick={every} /></div>
            {last?.error && <p className="field-error">{last.error}</p>}
            <dl className="kv">
              <div><dt>地址</dt><dd className="mono">{f.base}</dd></div>
              {f.version && <div><dt>版本</dt><dd className="num">{f.version}</dd></div>}
              {f.linked_at && <div><dt>连接</dt><dd>{timeAgo(f.linked_at)}</dd></div>}
            </dl>
          </>
        )}
        {confirm && !form && (
          <div className="confirm">
            <p>断开后会删除保存的登录，曲库回到扫描音乐目录。飞牛音乐里的内容不受影响。</p>
            <div className="card-actions"><button className="btn btn-danger" disabled={busy} onClick={async () => { if (await act(api.unlinkFeiniu)) setConfirm(false); }}>确认断开</button><button className="btn btn-quiet" onClick={() => setConfirm(false)}>取消</button></div>
          </div>
        )}
        {form && (
          <form className="form" onSubmit={submit}>
            <div className="finder" aria-live="polite">
              {looking ? <p><span className="radar" aria-hidden="true" />正在找飞牛音乐</p> : found && found.servers.length > 0 ? (
                <ul>{found.servers.map((s) => (
                  <li key={s.base}><button type="button" aria-pressed={!manual && base === s.base} onClick={() => { setManual(false); setBase(s.base); }}><b>{s.name || '飞牛'}</b><span>{s.gate ? '开了访问码' : `飞牛音乐 ${s.version}`}</span><small>{s.base.includes('127.0.0.1') ? '就在这台 NAS 上' : s.base.replace('https://', '')}</small></button></li>
                ))}</ul>
              ) : found && (
                found.relay ? <p>NAS 上的飞牛音乐没有应答，确认它已安装并启动。</p> : found.blocked.length ? <div className="blocked"><p>容器连不到这台 NAS。在 compose.yaml 里用 host 网络，再执行：</p><pre>docker compose up -d</pre></div> : <p>附近没有找到飞牛音乐，确认它已安装并启动。</p>
              )}
              {!looking && found && <span className="finder-act"><button type="button" className="link" onClick={look}>重新找</button>{!manual && <button type="button" className="link" onClick={() => setManual(true)}>手动填地址</button>}</span>}
            </div>
            {manual && <label className="field"><span>地址</span><input value={base} onChange={(e) => setBase(e.target.value)} placeholder={f.base || info.suggested || 'https://NAS 的地址:5667'} autoCapitalize="off" spellCheck={false} /></label>}
            <label className="field"><span>用户名</span><input value={user} onChange={(e) => setUser(e.target.value)} autoComplete="off" autoCapitalize="off" spellCheck={false} required /></label>
            <label className="field"><span>密码</span><input type="password" value={pw} onChange={(e) => setPw(e.target.value)} autoComplete="off" required /></label>
            {asksCode
              ? <label className="field"><span>访问码</span><input type="password" value={code} onChange={(e) => setCode(e.target.value)} autoComplete="off" placeholder={f.code ? '没改过就留空' : '飞牛设置的访问码'} /></label>
              : <p className="hint"><button type="button" className="link" onClick={() => setGate(true)}>飞牛开了访问码</button></p>}
            <div className="card-actions"><button className="btn btn-primary" disabled={busy || looking || !(base.trim() || f.base) || !user.trim() || !pw}>{busy ? <><span className="spinner spinner-tiny" />正在连接</> : '连接'}</button>{editing && <button type="button" className="btn btn-quiet" onClick={() => setEditing(false)}>取消</button>}</div>
            <p className="hint">密码只用于登录，不保存。{asksCode && '访问码存在这台飞牛上，通行过期后自动再验。'}平时用飞牛账号授权登录的用户，要先在飞牛音乐里设过密码。</p>
          </form>
        )}
        {error && <p className="field-error" role="alert">{error}</p>}
    </Setting>
  );
}
