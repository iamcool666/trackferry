export interface Line { at: number; text: string }

/** Reads LRC; lines without a time are dropped. */
export function parseLRC(s: string): Line[] {
  const out: Line[] = [];
  const rows = s.replace(/^\uFEFF/, '').split(/\r\n?|\n/);
  const offset = rows.map((row) => /^\s*\[offset:\s*([+-]?\d{1,7})\s*\]/i.exec(row)).filter((match) => match !== null).at(-1);
  const shift = Number(offset?.[1] ?? 0) || 0;
  for (const raw of rows) {
    const times = [...raw.matchAll(/\[(\d+):([0-5]?\d)(?:[.:](\d{1,3}))?\]/g)];
    const text = raw.replace(/\[(?:\d+:[\d.:]+|[a-z]+:[^\]]*)\]|<\d+:[\d.:]+>/gi, '').trim();
    if (!text) continue;
    for (const t of times) {
      const minute = Number(t[1]);
      if (minute <= 2147483647) out.push({ at: minute * 60000 + Number(t[2]) * 1000 + Number((t[3] ?? '').padEnd(3, '0')) - shift, text });
    }
  }
  return out.sort((a, b) => a.at - b.at).reduce<Line[]>((lines, line) => {
    const previous = lines.at(-1);
    if (previous?.at !== line.at) lines.push(line);
    else if (!previous.text.split('\n').includes(line.text)) previous.text += `\n${line.text}`;
    return lines;
  }, []);
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
