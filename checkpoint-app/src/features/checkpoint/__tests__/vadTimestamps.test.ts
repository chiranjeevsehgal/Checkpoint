import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { buildVadWindows, groupSpeechProbs, totalSpeechSeconds } from '../vadTimestamps.ts';

const OPTS = { threshold: 0.85 };
const HIGH = 0.95;
const LOW = 0.05;

function windows(high: [number, number][], total: number): number[] {
  const probs = new Array<number>(total).fill(LOW);
  for (const [from, to] of high) {
    for (let i = from; i < to; i++) probs[i] = HIGH;
  }
  return probs;
}

describe('groupSpeechProbs', () => {
  it('finds one padded span in speech then silence', () => {
    const spans = groupSpeechProbs(windows([[0, 100]], 200), OPTS);
    assert.deepEqual(spans, [{ start: 0, end: 3.4 }]);
  });

  it('returns empty for silence', () => {
    assert.deepEqual(groupSpeechProbs(windows([], 200), OPTS), []);
    assert.deepEqual(groupSpeechProbs([], OPTS), []);
  });

  it('drops a blip shorter than min speech', () => {
    assert.deepEqual(groupSpeechProbs(windows([[0, 10]], 200), OPTS), []);
  });

  it('bridges a short silence gap', () => {
    const spans = groupSpeechProbs(
      windows(
        [
          [0, 100],
          [106, 200],
        ],
        200,
      ),
      OPTS,
    );
    assert.deepEqual(spans, [{ start: 0, end: 6.4 }]);
  });

  it('splits on a long silence with padding', () => {
    const spans = groupSpeechProbs(
      windows(
        [
          [0, 50],
          [100, 200],
        ],
        200,
      ),
      OPTS,
    );
    assert.deepEqual(spans, [
      { start: 0, end: 1.8 },
      { start: 3.0, end: 6.4 },
    ]);
  });
});

describe('totalSpeechSeconds', () => {
  it('sums span durations', () => {
    const total = totalSpeechSeconds([
      { start: 0, end: 1.8 },
      { start: 3.0, end: 6.4 },
    ]);
    assert.ok(Math.abs(total - 5.2) < 1e-9, `got ${total}`);
  });
});

describe('buildVadWindows', () => {
  it('prepends zero context and carries the trailing 64 samples', () => {
    const pcm = Float32Array.from({ length: 512 * 3 }, (_, i) => i + 1);
    const windows = buildVadWindows(pcm);
    assert.equal(windows.length, 3);
    assert.equal(windows[0]!.length, 576);
    assert.deepEqual([...windows[0]!.slice(0, 64)], new Array(64).fill(0));
    assert.deepEqual([...windows[1]!.slice(0, 64)], [...windows[0]!.slice(512)]);
    assert.deepEqual([...windows[2]!.slice(0, 64)], [...windows[1]!.slice(512)]);
  });

  it('zero-pads a short final window', () => {
    const windows = buildVadWindows(Float32Array.from({ length: 512 + 10 }, () => 1));
    assert.equal(windows.length, 2);
    assert.deepEqual([...windows[1]!.slice(64 + 10)], new Array(502).fill(0));
  });

  it('returns no windows for empty input', () => {
    assert.deepEqual(buildVadWindows(new Float32Array(0)), []);
  });
});
