import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { resolveDeveloperMode } from '../config.ts';

describe('resolveDeveloperMode', () => {
  it('is off in a normal build even when previously enabled', () => {
    assert.equal(resolveDeveloperMode('1', false), false);
  });

  it('defaults on in a dev build', () => {
    assert.equal(resolveDeveloperMode(null, true), true);
  });

  it('stays off in a dev build when the user disabled it', () => {
    assert.equal(resolveDeveloperMode('0', true), false);
  });

  it('defaults off in a normal build', () => {
    assert.equal(resolveDeveloperMode(null, false), false);
  });
});
