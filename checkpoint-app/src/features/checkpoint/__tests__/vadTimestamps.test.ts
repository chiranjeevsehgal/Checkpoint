import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  groupSpeechProbs,
  totalSpeechSeconds,
} from "../vadTimestamps.ts";

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

describe("groupSpeechProbs", () => {
  it("finds one padded span in speech then silence", () => {
    const spans = groupSpeechProbs(windows([[0, 100]], 200), OPTS);
    assert.deepEqual(spans, [{ start: 0, end: 3.4 }]);
  });

  it("returns empty for silence", () => {
    assert.deepEqual(groupSpeechProbs(windows([], 200), OPTS), []);
    assert.deepEqual(groupSpeechProbs([], OPTS), []);
  });

  it("drops a blip shorter than min speech", () => {
    assert.deepEqual(groupSpeechProbs(windows([[0, 10]], 200), OPTS), []);
  });

  it("bridges a short silence gap", () => {
    const spans = groupSpeechProbs(windows([[0, 100], [106, 200]], 200), OPTS);
    assert.deepEqual(spans, [{ start: 0, end: 6.4 }]);
  });

  it("splits on a long silence with padding", () => {
    const spans = groupSpeechProbs(windows([[0, 50], [100, 200]], 200), OPTS);
    assert.deepEqual(spans, [
      { start: 0, end: 1.8 },
      { start: 3.0, end: 6.4 },
    ]);
  });
});

describe("totalSpeechSeconds", () => {
  it("sums span durations", () => {
    const total = totalSpeechSeconds([
      { start: 0, end: 1.8 },
      { start: 3.0, end: 6.4 },
    ]);
    assert.ok(Math.abs(total - 5.2) < 1e-9, `got ${total}`);
  });
});
