import { test } from 'node:test';
import assert from 'node:assert/strict';
import { art } from '../src/stage/art.ts';
import { singer } from '../src/copy.ts';
import { lineAt, parseLRC, plainLyrics, restAt, spanOf } from '../src/lyrics.ts';
import { seekPoint } from '../src/media-clock.ts';

test('没有封面的歌：同一首永远同一张，不同的歌不一样，歌名不会进到图里', () => {
  assert.equal(art('知我', 'download'), art('知我', 'download'));
  assert.notEqual(art('知我'), art('知己'));
  const svg = decodeURIComponent(art('<b>"&'));
  assert.ok(!svg.includes('<b>') && !/NaN|undefined/.test(svg));
});

test('目录名不当歌手', () => {
  for (const n of ['download', 'Unknown Artist', '未知歌手', ' ']) assert.equal(singer(n), '');
  for (const n of ['周杰伦', 'Music Man', '程响']) assert.equal(singer(n), n);
});

test('没有时间的歌词也能看', () => {
  assert.equal(parseLRC('第一句\n第二句').length, 0);
  assert.deepEqual(plainLyrics('[ti:歌]\n第一句\n\n第二句').map((l) => l.text), ['第一句', '第二句']);
});

test('LRC 全局偏移放在结尾也生效；同时间的翻译一起高亮', () => {
  const lines = parseLRC('\uFEFF[00:01.25][00:02.5]<00:01.25>原文\r\n[00:01:250]译文\r\n[00:02.5]原文\r\n[offset:+150]');
  assert.deepEqual(lines, [{ at: 1100, text: '原文\n译文' }, { at: 2350, text: '原文' }]);
  assert.deepEqual([1099, 1100, 2349, 2350, 1200].map((time) => lineAt(lines, time)), [-1, 0, 0, 1, 0]);
});

test('定位只落在实际可定位范围内，包括中间断开的音频', () => {
  const ranges = [{ from: 2000, to: 5000 }, { from: 9000, to: 12000 }];
  assert.deepEqual([0, 2750, 6000, 8500, 18000].map((time) => seekPoint(ranges, time)), [2000, 2750, 5000, 9000, 12000]);
  assert.equal(seekPoint([], 5000), null);
  assert.equal(seekPoint(ranges, NaN), null);
});

test('an interlude is known from an empty timed line, never guessed', () => {
  const lines = parseLRC('[00:06.00]第一句\n[00:09.00]\n[00:20.00]第二句\n[00:22.00]第三句');
  assert.deepEqual(lines.map((l) => l.text), ['第一句', '第二句', '第三句']);
  assert.equal(lines[0].end, 9000);
  assert.equal(lines[1].end, undefined);
  assert.equal(spanOf(lines, 0), 3000);
  assert.deepEqual(restAt(lines, -1, 1000), { next: 0, from: 0, to: 6000 });
  assert.equal(restAt(lines, 0, 8000), null);
  assert.deepEqual(restAt(lines, 0, 12000), { next: 1, from: 9000, to: 20000 });
  // No end marked: no dots, however long the pause.
  assert.equal(restAt(lines, 1, 21000), null);
  assert.equal(restAt(parseLRC('[00:01.00]开头就唱'), -1, 0), null);
});
