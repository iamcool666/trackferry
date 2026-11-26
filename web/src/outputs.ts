import type { Device } from './api';
import { deviceTitle } from './copy';
import { choose, connected, HERE, lookAt, useChosen, usePick } from './ear';
import { hereName, hereSong, useHere } from './here';
import { onDeck, songOf, useLive } from './live';
import { alsoPlaying, barOutput, shownOutput, type OutputNow } from './output-rule';
import { tvTarget, useTVOutputs } from './tv';

export interface Output extends OutputNow {
  name: string;
  /** What it has on, for the bar. */
  title?: string;
  artist?: string;
  cover?: string;
  /** It is being sent there or is loading. */
  waiting: boolean;
}

/** Every output and what it does now: this device, the speakers, the televisions. */
export function useOutputs(devices: Device[]) {
  const { flows } = useLive();
  const h = useHere();
  const televisions = useTVOutputs();
  const mine = hereSong(h);
  const out: Output[] = [
    { id: HERE, name: hereName, song: !!mine, playing: !!mine && h.on, waiting: !!mine && h.waiting, title: mine?.title, artist: mine?.artist, cover: mine?.cover },
    ...connected(devices).map((x) => {
      const f = onDeck(flows, x.device_id), song = f ? songOf(f) : null;
      return { id: x.device_id, name: deviceTitle(x), song: !!song, playing: !!f && f.player?.phase !== 'paused', waiting: !!f && !f.player?.phase, title: song?.title, artist: song?.artist, cover: song?.cover };
    }),
    ...televisions.filter((x) => x.online).map((x) => {
      const c = x.playback.current as { title?: string; artist?: string; cover?: string };
      return { id: tvTarget(x.id), name: x.name, song: !!c.title, playing: !!c.title && x.playback.phase === 'playing', waiting: x.playback.phase === 'buffering', title: c.title, artist: c.artist, cover: c.cover };
    }),
  ];
  return out;
}

/**
 * The output this page shows and plays on, the same everywhere (see output-rule.ts), and how to
 * pick another one.
 */
export function useTarget(devices: Device[]): [string, (id: string) => void] {
  const pick = usePick();
  const outputs = useOutputs(devices);
  return [shownOutput(pick, new Set(outputs.map((o) => o.id)), outputs), lookAt];
}

/** The shown output, the outputs that play besides it, and what the small bar shows. */
export function useStage(devices: Device[]) {
  const pick = usePick();
  const outputs = useOutputs(devices);
  const shown = shownOutput(pick, new Set(outputs.map((o) => o.id)), outputs);
  const bar = barOutput(shown, outputs) as Output | undefined;
  return { shown, outputs, also: alsoPlaying(shown, outputs) as Output[], bar, pick: lookAt };
}

/** The speaker 此刻 and 流转 look at. 流转 may look at all of them (''); playback never does. */
export function useEar(devices: Device[], playback = false): [string, (id: string) => void] {
  const id = useChosen();
  const target = useTarget(devices);
  if (playback) return target;
  return [id === HERE || id.startsWith('tv:') || connected(devices).some((d) => d.device_id === id) ? id : '', choose];
}
