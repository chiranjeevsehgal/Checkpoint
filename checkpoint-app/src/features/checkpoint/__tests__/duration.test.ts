import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { formatDuration } from '../duration.ts';

describe('formatDuration', () => {
  it('pads hours, minutes and seconds', () => {
    assert.equal(formatDuration(0), '00:00:00');
    assert.equal(formatDuration(102), '00:01:42');
    assert.equal(formatDuration(3725), '01:02:05');
  });

  it('clamps negatives and floors fractions', () => {
    assert.equal(formatDuration(-5), '00:00:00');
    assert.equal(formatDuration(59.9), '00:00:59');
  });
});
