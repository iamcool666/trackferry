import { useCallback, useEffect, useRef, useState } from 'react';
import { api, ApiError, type Device, type LinkResult, type SyncInfo } from '../api';
import { LinkAccount } from '../components/LinkAccount';
import { Notice } from '../components/Notice';
import { useBackdrop } from '../stage/Stage';
import { deviceSubtitle, deviceTitle, explainSync, isOnline } from '../copy';

const steps = ['关联小米账号', '选择音箱', '完成'];

/** First-run flow: link the account, pick which speakers TrackFerry may observe, done. */
/** Set when the Xiaomi step was skipped in this browser, so it is not shown again on every visit. */
export const SKIP_KEY = 'trackferry.skip-xiaomi';

export function Onboarding({ initialStep = 0, onFinish }: { initialStep?: number; onFinish: () => void }) {
  useBackdrop();
  const [step, setStep] = useState(initialStep);
  const [sync, setSync] = useState<SyncInfo | null>(null);
  const [devices, setDevices] = useState<Device[]>([]);
  const [chosen, setChosen] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [qrKey, setQrKey] = useState(0);

  const loadDevices = useCallback(async (info: SyncInfo | null) => {
    const res = await api.devices();
    const inAccount = res.devices.filter((d) => d.in_account);
    setDevices(inAccount);
    // Preselect what is already enabled; on a fresh install nothing is.
    setChosen(new Set(inAccount.filter((d) => d.mode !== 'off').map((d) => d.device_id)));
    setSync(info);
  }, []);

  const onLinked = useCallback(
    async (qr: LinkResult) => {
      await loadDevices(qr.sync ?? null);
      setStep(1);
    },
    [loadDevices],
  );

  const resync = async () => {
    setBusy(true);
    setError(null);
    try {
      const r = await api.sync();
      await loadDevices(r.sync);
    } catch (e) {
      setError((e as ApiError).message);
    } finally {
      setBusy(false);
    }
  };

  // Arriving at step 1 directly (account linked earlier): fetch a fresh list.
  const entered = useRef(false);
  useEffect(() => {
    if (initialStep !== 1 || entered.current) return;
    entered.current = true;
    resync();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const rescan = () => {
    setSync(null);
    setQrKey((k) => k + 1);
    setStep(0);
  };

  const toggle = (id: string) =>
    setChosen((cur) => {
      const next = new Set(cur);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const confirm = async () => {
    setBusy(true);
    setError(null);
    try {
      for (const d of devices) {
        const want = chosen.has(d.device_id) ? (d.mode === 'off' ? 'observe' : d.mode) : 'off';
        if (want !== d.mode) await api.setMode(d.device_id, want);
      }
      setStep(2);
    } catch (e) {
      setError((e as ApiError).message);
    } finally {
      setBusy(false);
    }
  };

  const explanation = sync ? explainSync(sync) : null;
  const enabled = devices.filter((d) => chosen.has(d.device_id));

  return (
    <section className="onboarding">
      <ol className="stepper" aria-label="设置进度">
        {steps.map((label, i) => (
          <li key={label} className={i < step ? 'done' : i === step ? 'current' : ''} aria-current={i === step ? 'step' : undefined}>
            <span className="step-dot" />
            <span className="step-label">{label}</span>
          </li>
        ))}
      </ol>

      {step === 0 && (
        <div className="panel">
          <h2>关联小米账号</h2>
          <p className="lead">用来读取音箱列表和接收点歌。</p>
          <LinkAccount key={qrKey} onLinked={onLinked} />
          <p className="onboard-skip">只用音乐文件夹、电视或投放，可以<button type="button" className="btn btn-quiet" onClick={() => { try { localStorage.setItem(SKIP_KEY, '1'); } catch { /* 只这一次 */ } onFinish(); }}>先不关联小米</button>，以后在“账号”里随时关联。</p>
        </div>
      )}

      {step === 1 && (
        <div className="panel">
          <h2>选择要接入的音箱</h2>
          <p className="lead">勾选的音箱进入观察：只记录，不出声。</p>

          {busy && !sync && <div className="loading-row"><span className="spinner" />正在读取音箱列表…</div>}
          {explanation && explanation.tone !== 'ok' && (
            <Notice tone={explanation.tone} title={explanation.title}>
              {explanation.body && <p>{explanation.body}</p>}
              <div className="notice-actions">
                {explanation.action === 'retry' && <button className="btn btn-small" onClick={resync} disabled={busy}>{busy ? '正在重试…' : '重新获取'}</button>}
                <button className={`btn btn-small ${explanation.action === 'rescan' ? 'btn-primary' : ''}`} onClick={rescan}>换个账号重新登录</button>
              </div>
            </Notice>
          )}
          {explanation?.tone === 'ok' && <p className="found">{explanation.title}{devices.length !== sync?.count ? `（其中 ${devices.length} 台在账号中）` : ''}</p>}

          {devices.length > 0 && (
            <ul className="pick-list">
              {devices.map((d) => (
                <li key={d.device_id}>
                  <label className={`pick ${chosen.has(d.device_id) ? 'pick-on' : ''}`}>
                    <input type="checkbox" checked={chosen.has(d.device_id)} onChange={() => toggle(d.device_id)} />
                    <span className="pick-box" aria-hidden="true"></span>
                    <span className="pick-main">
                      <span className="speaker-name">{deviceTitle(d)}</span>
                      <span className="speaker-meta">
                        <span className={`presence ${isOnline(d) ? 'presence-on' : 'presence-off'}`}>{isOnline(d) ? '在线' : '离线'}</span>
                        <span>{deviceSubtitle(d)}</span>
                      </span>
                    </span>
                  </label>
                </li>
              ))}
            </ul>
          )}
          {devices.length > 0 && <p className="hint">建议先只勾选一台（比如书房），确认记录正常后再加其他房间。</p>}
          {error && <p className="field-error" role="alert">{error}</p>}

          <div className="actions">
            {devices.length > 0 && (
              <button className="link" onClick={resync} disabled={busy}>列表不对？重新获取</button>
            )}
            <span className="spacer" />
            <button className="btn btn-primary" onClick={confirm} disabled={busy || devices.length === 0}>
              {busy && sync ? '正在保存…' : enabled.length ? `启用 ${enabled.length} 台，下一步` : '先都不启用，下一步'}
            </button>
          </div>
        </div>
      )}

      {step === 2 && (
        <div className="panel panel-done">
          <h2>{enabled.length ? `已为 ${enabled.length} 台音箱开启观察模式` : '小米账号已关联'}</h2>
          {enabled.length > 0 && <p className="lead">{enabled.map(deviceTitle).join('、')}</p>}
          {enabled.length > 0 && (
            <div className="say-first">
              <p>对音箱说一句，几秒内会出现在“此刻”。小爱照常回答。</p>
              <ul>{['播放周杰伦的晴天', '来一首后来', '播放陈奕迅的歌', '随便放一首歌'].map((x) => <li key={x}>“{x}”</li>)}</ul>
            </div>
          )}
          <div className="actions actions-center">
            <button className="btn btn-primary" onClick={onFinish}>完成</button>
          </div>
        </div>
      )}
    </section>
  );
}
