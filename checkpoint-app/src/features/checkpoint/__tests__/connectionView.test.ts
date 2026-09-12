import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { connectionActivity, formatFingerprint } from '../connectionView.ts';

describe('formatFingerprint', () => {
  it('returns empty for missing or short id', () => {
    assert.equal(formatFingerprint(null), '');
    assert.equal(formatFingerprint('abcd'), '');
  });

  it('uppercases and groups the first 8 hex chars', () => {
    assert.equal(formatFingerprint('e8f60a89384d'), 'E8F6-0A89');
  });
});

describe('connectionActivity', () => {
  it('reports not connected', () => {
    assert.deepEqual(
      connectionActivity({ connected: false, recording: true, vadActive: true, syncing: 3 }),
      { label: 'Not connected', tone: 'disconnected' },
    );
  });

  it('prioritizes recording over syncing', () => {
    assert.deepEqual(
      connectionActivity({ connected: true, recording: true, vadActive: false, syncing: 2 }),
      { label: 'Recording now', tone: 'recording' },
    );
  });

  it('reports capturing speech while the VAD is active', () => {
    assert.deepEqual(
      connectionActivity({ connected: true, recording: false, vadActive: true, syncing: 0 }),
      { label: 'Capturing speech', tone: 'capturing' },
    );
  });

  it('reports the number of syncing transfers', () => {
    assert.deepEqual(
      connectionActivity({ connected: true, recording: false, vadActive: false, syncing: 2 }),
      { label: 'Syncing 2', tone: 'syncing' },
    );
  });

  it('reports idle when connected with no activity', () => {
    assert.deepEqual(
      connectionActivity({ connected: true, recording: false, vadActive: false, syncing: 0 }),
      { label: 'Idle', tone: 'connected' },
    );
  });
});
