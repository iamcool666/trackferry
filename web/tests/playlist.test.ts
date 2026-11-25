import { test } from 'node:test';
import assert from 'node:assert/strict';
import { arrange, putNext, step, takeOff } from '../src/playlist.ts';

const list = (keys: string, i = 0) => ({ songs: [...keys].map((key, n) => ({ key, n })), i });
const keys = (l: { songs: { key: string }[] }) => l.songs.map((x) => x.key).join('');

test('打乱不换正在放的歌，再换回顺序能还原', () => {
  const l = arrange(list('abcdef', 2), 'shuffle', () => 0);
  assert.equal(l.songs[l.i].key, 'c');
  assert.equal(l.i, 0);
  assert.equal([...keys(l)].sort().join(''), 'abcdef');
  const back = arrange(l, 'loop');
  assert.equal(keys(back), 'abcdef');
  assert.equal(back.songs[back.i].key, 'c');
});

test('下一首放：紧跟在后面，已在列表里的只挪位置，换回顺序也不跑', () => {
  let l = putNext(list('abc'), { key: 'x' });
  assert.equal(keys(l), 'axbc');
  l = putNext(l, { key: 'c' });
  assert.equal(keys(l), 'acxb');
  assert.equal(keys(arrange(l, 'loop')), 'acxb');
  assert.equal(keys(putNext(list('abc', 2), { key: 'a' })), 'bca');
  assert.equal(putNext(list('abc', 2), { key: 'a' }).i, 1);
  assert.equal(keys(putNext(list('ab'), { key: 'a' })), 'ab');
  assert.equal(keys(putNext({ songs: [], i: 0 }, { key: 'a' })), 'a');
});

test('移出：前面的不影响正在放的，正在放的换成下一首', () => {
  assert.deepEqual([keys(takeOff(list('abc', 2), 'a')), takeOff(list('abc', 2), 'a').i], ['bc', 1]);
  assert.deepEqual([keys(takeOff(list('abc', 1), 'b')), takeOff(list('abc', 1), 'b').i], ['ac', 1]);
  assert.equal(takeOff(list('abc', 2), 'c').i, 0);
  assert.equal(takeOff(list('abc', 1), 'z').i, 1);
});

test('往下走：单曲循环只在放完时重复，只有一首时放完就停', () => {
  assert.equal(step(3, 2, 1, 'loop', false), 0);
  assert.equal(step(3, 0, -1, 'loop', true), 2);
  assert.equal(step(3, 1, 1, 'one', false), 1);
  assert.equal(step(3, 1, 1, 'one', true), 2);
  assert.equal(step(1, 0, 1, 'loop', false), null);
  assert.equal(step(0, 0, 1, 'loop', true), null);
});
