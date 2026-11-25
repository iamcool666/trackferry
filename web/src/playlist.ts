import type { PlayMode } from './api';

/** Songs in the order they are played; n is where a song stood before any shuffling. */
export interface Slot { key: string; n: number }
export interface List<T extends Slot> { songs: T[]; i: number }

export function shuffle<T>(list: T[], random = Math.random): T[] {
  const out = [...list];
  for (let k = out.length - 1; k > 0; k--) {
    const j = Math.floor(random() * (k + 1));
    [out[k], out[j]] = [out[j], out[k]];
  }
  return out;
}

/** The same songs, ordered for a mode; the one that is on stays on. */
export function arrange<T extends Slot>({ songs, i }: List<T>, mode: PlayMode, random = Math.random): List<T> {
  const on = songs[i];
  if (!on) return { songs, i };
  if (mode === 'shuffle') return { songs: [on, ...shuffle(songs.filter((x) => x !== on), random)], i: 0 };
  const back = [...songs].sort((a, b) => a.n - b.n);
  return { songs: back, i: back.indexOf(on) };
}

/** Puts a song right after the one that is on. One that is there already moves. */
export function putNext<T extends Slot>({ songs, i }: List<T>, song: Omit<T, 'n'>): List<T> {
  const on = songs[i];
  if (!on) return { songs: [{ ...song, n: 0 } as T], i: 0 };
  if (on.key === song.key) return { songs, i };
  const rest = songs.filter((x) => x.key !== song.key);
  const at = rest.indexOf(on);
  const after = Math.min(...rest.filter((x) => x.n > on.n).map((x) => x.n), on.n + 1);
  return { songs: [...rest.slice(0, at + 1), { ...song, n: (on.n + after) / 2 } as T, ...rest.slice(at + 1)], i: at };
}

/** Takes a song off. If it was on, the one after it is. */
export function takeOff<T extends Slot>({ songs, i }: List<T>, key: string): List<T> {
  const at = songs.findIndex((x) => x.key === key);
  if (at < 0) return { songs, i };
  const rest = songs.filter((_, k) => k !== at);
  return { songs: rest, i: at < i ? i - 1 : i >= rest.length ? 0 : i };
}

/** Where to go from here; null is the end. by: pressed, not run out. */
export function step(count: number, i: number, dir: 1 | -1, mode: PlayMode, by: boolean): number | null {
  if (!count) return null;
  if (!by && mode === 'one') return i;
  if (count === 1) return by ? 0 : null;
  return (i + dir + count) % count;
}
