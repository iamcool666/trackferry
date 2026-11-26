import { test } from 'node:test';
import assert from 'node:assert/strict';
import { alsoPlaying, barOutput, HERE_ID, shownOutput, takesNewTV, type OutputNow } from '../src/output-rule.ts';

const here = (playing = false, song = playing): OutputNow => ({ id: HERE_ID, song, playing });
const out = (id: string, playing: boolean, song = true): OutputNow => ({ id, song, playing });

test('选中的设备一直是选中的，别处在放也不抢', () => {
  const all = [here(), out('客厅', true), out('tv:1', false)];
  const there = new Set(all.map((o) => o.id));
  assert.equal(shownOutput('tv:1', there, all), 'tv:1');
  assert.equal(shownOutput(HERE_ID, there, all), HERE_ID);
});

test('选中的设备离线：先显示正在放的，没有就回本机；它回来就还是它', () => {
  const all = [here(), out('客厅', true)];
  assert.equal(shownOutput('tv:1', new Set([HERE_ID, '客厅']), all), '客厅');
  assert.equal(shownOutput('tv:1', new Set([HERE_ID]), [here()]), HERE_ID);
  assert.equal(shownOutput('tv:1', new Set([HERE_ID, '客厅', 'tv:1']), [...all, out('tv:1', false)]), 'tv:1');
});

test('几处同时在放：显示的那台之外有几处说清楚', () => {
  const all = [here(true), out('客厅', true), out('卧室', false)];
  assert.deepEqual(alsoPlaying(HERE_ID, all).map((o) => o.id), ['客厅']);
  assert.deepEqual(alsoPlaying('卧室', all).map((o) => o.id), [HERE_ID, '客厅']);
});

test('底栏和此刻是同一台：有歌就是它，暂停也是它', () => {
  const all = [here(false, true), out('客厅', true)];
  assert.equal(barOutput(HERE_ID, all)?.id, HERE_ID);
  // 选中的没有歌：显示正在放的，点一下就会选中它
  assert.equal(barOutput('卧室', [out('卧室', false, false), ...all])?.id, '客厅');
  assert.equal(barOutput(HERE_ID, [here()]), undefined);
});

test('新电视上线：只在本机被选中且没在放时自动选它', () => {
  assert.equal(takesNewTV(HERE_ID, [here()]), true);
  assert.equal(takesNewTV(HERE_ID, [here(true)]), false);
  assert.equal(takesNewTV('客厅', [here()]), false);
  assert.equal(takesNewTV('tv:2', [here()]), false);
});

import { EQ_BANDS, EQ_PRESETS, eqPreamp, eqPreset, eqWidth } from '../src/eq.ts';

test('音效：每个预设五段，先压低最大增益，不认识的回原声', () => {
  for (const p of EQ_PRESETS) {
    assert.equal(p.gains.length, EQ_BANDS.length);
    assert.ok(eqPreamp(p) <= -Math.max(0, ...p.gains));
  }
  assert.equal(eqPreset('不存在').name, 'flat');
  assert.deepEqual(eqWidth(1), { a: 1, b: 0 });
  const w = eqWidth(1.6);
  assert.ok(w.a > 1 && w.b < 0 && Math.abs(w.a + w.b - 1) < 1e-9); // 单声道内容不变响
});
