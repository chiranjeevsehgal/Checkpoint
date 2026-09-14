import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

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
    assert.equal(linkView('connecting', 'Pendant-1').sub, 'Looking for "Pendant-1" Pendant');
    assert.equal(linkView('listening', 'Pendant-1').sub, 'Linked to Pendant-1 Pendant');
  });

  it('flags permission states for the settings action', () => {
    assert.equal(linkView('needs permission', 'Checkpoint').openSettings, true);
    assert.equal(linkView('permission denied', 'Checkpoint').openSettings, true);
    assert.equal(linkView('bluetooth off', 'Checkpoint').openSettings, false);
  });

  it('describes an active reconnect', () => {
    assert.equal(linkView('reconnecting', 'Checkpoint').label, 'Reconnecting…');
  });

  it('explains a pendant linked to another account', () => {
    const view = linkView('not owned', 'Checkpoint');
    assert.equal(view.label, 'Linked to another account');
    assert.match(view.sub, /another Checkpoint account/);
    assert.equal(view.openSettings, false);
  });

  it('falls back to idle for unknown states', () => {
    assert.equal(linkView('something-else', 'Checkpoint').label, 'Not connected');
  });
});
