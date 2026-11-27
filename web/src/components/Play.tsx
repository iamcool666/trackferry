import { useState } from 'react';
import type { Device } from '../api';
import { deviceTitle } from '../copy';
import { connected, HERE } from '../ear';
import { useTarget } from '../outputs';
import { hereName } from '../here';
import { playOn, type Playback } from '../playback';
import { SpeakerChip } from './SpeakerChip';
import { useToast } from './Toast';
import { activeTV } from '../tv';
import { tookNote } from '../took';

export function PlaybackTarget({ devices }: { devices: Device[] }) {
  const [target, setTarget] = useTarget(devices);
  return <SpeakerChip devices={devices} value={target} onChange={setTarget} title="播放到" showTitle televisions />;
}

export function Play({ what, title, devices, label }: { what: Playback; title: string; devices: Device[]; label?: string }) {
  const [target] = useTarget(devices);
  const [busy, setBusy] = useState(false);
  const [toast, say] = useToast();
  const speaker = connected(devices).find((d) => d.device_id === target);
  const tv = activeTV(target);
  const output = tv?.name ?? (speaker ? deviceTitle(speaker) : hereName);
  const play = async () => {
    setBusy(true);
    try {
      if (await playOn(target, what) && target !== HERE) say({ tone: 'ok', text: tv ? `${output} 已确认播放` : `已送往${output}${tookNote()}` });
    } catch (error) {
      say({ tone: 'bad', text: (error as Error).message });
    } finally {
      setBusy(false);
    }
  };
  return <span className={`play ${label ? 'play-label' : ''}`}>
    <button type="button" className={label ? 'btn btn-small' : 'play-btn'} disabled={busy} aria-label={`播放${title}，${output}`} title={`播放到${output}`} onClick={play}>
      {busy ? <span className="spinner spinner-tiny" /> : <svg viewBox="0 0 12 14" width="11" height="13" aria-hidden="true"><path d="M1 1l10 6-10 6z" fill="currentColor" /></svg>}
      {label}
    </button>
    {toast}
  </span>;
}
