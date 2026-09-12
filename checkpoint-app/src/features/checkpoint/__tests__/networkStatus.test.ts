import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { classifyConnectivity, type HealthProbe } from '../networkStatus.ts';

const NOW = 1_000_000;

function probe(overrides: Partial<HealthProbe> = {}): HealthProbe {
  return { ok: true, latencyMs: 120, at: NOW, ...overrides };
}

describe('classifyConnectivity', () => {
  it('is offline when the OS reports no internet', () => {
    assert.equal(classifyConnectivity({ isInternetReachable: false }, [probe()], NOW), 'offline');
    assert.equal(classifyConnectivity({ isConnected: false }, [probe()], NOW), 'offline');
  });

  it('is online before any probe has landed', () => {
    assert.equal(classifyConnectivity({ isInternetReachable: true }, [], NOW), 'online');
  });

  it('is online after a fast success', () => {
    assert.equal(classifyConnectivity({ isInternetReachable: true }, [probe()], NOW), 'online');
  });

  it('is unstable after a slow success', () => {
    assert.equal(
      classifyConnectivity({ isInternetReachable: true }, [probe({ latencyMs: 5_000 })], NOW),
      'unstable',
    );
  });

  it('is server-unavailable after a single failure', () => {
    assert.equal(
      classifyConnectivity({ isInternetReachable: true }, [probe({ ok: false })], NOW),
      'server-unavailable',
    );
  });

  it('is unstable after repeated consecutive failures', () => {
    assert.equal(
      classifyConnectivity(
        { isInternetReachable: true },
        [probe({ ok: false }), probe({ ok: false, at: NOW - 5_000 })],
        NOW,
      ),
      'unstable',
    );
  });

  it('recovers to online once a fresh success arrives', () => {
    assert.equal(
      classifyConnectivity(
        { isInternetReachable: true },
        [probe({ ok: false, at: NOW - 10_000 }), probe({ at: NOW })],
        NOW,
      ),
      'online',
    );
  });

  it('ignores probes outside the unstable window', () => {
    assert.equal(
      classifyConnectivity(
        { isInternetReachable: true },
        [probe({ ok: false, at: NOW - 90_000 })],
        NOW,
      ),
      'online',
    );
  });
});
