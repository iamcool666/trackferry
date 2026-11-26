import { useCallback, useEffect, useRef, useState } from 'react';
import { api, ApiError, qrImageURL, type LinkResult, type QRStatus } from '../api';

/**
 * Xiaomi QR login. Shows the code, long-polls the server-side login, and reports once the
 * account is linked and the first device sync has finished.
 */
export function QRLink({ onLinked, compact = false }: { onLinked: (r: LinkResult) => void; compact?: boolean }) {
  const [qr, setQr] = useState<QRStatus | null>(null);
  const [error, setError] = useState<ApiError | null>(null);
  const [loading, setLoading] = useState(false);
  const [imgFailed, setImgFailed] = useState(false);
  const [backup, setBackup] = useState(false);
  const [now, setNow] = useState(Date.now());
  const started = useRef(false);
  const reported = useRef<string | null>(null);

  const start = useCallback(async () => {
    setLoading(true);
    setError(null);
    setImgFailed(false);
    setBackup(false);
    setNow(Date.now());
    try {
      setQr(await api.startQR());
    } catch (e) {
      setError(e as ApiError);
      setQr(null);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (started.current) return;
    started.current = true;
    // Resume a login that is still pending (e.g. after a page reload) instead of
    // invalidating it with a new code.
    api
      .qr()
      .then((s) => (s.state === 'pending' && new Date(s.expires_at).getTime() > Date.now() + 5000 ? setQr(s) : start()))
      .catch(() => start());
  }, [start]);

  const active = qr?.state === 'pending' || qr?.state === 'confirming';
  useEffect(() => {
    if (!active) return;
    const id = qr?.id;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      try {
        const s = await api.qr();
        if (!stopped) setQr((cur) => cur && cur.id === id ? (s.id === id ? s : { ...cur, state: 'expired' }) : cur);
      } catch (e) {
        // 404: the server no longer knows this login (e.g. it restarted). Anything else is
        // transient; keep polling.
        if (!stopped && (e as ApiError).status === 404) setQr((cur) => cur && cur.id === id ? { ...cur, state: 'expired' } : cur);
      }
      if (!stopped) timer = setTimeout(poll, 1500);
    };
    timer = setTimeout(poll, 1500);
    const tick = setInterval(() => setNow(Date.now()), 1000);
    return () => {
      stopped = true;
      clearTimeout(timer);
      clearInterval(tick);
    };
  }, [active, qr?.id]);

  useEffect(() => {
    if (qr?.state === 'linked' && reported.current !== qr.id) {
      reported.current = qr.id;
      onLinked(qr);
    }
  }, [qr, onLinked]);

  const remaining = qr ? Math.max(0, Math.round((new Date(qr.expires_at).getTime() - now) / 1000)) : 0;
  const expired = qr?.state === 'expired' || (qr?.state === 'pending' && remaining === 0);

  return (
    <div className={`qr ${compact ? 'qr-compact' : ''}`}>
      <div className="qr-frame" aria-live="polite">
        {loading && !qr && <div className="qr-placeholder"><span className="spinner" />正在向小米申请二维码…</div>}
        {error && (
          <div className="qr-placeholder qr-error">
            <strong>拿不到二维码</strong>
            <span>{error.message}</span>
            <button className="btn btn-small" onClick={start}>重试</button>
          </div>
        )}
        {qr && qr.state === 'pending' && !expired && !imgFailed && (
          <img className="qr-image" key={`${qr.id}/${backup}`} src={qrImageURL(qr.id, backup)} alt="小米登录二维码" width={240} height={240} onError={() => { if (backup) setImgFailed(true); else setBackup(true); }} />
        )}
        {qr && imgFailed && qr.state === 'pending' && !expired && (
          <div className="qr-placeholder qr-error">
            <strong>二维码图片加载失败</strong>
            <span>换一张再扫，也可以切换到账号密码登录。</span>
            <button className="btn btn-small" onClick={start} disabled={loading}>换一张</button>
          </div>
        )}
        {qr && expired && (
          <div className="qr-placeholder">
            <strong>二维码已过期</strong>
            <span>{qr.error ? '小米结束了这次登录。' : '超过有效期还没有确认。'}</span>
            <button className="btn btn-small btn-primary" onClick={start} disabled={loading}>刷新二维码</button>
          </div>
        )}
        {qr && qr.state === 'confirming' && (
          <div className="qr-placeholder"><span className="spinner" /><strong>已确认登录</strong><span>正在换取音箱权限并读取音箱列表…</span></div>
        )}
        {qr && qr.state === 'linked' && (
          <div className="qr-placeholder qr-ok"><span className="check">✓</span><strong>已关联</strong></div>
        )}
        {qr && qr.state === 'failed' && (
          <div className="qr-placeholder qr-error">
            <strong>扫码成功，但没能完成关联</strong>
            <span>{qr.error}</span>
            <button className="btn btn-small btn-primary" onClick={start}>重新扫码</button>
          </div>
        )}
      </div>

      <div className="qr-help">
        <ol className="qr-steps">
          <li>用手机打开 <b>米家 App</b>，点首页右上角的 <b>＋ → 扫一扫</b></li>
          <li>扫描这个二维码，在手机上点 <b>确认登录</b></li>
          <li>请使用 <b>绑定了小爱音箱的那个小米账号</b>。账号不对，登录会成功，但找不到音箱</li>
        </ol>
        <p className="qr-same-phone">正在用手机看这个页面？手机没法扫自己屏幕上的码，请在电脑或平板上打开这个页面再扫。</p>
        {qr?.state === 'pending' && !expired && <p className="qr-timer">
          二维码 {remaining} 秒后过期 · <button className="link" onClick={start} disabled={loading}>换一张</button>
          {!backup && <> · <button className="link" disabled={loading} onClick={() => { setImgFailed(false); setBackup(true); }}>换用备用码</button></>}
        </p>}
        {!compact && <p className="hint">登录凭据只存在 NAS 的 <code>data/private</code>。没有米家 App 时再用账号密码。</p>}
      </div>
    </div>
  );
}
