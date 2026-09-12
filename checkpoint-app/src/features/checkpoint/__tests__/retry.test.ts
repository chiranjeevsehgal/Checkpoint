import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { nextRetryDelayMs } from '../retry.ts';

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
