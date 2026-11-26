import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { api, ApiError, type Controls, type Device, type Hush, type Mode, type PlayMode, type Press, type WatchStatus } from '../api';
import { here, hereName, useHere } from '../here';
import { onDeck, useLive } from '../live';
import { Deck, playModes } from './Deck';
import { HereQueue, SpeakerQueue } from './Queue';
import { useToast } from './Toast';
import { Sheet } from './Sheet';
import { EQ_PRESETS, eqPreset } from '../eq';
import { Pick } from './Pick';
import { deviceSubtitle, deviceTitle, isOnline, modeHint, modeLabel } from '../copy';

const modes: Mode[] = ['off', 'observe', 'takeover'];

export const watchText: Record<WatchStatus['state'], string> = {
  starting: '正在连接', listening: '正在听', error: '读取失败', paused: '已暂停', unsupported: '这个型号暂不支持',
};

function ModeSwitch({ device, onChange }: { device: Device; onChange: (d: Device) => void }) {
  const [busy, setBusy] = useState<Mode | null>(null);
  const [error, setError] = useState<string | null>(null);
  const set = async (mode: Mode) => {
    if (mode === device.mode || busy) return;
    setBusy(mode);
    setError(null);
    try {
      onChange(await api.setMode(device.device_id, mode));
    } catch (e) {
      setError((e as ApiError).message);
    } finally {
      setBusy(null);
    }
  };
  return (
    <div className="mode">
      <div className="segmented" role="radiogroup" aria-label={`${deviceTitle(device)} 的模式`}>
        {modes.map((m) => {
          const locked = m === 'takeover' && !device.takeover_allowed && device.mode !== 'takeover';
          return (
            <button
              key={m}
              role="radio"
              aria-checked={device.mode === m}
              className={`seg seg-${m} ${device.mode === m ? 'on' : ''}`}
              disabled={locked || !device.in_account}
              title={locked ? '未在 config.yaml 的 takeover_speakers 中放行' : modeHint[m]}
              onClick={() => set(m)}
            >
              {busy === m && <span className="spinner spinner-tiny" />}
              {modeLabel[m]}
              {locked && <svg className="lock" viewBox="0 0 10 12" width="9" height="11" aria-label="未放行"><path d="M2.5 5V3.5a2.5 2.5 0 015 0V5M1 5h8v6H1z" fill="none" stroke="currentColor" strokeWidth="1.5" /></svg>}
            </button>
          );
        })}
      </div>
      {error && <p className="field-error">{error}</p>}
    </div>
  );
}

const hushes: { value: Hush; label: string; says: string }[] = [
  { value: 'normal', label: '普通', says: '音量调到零并打断，出歌最快' },
  { value: 'strong', label: '加强', says: '再加一道米家静音和暂停，适合压不住的型号' },
  { value: 'wait', label: '等小爱说完', says: '一直静音，估计小爱说完了再放歌，出歌慢一些' },
];

function HushPick({ device, onChange }: { device: Device; onChange: (d: Device) => void }) {
  const [error, setError] = useState<string | null>(null);
  const set = async (h: Hush) => {
    try { onChange(await api.setHush(device.device_id, h)); setError(null); } catch (e) { setError((e as ApiError).message); }
  };
  return (
    <div className="hush">
      <span>压住小爱的回答</span>
      <Pick label={`${deviceTitle(device)}压住小爱回答的方式`} value={device.hush} choices={hushes.filter((h) => h.value !== 'strong' || device.can_hush)} onPick={set} />
      <small>{hushes.find((h) => h.value === device.hush)?.says}</small>
      {error && <p className="field-error" role="alert">{error}</p>}
    </div>
  );
}

/** The speaker itself; its ring shows what it is doing. */
function Body({ mode, state }: { mode: Mode; state?: WatchStatus['state'] }) {
  const on = mode !== 'off' && state === 'listening';
  return (
    <svg className={`body body-${mode} ${on ? 'body-on' : ''}`} viewBox="0 0 120 170" aria-hidden="true">
      <defs>
        <linearGradient id="shell" x1="0" x2="1"><stop stopColor="#0b0e11" /><stop offset=".28" stopColor="#4a535b" /><stop offset=".55" stopColor="#262c32" /><stop offset="1" stopColor="#07090b" /></linearGradient>
        <pattern id="mesh" width="5" height="5" patternUnits="userSpaceOnUse" patternTransform="rotate(45)"><circle cx="2.5" cy="2.5" r="1.1" fill="#000" fillOpacity=".5" /></pattern>
      </defs>
      <ellipse cx="60" cy="156" rx="46" ry="9" fill="#000" fillOpacity=".45" />
      <path d="M14 26v118c0 9 20 16 46 16s46-7 46-16V26z" fill="url(#shell)" />
      <path d="M14 40v104c0 9 20 16 46 16s46-7 46-16V40c-9 8-27 12-46 12s-37-4-46-12z" fill="url(#mesh)" />
      <ellipse cx="60" cy="26" rx="46" ry="15" fill="#15191d" />
      <ellipse className="ring" cx="60" cy="26" rx="36" ry="11" fill="none" strokeWidth="3.5" />
    </svg>
  );
}

