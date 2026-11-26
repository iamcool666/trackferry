/**
 * Sound presets, the same on this page and on the television. Five bands and, for 立体声, a wider
 * stage. The page only routes its sound through Web Audio once a preset other than 原声 is picked,
 * so a page that never picks one plays exactly as before.
 *
 * iOS stops Web Audio when the screen locks, which would silence music in the pocket: there the
 * presets are not offered (the same phones leave the volume to their buttons, see here.ts).
 * 小爱音箱 have no way to take a preset from us, so they are not offered there either.
 */

export type EQName = 'flat' | 'pop' | 'rock' | 'vocal' | 'classical' | 'electronic' | 'bass' | 'stereo';

/** Centres of the bands, in Hz: a low shelf, three peaks and a high shelf. */
export const EQ_BANDS = [60, 230, 910, 3600, 14000] as const;

export interface EQPreset { name: EQName; label: string; gains: readonly number[]; width: number }

export const EQ_PRESETS: readonly EQPreset[] = [
  { name: 'flat', label: '原声', gains: [0, 0, 0, 0, 0], width: 1 },
  { name: 'pop', label: '流行', gains: [-1, 2.5, 3.5, 1.5, -0.5], width: 1 },
  { name: 'rock', label: '摇滚', gains: [4, 2, -2, 2.5, 4], width: 1 },
  { name: 'vocal', label: '人声', gains: [-2, -1, 3, 3.5, 1], width: 1 },
  { name: 'classical', label: '古典', gains: [3, 1, 0, 1, 3], width: 1 },
  { name: 'electronic', label: '电子', gains: [4.5, 1.5, 0, 2, 4], width: 1 },
  { name: 'bass', label: '低音', gains: [6, 3.5, 0, 0, 0], width: 1 },
  { name: 'stereo', label: '立体声', gains: [1, 0, 0, 0, 1.5], width: 1.6 },
];

export const eqPreset = (name: string): EQPreset => EQ_PRESETS.find((p) => p.name === name) ?? EQ_PRESETS[0];

/** How far everything is turned down first, in dB, so that boosted bands do not clip. */
export const eqPreamp = (p: EQPreset) => -Math.max(0, ...p.gains) - (p.width > 1 ? 1.5 : 0);

/** The mix of a stereo widener: L' = a·L + b·R, R' = a·R + b·L. width 1 leaves it as it is. */
export const eqWidth = (width: number) => ({ a: (1 + width) / 2, b: (1 - width) / 2 });
