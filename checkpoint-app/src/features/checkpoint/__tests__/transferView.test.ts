import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { formatTransferTime, transferView, type TransferInput } from '../transferView.ts';

function input(overrides: Partial<TransferInput> = {}): TransferInput {
  return {
    fileId: 'ab12',
    totalBytes: 2048,
    received: 0,
    totalFrags: 10,
    vad: '—',
    ingest: 'pending',
    ...overrides,
  };
}

describe('transferView', () => {
  it('is receiving until every fragment arrives', () => {
    const view = transferView(input({ received: 4 }));
    assert.equal(view.stage, 'receiving');
    assert.equal(view.vadLabel, 'Checking for speech…');
    assert.equal(view.ingestLabel, 'Waiting…');
    assert.equal(view.vadLabel.includes('Waiting'), false);
  });

  it('is analyzing once received but before the verdict', () => {
    const view = transferView(input({ received: 10, vad: 'pending' }));
    assert.equal(view.stage, 'analyzing');
  });

  it('is uploading once the verdict is in but the result is not', () => {
    const view = transferView(input({ received: 10, vad: 'speech 3.2s' }));
    assert.equal(view.stage, 'uploading');
    assert.equal(view.vadLabel, 'Speech · 3.2s');
    assert.equal(view.ingestLabel, 'Uploading…');
  });

  it('is done with an upload id when accepted', () => {
    const view = transferView(
      input({ received: 10, vad: 'speech 3.20s', ingest: 'READY ab12cd34' }),
    );
    assert.equal(view.stage, 'done');
    assert.equal(view.outcome, 'uploaded');
    assert.equal(view.ingestLabel, 'Uploaded · ab12cd34');
    assert.equal(view.failed, false);
  });

  it('labels filtered silence', () => {
    const view = transferView(
      input({ received: 10, vad: 'no-speech 0.00s', ingest: 'skipped-no-speech' }),
    );
    assert.equal(view.vadLabel, 'No speech detected');
    assert.equal(view.ingestLabel, 'Filtered — silence');
    assert.equal(view.outcome, 'filtered');
  });

  it('surfaces a failure reason', () => {
    const view = transferView(
      input({ received: 10, vad: 'speech 1.0s', ingest: 'failed: network timeout' }),
    );
    assert.equal(view.failed, true);
    assert.equal(view.outcome, 'failed');
    assert.equal(view.ingestLabel, 'Failed — network timeout');
  });

  it('exposes a canonical status and headline', () => {
    assert.equal(transferView(input({ received: 4 })).headline, 'Receiving');
    assert.equal(transferView(input({ received: 4 })).status, 'receiving');
    assert.equal(transferView(input({ received: 10, vad: 'pending' })).headline, 'Analyzing');
    assert.equal(transferView(input({ received: 10, vad: 'speech 3.2s' })).status, 'uploading');
    const uploaded = transferView(
      input({ received: 10, vad: 'speech 3.20s', ingest: 'READY ab12cd34' }),
    );
    assert.equal(uploaded.headline, 'Uploaded successfully');
    assert.equal(uploaded.status, 'uploaded');
    const filtered = transferView(
      input({ received: 10, vad: 'no-speech 0.00s', ingest: 'skipped-no-speech' }),
    );
    assert.equal(filtered.headline, 'No speech detected');
    assert.equal(filtered.status, 'filtered');
    const failed = transferView(
      input({ received: 10, vad: 'speech 1.0s', ingest: 'failed: network timeout' }),
    );
    assert.equal(failed.headline, 'Upload failed');
    assert.equal(failed.status, 'failed');
  });
});

describe('formatTransferTime', () => {
  it('shows a clock time for today and a date for older items', () => {
    const now = new Date(2026, 4, 10, 20, 0, 0).getTime();
    assert.equal(formatTransferTime(new Date(2026, 4, 10, 20, 42, 0).getTime(), now), '8:42 PM');
    assert.notEqual(
      formatTransferTime(new Date(2026, 4, 9, 20, 42, 0).getTime(), now),
      '8:42 PM',
    );
  });
});
