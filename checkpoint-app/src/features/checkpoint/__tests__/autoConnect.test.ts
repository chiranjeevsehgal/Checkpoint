import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import {
  AUTO_CONNECT_COOLDOWN_MS,
  shouldAttemptAutoConnect,
  type AutoConnectGate,
} from '../config.ts';

function gate(overrides: Partial<AutoConnectGate> = {}): AutoConnectGate {
  return {
    autoSyncEnabled: true,
    suppressed: false,
    connected: false,
    busy: false,
    autoConnecting: false,
    lastAttemptAt: 0,
    now: AUTO_CONNECT_COOLDOWN_MS,
    ...overrides,
  };
}

describe('shouldAttemptAutoConnect', () => {
  it('allows an idle attempt when enabled', () => {
    assert.equal(shouldAttemptAutoConnect(gate()), true);
  });

  it('blocks when auto-sync is off', () => {
    assert.equal(shouldAttemptAutoConnect(gate({ autoSyncEnabled: false })), false);
  });

  it('blocks after a manual disconnect', () => {
    assert.equal(shouldAttemptAutoConnect(gate({ suppressed: true })), false);
  });

  it('blocks while connected, busy or already connecting', () => {
    assert.equal(shouldAttemptAutoConnect(gate({ connected: true })), false);
    assert.equal(shouldAttemptAutoConnect(gate({ busy: true })), false);
    assert.equal(shouldAttemptAutoConnect(gate({ autoConnecting: true })), false);
  });

  it('waits for the cooldown to elapse', () => {
    assert.equal(shouldAttemptAutoConnect(gate({ lastAttemptAt: 1000, now: 1000 })), false);
    assert.equal(
      shouldAttemptAutoConnect(gate({ lastAttemptAt: 1000, now: 1000 + AUTO_CONNECT_COOLDOWN_MS })),
      true,
    );
  });
});
