export interface MediaRange { from: number; to: number }

export function mediaRanges(ranges: TimeRanges): MediaRange[] {
  return Array.from({ length: ranges.length }, (_, index) => ({
    from: ranges.start(index) * 1000,
    to: ranges.end(index) * 1000,
  }));
}

export function sameRanges(a: MediaRange[], b: MediaRange[]): boolean {
  return a.length === b.length && a.every((range, index) => range.from === b[index].from && range.to === b[index].to);
}

export function seekPoint(ranges: MediaRange[], wanted: number): number | null {
  if (!Number.isFinite(wanted)) return null;
  let closest: number | null = null;
  for (const range of ranges) {
    const point = Math.min(range.to, Math.max(range.from, wanted));
    if (closest === null || Math.abs(point - wanted) < Math.abs(closest - wanted)) closest = point;
  }
  return closest;
}