export function SpeakerRow({ device, watch, onChange }: { device: Device; watch?: WatchStatus; onChange: (d: Device) => void }) {
  const [open, setOpen] = useState(() => innerWidth > 760 || device.mode === 'off' || watch?.state === 'error');
  return (
    <li className={`speaker mode-${device.mode} ${device.in_account ? '' : 'speaker-gone'}`}>
      <Body mode={device.in_account ? device.mode : 'off'} state={watch?.state} />
      <div className="speaker-info">
        <h2>{deviceTitle(device)}</h2>
        <p className="speaker-meta">
          {device.in_account
            ? <span className={isOnline(device) ? 'on-dot' : 'off-dot'}>{isOnline(device) ? '在线' : '离线'}</span>
            : <span className="off-dot">已不在账号中</span>}
          <span>{deviceSubtitle(device)}</span>
        </p>
        {device.in_account && device.mode !== 'off' && <Buttons device={device} inlineVolume />}
        <details className="spk-set" open={open} onToggle={(e) => setOpen(e.currentTarget.open)}>
          <summary><span>{device.in_account ? modeLabel[device.mode] : '设置'}</span>{device.in_account && device.mode === 'takeover' && <small>压住小爱：{hushes.find((h) => h.value === device.hush)?.label}</small>}<svg viewBox="0 0 12 8" width="11" height="8" aria-hidden="true"><path d="M1 1.5l5 5 5-5" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" /></svg></summary>
          <ModeSwitch device={device} onChange={onChange} />
          {device.in_account && device.mode === 'takeover' && <HushPick device={device} onChange={onChange} />}
          <p className="speaker-hint">
            {device.in_account ? modeHint[device.mode] : '设置会保留，重新绑定后恢复。'}
            {watch && device.mode !== 'off' && <small className={watch.state === 'error' ? 'bad' : ''}>{watchText[watch.state]}{watch.error ? `：${watch.error}` : watch.recorded > 0 ? `，启动后听到 ${watch.recorded} 次点歌` : ''}</small>}
          </p>
        </details>
      </div>
    </li>
  );
}

