import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { fetchWithTimeout } from '../http.ts';

function stubFetch(handler: typeof fetch): () => void {
  const original = globalThis.fetch;
  globalThis.fetch = handler;
  return () => {
    globalThis.fetch = original;
  };
}

describe('fetchWithTimeout', () => {
  it('rejects when the request outlives the timeout', async () => {
    const restore = stubFetch(
      ((_input, init) =>
        new Promise((_resolve, reject) => {
          init?.signal?.addEventListener('abort', () => reject(init.signal?.reason));
        })) as typeof fetch,
    );
    try {
      await assert.rejects(fetchWithTimeout('http://192.0.2.1', {}, 20));
    } finally {
      restore();
    }
  });

  it('resolves a response that arrives in time', async () => {
    const restore = stubFetch((() =>
      Promise.resolve(new Response('ok', { status: 200 }))) as typeof fetch);
    try {
      const response = await fetchWithTimeout('http://example.invalid', {}, 1000);
      assert.equal(response.status, 200);
    } finally {
      restore();
    }
  });
});
