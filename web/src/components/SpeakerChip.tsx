import { useState } from 'react';
import type { Device } from '../api';
import { deviceTitle } from '../copy';
import { connected, HERE } from '../ear';
import { handheld, hereName, hereSong, useHere } from '../here';
import { onDeck, onSpeaker, phaseText, songOf, useLive } from '../live';
import { Cover } from './Cover';
import { Sheet } from './Sheet';
import { watchText } from './Speaker';
import { tvTarget, tvID, tvPhase, useTVOutputs } from '../tv';

export const speakerIcon = <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true"><rect x="6" y="2.5" width="12" height="19" rx="5" fill="none" stroke="currentColor" strokeWidth="1.6" /><path d="M9.4 6.5h5.2" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" /><circle cx="12" cy="14.5" r="2.6" fill="none" stroke="currentColor" strokeWidth="1.6" /></svg>;
export const hereIcon = handheld
  ? <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" aria-hidden="true"><rect x="6.5" y="2.5" width="11" height="19" rx="2.8" /><path d="M10.5 18.6h3" /></svg>
  : <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" aria-hidden="true"><rect x="4.5" y="5" width="15" height="10" rx="1.6" /><path d="M2.5 18.5h19" /></svg>;
export const tvIcon = <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" aria-hidden="true"><rect x="3" y="4" width="18" height="13" rx="2" /><path d="M8 21h8m-4-4v4" /></svg>;
const allIcon = <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true"><path d="M5 15V9M10 19V5M15 16V8M20 13.5v-3" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" /></svg>;

/** Which speaker it is about, in a word; a tap changes it. all: "" may be chosen too. */
export function SpeakerChip({ devices, value, onChange, all = false, title = '看哪台音箱', bare = '', showTitle = false, televisions = false, inline = false }: { devices: Device[]; value: string; onChange: (id: string) => void; all?: boolean; title?: string; /** A word without a button around it; what it says when none is chosen. */ bare?: string; showTitle?: boolean; televisions?: boolean; inline?: boolean }) {
  const { flows, pulses } = useLive();
  const [open, setOpen] = useState(false);
  const list = connected(devices);
  const h = useHere(), mine = hereSong(h), local = value === HERE;
  const candidates = useTVOutputs();
  const tvs = televisions ? candidates.filter((item) => item.online) : [];
  const tv = tvs.find((item) => item.id === tvID(value));
  const d = list.find((x) => x.device_id === value);
  const on = (id: string) => onSpeaker(flows, id)?.player?.phase === 'playing';
  const busy = local ? h.on : tv ? tv.playback.phase === 'playing' : d ? on(d.device_id) : list.some((x) => on(x.device_id));
  const only = list.length === 1 && !value ? list[0] : null;
  const name = local ? hereName : tv ? tv.name : d ? deviceTitle(d) : only ? deviceTitle(only) : bare || '全部音箱';
  const pick = (id: string) => { onChange(id); setOpen(false); };
  const options = (
    <div className="picks">
      <button type="button" aria-pressed={local} onClick={() => pick(HERE)}>
        <i>{mine ? <Cover cover={mine.cover} title={mine.title} artist={mine.artist} /> : hereIcon}</i>
        <span><b>{hereName}</b>{mine && <small>{`${h.on ? '' : '停着　'}${mine.title}`}</small>}</span>
        {h.on && <em className="eq" aria-hidden="true"><u /><u /><u /></em>}
      </button>
      {all && list.length > 1 && <button type="button" aria-pressed={!value} onClick={() => pick('')}><i>{allIcon}</i><span><b>全部音箱</b><small>{list.length} 台一起看</small></span></button>}
      {tvs.map((item) => <button type="button" key={item.id} aria-pressed={tv?.id === item.id} onClick={() => pick(tvTarget(item.id))}>
        <i>{tvIcon}</i><span><b>{item.name}</b><small>{item.playback.current.title || tvPhase[item.playback.phase]}</small></span>
      </button>)}
      {list.map((x) => {
        const f = onDeck(flows, x.device_id);
        const song = f && songOf(f);
        const p = pulses[x.device_id];
        return (
          <button key={x.device_id} type="button" aria-pressed={value === x.device_id || only === x} onClick={() => pick(x.device_id)}>
            <i>{song ? <Cover cover={song.cover} title={song.title ?? ''} artist={song.artist} /> : speakerIcon}</i>
            <span><b>{deviceTitle(x)}</b><small>{song ? `${f?.player?.phase && f.player.phase !== 'playing' ? `${phaseText[f.player.phase]}　` : ''}${song.title}` : p ? watchText[p.state] : '没有在听'}</small></span>
            {on(x.device_id) && <em className="eq" aria-hidden="true"><u /><u /><u /></em>}
          </button>
        );
      })}
    </div>
  );
  return (
    <>
      <button type="button" className={bare ? 'chip chip-bare' : 'chip glass'} onClick={() => setOpen((value) => !value)} aria-haspopup={inline ? undefined : 'dialog'} aria-expanded={open} aria-label={`${title}：${name}`}>
        {busy && !showTitle ? <em className="eq" aria-hidden="true"><u /><u /><u /></em> : local ? hereIcon : tv ? tvIcon : d || only ? speakerIcon : allIcon}
        {showTitle && <small className="chip-purpose">{title}</small>}
        <b>{name}</b>
        <svg viewBox="0 0 12 8" width="11" height="8" aria-hidden="true"><path d="M1 1.5l5 5 5-5" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" /></svg>
      </button>
      {open && inline && <div className="output-inline" role="group" aria-label={title}>{options}</div>}
      {open && !inline && (
        <Sheet title={title} onClose={() => setOpen(false)}>
          {options}
        </Sheet>
      )}
    </>
  );
}
