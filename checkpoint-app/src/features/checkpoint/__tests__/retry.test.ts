import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { growBackoffMs, nextRetryDelayMs } from '../retry.ts';

describe('nextRetryDelayMs', () => {
  it('starts at the first delay', () => {
    assert.equal(nextRetryDelayMs(0), 5_000);
    assert.equal(nextRetryDelayMs(1), 5_000);
  });

  it('grows through the schedule', () => {
    assert.equal(nextRetryDelayMs(2), 30_000);
    assert.equal(nextRetryDelayMs(3), 120_000);
    assert.equal(nextRetryDelayMs(4), 600_000);
  });

  it('caps at the final delay', () => {
    assert.equal(nextRetryDelayMs(5), 1_800_000);
    assert.equal(nextRetryDelayMs(50), 1_800_000);
  });

  it('never decreases', () => {
    let previous = 0;
    for (let attempts = 0; attempts <= 8; attempts++) {
      const delay = nextRetryDelayMs(attempts);
      assert.ok(delay >= previous, `attempt ${attempts} decreased`);
      previous = delay;
    }
  });
});

describe('growBackoffMs', () => {
  it('doubles the delay', () => {
    assert.equal(growBackoffMs(2_000, 30_000), 4_000);
    assert.equal(growBackoffMs(4_000, 30_000), 8_000);
  });

  it('caps at the maximum', () => {
    assert.equal(growBackoffMs(16_000, 30_000), 30_000);
    assert.equal(growBackoffMs(30_000, 30_000), 30_000);
  });
});
