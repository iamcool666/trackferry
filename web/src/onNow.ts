import { useMemo } from 'react';
import type { Device } from './api';
import { connected } from './ear';
import { hereSong, useHere } from './here';
import { onDeck, songOf, useLive } from './live';
import { useTVOutputs } from './tv';

/** Which library songs are current on one of the outputs, and whether each is playing. */
export function useOnNow(devices: Device[]): Map<number, boolean> {
  const { flows } = useLive();
  const h = useHere();
  const televisions = useTVOutputs();
  return useMemo(() => {
    const on = new Map<number, boolean>();
    const mine = hereSong(h);
    if (mine?.track_id) on.set(mine.track_id, h.on);
    for (const d of connected(devices)) {
      const f = onDeck(flows, d.device_id);
      const id = f && songOf(f)?.track_id;
      if (id) on.set(id, on.get(id) || f.player?.phase !== 'paused');
    }
    for (const tv of televisions) {
      const { current, phase } = tv.playback;
      if (tv.online && current.id) on.set(current.id, on.get(current.id) || phase === 'playing');
    }
    return on;
  }, [flows, h, devices, televisions]);
}