/** The hands of a speaker. foot stands left of the second line; without it, what list is on. */
export function Buttons({ device, foot, tail, inlineVolume }: { device: Device; foot?: ReactNode; tail?: ReactNode; inlineVolume?: boolean }) {
  const id = device.device_id;
  const name = deviceTitle(device);
  const flow = onDeck(useLive().flows, id);
  const phase = flow?.player?.phase;
  const [meant, setMeant] = useState<'playing' | 'paused' | 'idle' | null>(null); // until the speaker is heard from
  const [now, setNow] = useState<Controls | null>(null);
  const [lost, setLost] = useState<string | null>(null); // why the speaker cannot be read
  const [busy, setBusy] = useState<Press | null>(null);
  const [open, setOpen] = useState(false);
  const [toast, say] = useToast();
  const again = useRef<ReturnType<typeof setTimeout>>(undefined);
  const alive = useRef(true);
  const load = useCallback(async () => {
    try { const s = await api.controls(id); if (alive.current) { setNow(s); setLost(null); } } catch (e) { if (alive.current) setLost((e as ApiError).message); }
  }, [id]);
  useEffect(() => {
    alive.current = true;
    load();
    const t = setInterval(() => { if (!document.hidden) load(); }, 8000);
    return () => { alive.current = false; clearInterval(t); clearTimeout(again.current); };
  }, [load]);
  useEffect(() => { setMeant(null); load(); }, [phase, flow?.id, load]);
  useEffect(() => {
    if (!meant) return;
    const t = setTimeout(() => setMeant(null), 5000);
    return () => clearTimeout(t);
  }, [meant]);
  const state = meant ?? (flow ? (phase === 'paused' ? 'paused' : 'playing') : now?.state) ?? 'idle';
  const press = async (action: Press, set: { volume?: number; mode?: PlayMode } = {}) => {
    setBusy(action);
    const to = ({ play: 'playing', pause: 'paused', stop: 'idle' } as const)[action as 'play'];
    if (to) { setMeant(to); setNow((n) => n && { ...n, state: to }); }
    if (set.volume !== undefined) setNow((n) => n && { ...n, ...set });
    try {
      await api.press(id, action, set);
      if (action === 'wake') say({ tone: 'ok', text: '已唤醒，直接说话就行' });
      if (set.mode) say({ tone: 'ok', text: playModes[set.mode].name });
      setNow((n) => n && { ...n, ...set });
    } catch (e) {
      setMeant(null);
      say({ tone: 'bad', text: (e as ApiError).message });
    } finally {
      setBusy((b) => (b === action ? null : b));
      clearTimeout(again.current);
      again.current = setTimeout(load, 900); // what was pressed takes a moment
    }
  };
  const q = now?.queue;
  return (
    <>
      <Deck name={name} state={state} waiting={!!flow && !phase && !meant} busy={busy} alone={now !== null && !q} mode={now?.mode ?? ''} count={(q?.total ?? 0) + (q?.next?.length ?? 0)}
        onMode={(mode) => press('mode', { mode })} onToggle={() => press(state === 'playing' ? 'pause' : 'play')} onPrevious={() => press('previous')} onNext={() => press('next')} onList={() => setOpen(true)}
        vol={{ value: now?.volume ?? null, set: (volume) => press('volume', { volume }) }} inlineVolume={inlineVolume} onWake={device.can_wake ? () => press('wake') : undefined}
        foot={foot ?? (q?.list ? <span className="foot-list">{q.list}</span> : null)} tail={tail} note={lost && <p className="ctl-said" role="status">暂时读不到音箱，会自动重试：{lost}</p>} />
      {open && <SpeakerQueue device={device} onStop={() => press('stop')} onClose={() => setOpen(false)} />}
      {toast}
    </>
  );
}

/** The hands of this device. */
export function HereButtons({ foot, tail }: { foot?: ReactNode; tail?: ReactNode }) {
  const h = useHere();
  const [open, setOpen] = useState(false);
  const [sound, setSound] = useState(false);
  const [toast, say] = useToast();
  const eq = h.canEQ && <button type="button" className={`foot-btn foot-eq${h.eq !== 'flat' ? ' on' : ''}`} onClick={() => setSound(true)} aria-haspopup="dialog" aria-label={`音效：${eqPreset(h.eq).label}`} title="音效">
    <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" aria-hidden="true"><path d="M5 4v16M12 4v16M19 4v16" opacity=".35" /><path d="M3 15h4M10 8h4M17 13h4" /></svg>
  </button>;
  return (
    <>
      <Deck name={hereName} state={h.on ? 'playing' : 'paused'} waiting={h.waiting} off={!h.songs.length} alone={h.songs.length < 2} mode={h.mode} count={h.songs.length}
        onMode={(m) => { here.mode(m); say({ tone: 'ok', text: playModes[m].name }); }} onToggle={here.toggle} onPrevious={here.previous} onNext={here.next} onList={() => setOpen(true)}
        vol={h.canVolume ? { value: h.volume, live: here.volume, set: here.volume } : null} foot={foot} tail={<>{eq}{tail}</>} />
      {open && <HereQueue onClose={() => setOpen(false)} />}
      {sound && <Sheet title="音效" onClose={() => setSound(false)}>
        <div className="picks eq-picks">{EQ_PRESETS.map((p) => <button type="button" key={p.name} aria-pressed={h.eq === p.name} onClick={() => { if (!here.eq(p.name)) say({ tone: 'bad', text: '这个浏览器不能调音效' }); }}>
          <i><svg viewBox="0 0 40 24" width="34" height="20" aria-hidden="true">{p.gains.map((g, n) => <rect key={n} x={2 + n * 8} y={12 - Math.max(0, g) * 1.6} width="4" height={Math.max(1.5, Math.abs(g) * 1.6)} rx="2" fill="currentColor" opacity={g === 0 ? .35 : 1} />)}</svg></i>
          <span><b>{p.label}</b>{p.name === 'stereo' && <small>声场更宽，耳机里最明显</small>}{p.name === 'flat' && <small>不加处理</small>}</span>
        </button>)}</div>
        <p className="eq-note">只作用在这台设备上。小爱音箱不接受外来音效；电视在电视上选。</p>
      </Sheet>}
      {toast}
    </>
  );
}
