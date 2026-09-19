import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { shouldRunSyncService } from '../backgroundPolicy.ts';

describe('shouldRunSyncService', () => {
  it('runs while connected regardless of auto-sync', () => {
    assert.equal(
      shouldRunSyncService({ connected: true, autoSyncEnabled: false, remindersEnabled: false }),
      true,
    );
    assert.equal(
      shouldRunSyncService({ connected: true, autoSyncEnabled: true, remindersEnabled: false }),
      true,
    );
  });

  it('runs while auto-sync is enabled even when disconnected', () => {
    assert.equal(
      shouldRunSyncService({ connected: false, autoSyncEnabled: true, remindersEnabled: false }),
      true,
    );
  });

  it('runs for reminders alone', () => {
    assert.equal(
      shouldRunSyncService({ connected: false, autoSyncEnabled: false, remindersEnabled: true }),
      true,
    );
  });

  it('stops when disconnected with auto-sync and reminders disabled', () => {
    assert.equal(
      shouldRunSyncService({ connected: false, autoSyncEnabled: false, remindersEnabled: false }),
      false,
    );
  });
});
