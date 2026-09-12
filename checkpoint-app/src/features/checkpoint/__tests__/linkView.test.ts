import { describe, it } from 'node:test';
import assert from 'node:assert/strict';

import { linkView } from '../linkView.ts';

describe('linkView', () => {
  it('describes idle', () => {
    assert.deepEqual(linkView('idle', 'Checkpoint'), {
      label: 'Not connected',
      sub: 'Tap Connect to look for your pendant.',
      openSettings: false,
    });
  });

  it('includes the device name while connecting and connected', () => {
    assert.equal(linkView('connecting', 'Pendant-1').sub, 'Looking for "Pendant-1"');
    assert.equal(linkView('listening', 'Pendant-1').sub, 'Linked to Pendant-1');
  });

  it('flags permission states for the settings action', () => {
    assert.equal(linkView('needs permission', 'Checkpoint').openSettings, true);
    assert.equal(linkView('permission denied', 'Checkpoint').openSettings, true);
    assert.equal(linkView('bluetooth off', 'Checkpoint').openSettings, false);
  });

  it('falls back to idle for unknown states', () => {
    assert.equal(linkView('something-else', 'Checkpoint').label, 'Not connected');
  });
});
