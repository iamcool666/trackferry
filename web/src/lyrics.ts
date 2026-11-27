export interface Line { at: number; text: string; /** When the line is over, if the LRC says so with an empty timed line. */ end?: number }

/** Reads LRC; lines without a time are dropped. */
export function parseLRC(s: string): Line[] {
  const out: Line[] = [];
  const rows = s.replace(/^\uFEFF/, '').split(/\r\n?|\n/);
  const offset = rows.map((row) => /^\s*\[offset:\s*([+-]?\d{1,7})\s*\]/i.exec(row)).filter((match) => match !== null).at(-1);
  const shift = Math.max(-60000, Math.min(60000, Number(offset?.[1] ?? 0) || 0));
  const ends: number[] = [];
  for (const raw of rows) {
    const times = [...raw.matchAll(/\[(\d{1,4}):([0-5]?\d)(?:[.:](\d{1,3}))?\]/g)];
    const text = raw.replace(/\[(?:\d+:[\d.:]+|[a-z]+:[^\]]*)\]|<\d+:[\d.:]+>/gi, '').trim();
    for (const t of times) {
      const minute = Number(t[1]);
      if (minute > 2147483647) continue;
      const at = minute * 60000 + Number(t[2]) * 1000 + Number((t[3] ?? '').padEnd(3, '0')) - shift;
      // An empty timed line marks where the singing stops, so an interlude is known, not guessed.
      if (!text) ends.push(at);
      else out.push({ at, text });
    }
  }
  const lines = out.sort((a, b) => a.at - b.at).reduce<Line[]>((lines, line) => {
    const previous = lines.at(-1);
    if (previous?.at !== line.at) lines.push(line);
    else if (!previous.text.split('\n').includes(line.text)) previous.text += `\n${line.text}`;
    return lines;
  }, []);
  ends.sort((a, b) => a - b);
  lines.forEach((l, k) => {
    const next = lines[k + 1]?.at ?? Infinity, end = ends.find((e) => e > l.at && e <= next);
    if (end !== undefined && end < next) l.end = end;
  });
  return lines;
}

/** How long line k is sung: to its marked end, else to the next line, never through a long interlude. */
export function spanOf(lines: Line[], k: number) {
  const l = lines[k];
  if (l.end !== undefined) return Math.max(400, l.end - l.at);
  const next = lines[k + 1]?.at ?? l.at + 4000;
  return Math.min(Math.max(400, next - l.at), 600 + l.text.length * 420);
}

/** The interlude at time `at`, if the song is in one: before the first line, or after a line marked as over. */
export function restAt(lines: Line[], k: number, at: number): { next: number; from: number; to: number } | null {
  const REST = 4000;
  if (k < 0) return lines[0] && lines[0].at >= REST ? { next: 0, from: 0, to: lines[0].at } : null;
  const l = lines[k], n = lines[k + 1];
  if (!n || l.end === undefined || at < l.end || n.at - l.end < REST) return null;
  return { next: k + 1, from: l.end, to: n.at };
}

export function lineAt(lines: Line[], at: number) {
  let left = 0, right = lines.length;
  while (left < right) {
    const middle = (left + right) >>> 1;
    if (lines[middle].at <= at) left = middle + 1;
    else right = middle;
  }
  return left - 1;
}

/** Words without times, as written. */
export function plainLyrics(s: string): Line[] {
  return s.split(/\r?\n/).map((l) => l.replace(/\[[^\]]*\]/g, '').trim()).filter(Boolean).map((text) => ({ at: 0, text }));
}
