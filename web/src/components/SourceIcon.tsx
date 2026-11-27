import { useId } from 'react';
import type { LayerName } from '../api';
import feiniu from '../assets/sources/feiniu.jpg';
import kuwo from '../assets/sources/kuwo.jpg';
import kugou from '../assets/sources/kugou.jpg';
import migu from '../assets/sources/migu.jpg';
import netease from '../assets/sources/netease.jpg';
import xiaoai from '../assets/sources/xiaoai.jpg';

const src: Record<Exclude<LayerName, 'library'> | 'feiniu', string> = { feiniu, kuwo, kugou, migu, netease, xiaomi: xiaoai };

// The library may be 飞牛音乐 or a music folder, so it has a mark of its own; 飞牛's logo is only for 飞牛 itself.
// Drawn like the brand icons beside it: a full colour tile with a solid white glyph.
const own = {
  library: { from: '#6fc3d6', to: '#2c6a86', glyph: <><rect x="6.6" y="6.4" width="2.6" height="11.2" rx=".7" /><rect x="10.3" y="6.4" width="2.6" height="11.2" rx=".7" /><path d="m14.1 7.6 2.3-.6a.6.6 0 0 1 .73.43l2.4 9.2a.6.6 0 0 1-.43.73l-2.3.6a.6.6 0 0 1-.73-.43l-2.4-9.2a.6.6 0 0 1 .43-.73z" /></> },
  folder: { from: '#f7cf72', to: '#e08a3c', glyph: <path fillRule="evenodd" d="M5 8.2A1.7 1.7 0 0 1 6.7 6.5h3.1c.45 0 .88.18 1.2.5l1 1h5.3A1.7 1.7 0 0 1 19 9.7v6.6a1.7 1.7 0 0 1-1.7 1.7H6.7A1.7 1.7 0 0 1 5 16.3zm8.4 2.3v3.05a1.55 1.55 0 1 0 1.1 1.48V12.1l1.5.45v-1.1z" /> },
};

export function SourceIcon({ name }: { name: LayerName | 'feiniu' | 'folder' }) {
  const id = 'src' + useId().replace(/[^\w-]/g, '');
  if (name === 'library' || name === 'folder') {
    const { from, to, glyph } = own[name];
    return <svg className="src-icon src-own" viewBox="0 0 24 24" aria-hidden="true">
      <defs><linearGradient id={id} x1="0" y1="0" x2="0" y2="1"><stop offset="0" stopColor={from} /><stop offset="1" stopColor={to} /></linearGradient></defs>
      <rect width="24" height="24" fill={`url(#${id})`} />
      <g fill="#fff">{glyph}</g>
    </svg>;
  }
  return <img className="src-icon" src={src[name]} alt="" draggable={false} />;
}
