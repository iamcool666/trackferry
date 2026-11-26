/**
 * Which output this page shows and controls, the same on 此刻, the bar below, 播放到 and 流转.
 *
 * - A pick sticks: it stays while that output is there, whatever plays elsewhere.
 * - Playing or moving music from this page picks the output it went to.
 * - While the pick is away (a television that went off, a speaker taken out), a playing output
 *   is shown instead, else this device; the pick comes back with its output.
 * - A television that comes on is picked by itself only while this device is the pick and plays nothing.
 * - When more outputs play than the one shown, the page says how many more.
 */

/** This device's id among the outputs. */
export const HERE_ID = 'here';

export interface OutputNow {
  id: string;
  /** It has a song, playing or paused. */
  song: boolean;
  playing: boolean;
}

/** The output to show: the pick while it is there, else one that plays, else this device. */
export function shownOutput(pick: string, available: ReadonlySet<string>, outputs: readonly OutputNow[]): string {
  if (pick && available.has(pick)) return pick;
  return outputs.find((o) => o.playing && o.id !== pick && available.has(o.id))?.id ?? HERE_ID;
}

/** The outputs that play besides the one shown. */
export const alsoPlaying = (shown: string, outputs: readonly OutputNow[]) => outputs.filter((o) => o.playing && o.id !== shown);

/**
 * What the small bar on the other pages shows: the shown output when it has a song; otherwise one
 * that plays, then one that has a song. Tapping it picks it, so the bar and 此刻 agree again.
 */
export function barOutput(shown: string, outputs: readonly OutputNow[]): OutputNow | undefined {
  return outputs.find((o) => o.id === shown && o.song) ?? outputs.find((o) => o.playing) ?? outputs.find((o) => o.song);
}

/** Whether a television that just came on is picked by itself. */
export const takesNewTV = (pick: string, outputs: readonly OutputNow[]) =>
  (pick === HERE_ID || pick === '') && !outputs.some((o) => o.id === HERE_ID && o.playing);
