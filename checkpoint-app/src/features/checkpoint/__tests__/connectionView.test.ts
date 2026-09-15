import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import {
  connectionActivity,
  formatFingerprint,
  levelPercent,
  signalLabel,
  signalLevel,
} from '../connectionView.ts';

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

describe('levelPercent', () => {
  it('reads empty at and below the silence floor', () => {
    assert.equal(levelPercent(-54), 0);
    assert.equal(levelPercent(-90), 0);
  });

  it('reads full at and above full scale', () => {
    assert.equal(levelPercent(0), 100);
    assert.equal(levelPercent(3), 100);
  });

  it('scales linearly between the floor and full scale', () => {
    assert.equal(levelPercent(-27), 50);
  });
});

describe('signalLevel', () => {
  it('buckets RSSI into five levels at the boundaries', () => {
    assert.equal(signalLevel(-40), 4);
    assert.equal(signalLevel(-50), 4);
    assert.equal(signalLevel(-51), 3);
    assert.equal(signalLevel(-70), 3);
    assert.equal(signalLevel(-71), 2);
    assert.equal(signalLevel(-85), 2);
    assert.equal(signalLevel(-86), 1);
    assert.equal(signalLevel(-95), 1);
    assert.equal(signalLevel(-96), 0);
  });

  it('labels every level', () => {
    assert.equal(signalLabel(4), 'Excellent');
    assert.equal(signalLabel(3), 'Good');
    assert.equal(signalLabel(2), 'Fair');
    assert.equal(signalLabel(1), 'Weak');
    assert.equal(signalLabel(0), 'Very weak');
  });
});
