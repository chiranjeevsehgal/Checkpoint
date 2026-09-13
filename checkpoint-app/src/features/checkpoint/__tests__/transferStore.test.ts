import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import {
  applyEventToRecords,
  isInProgress,
  isTerminal,
  patchTransfer,
  pruneExpired,
  sortTransfers,
  type TransferRecord,
} from '../transferStore.ts';
import type { CheckpointEvent } from '../types.ts';

const NOW = 1_000_000;

function announce(overrides: Partial<Extract<CheckpointEvent, { type: 'announce' }>> = {}) {
  return {
    type: 'announce' as const,
    fileId: 'aaaa000000000001',
    totalBytes: 2048,
    totalFrags: 10,
    ...overrides,
  };
}

function record(overrides: Partial<TransferRecord> = {}): TransferRecord {
  return {
    fileId: 'aaaa000000000001',
    createdAt: NOW,
    updatedAt: NOW,
    totalBytes: 2048,
    received: 10,
    totalFrags: 10,
    vad: 'speech 2.0s',
    ingest: 'READY abcd1234',
    outcome: 'uploaded',
    attempts: 0,
    ...overrides,
  };
}

describe('applyEventToRecords', () => {
  it('creates a pending record on announce', () => {
    const records = applyEventToRecords([], announce(), NOW);
    assert.equal(records.length, 1);
    assert.equal(records[0]!.createdAt, NOW);
    assert.equal(records[0]!.outcome, 'pending');
    assert.equal(records[0]!.attempts, 0);
  });

  it('ignores a duplicate announce', () => {
    const first = applyEventToRecords([], announce(), NOW);
    const second = applyEventToRecords(first, announce(), NOW + 50);
    assert.equal(second.length, 1);
    assert.equal(second[0]!.createdAt, NOW);
  });

  it('tracks progress without changing createdAt', () => {
    const first = applyEventToRecords([], announce(), NOW);
    const next = applyEventToRecords(
      first,
      { type: 'progress', fileId: 'aaaa000000000001', received: 4, totalFrags: 10 },
      NOW + 100,
    );
    assert.equal(next[0]!.received, 4);
    assert.equal(next[0]!.createdAt, NOW);
    assert.equal(next[0]!.outcome, 'pending');
  });

  it('stores the device recording time from file_done', () => {
    let records = applyEventToRecords([], announce(), NOW);
    records = applyEventToRecords(
      records,
      {
        type: 'file_done',
        fileId: 'aaaa000000000001',
        crcOk: true,
        totalBytes: 2048,
        ingestStatus: 'pending',
        vadStatus: 'pending',
        recordedAt: 1_789_194_600_000,
      },
      NOW + 100,
    );
    assert.equal(records[0]!.recordedAt, 1_789_194_600_000);
  });

  it('leaves recordedAt unset when the device had no anchor', () => {
    let records = applyEventToRecords([], announce(), NOW);
    records = applyEventToRecords(
      records,
      {
        type: 'file_done',
        fileId: 'aaaa000000000001',
        crcOk: true,
        totalBytes: 2048,
        ingestStatus: 'pending',
        vadStatus: 'pending',
      },
      NOW + 100,
    );
    assert.equal(records[0]!.recordedAt, undefined);
  });

  it('marks an accepted transfer uploaded', () => {
    let records = applyEventToRecords([], announce(), NOW);
    records = applyEventToRecords(
      records,
      {
        type: 'file_done',
        fileId: 'aaaa000000000001',
        crcOk: true,
        totalBytes: 2048,
        ingestStatus: 'pending',
        vadStatus: 'pending',
      },
      NOW + 100,
    );
    records = applyEventToRecords(
      records,
      {
        type: 'ingest',
        fileId: 'aaaa000000000001',
        uploadId: 'abcd1234',
        ingestStatus: 'SUBMITTED',
        ingestError: '',
        vadStatus: 'speech',
        vadSpeechS: '2.00',
      },
      NOW + 200,
    );
    assert.equal(records[0]!.outcome, 'uploaded');
    assert.equal(records[0]!.uploadId, 'abcd1234');
    assert.ok(isTerminal(records[0]!));
  });

  it('marks a failed transfer and keeps the reason', () => {
    let records = applyEventToRecords([], announce(), NOW);
    records = applyEventToRecords(
      records,
      {
        type: 'file_done',
        fileId: 'aaaa000000000001',
        crcOk: true,
        totalBytes: 2048,
        ingestStatus: 'pending',
        vadStatus: 'pending',
      },
      NOW + 100,
    );
    records = applyEventToRecords(
      records,
      {
        type: 'ingest',
        fileId: 'aaaa000000000001',
        uploadId: '',
        ingestStatus: 'failed',
        ingestError: 'network timeout',
        vadStatus: 'speech',
        vadSpeechS: '2.00',
      },
      NOW + 200,
    );
    assert.equal(records[0]!.outcome, 'failed');
    assert.match(records[0]!.ingest, /^failed/);
    assert.equal(isTerminal(records[0]!), false);
  });

  it('records the local uri when provided', () => {
    let records = applyEventToRecords([], announce(), NOW);
    records = applyEventToRecords(
      records,
      {
        type: 'file_done',
        fileId: 'aaaa000000000001',
        crcOk: true,
        totalBytes: 2048,
        ingestStatus: 'pending',
        vadStatus: 'pending',
      },
      NOW + 150,
      'document/checkpoint/received/file_aaaa000000000001.ogg',
    );
    assert.equal(records[0]!.localUri, 'document/checkpoint/received/file_aaaa000000000001.ogg');
  });
});

describe('patchTransfer', () => {
  it('updates retry bookkeeping while keeping the outcome', () => {
    const next = patchTransfer(
      [record({ outcome: 'failed', ingest: 'failed: network timeout' })],
      'aaaa000000000001',
      NOW + 5000,
      { attempts: 2, nextAttemptAt: NOW + 35_000 },
    );
    assert.equal(next[0]!.attempts, 2);
    assert.equal(next[0]!.nextAttemptAt, NOW + 35_000);
    assert.equal(next[0]!.outcome, 'failed');
  });
});

describe('isInProgress', () => {
  it('is true while receiving or uploading', () => {
    assert.equal(isInProgress(record({ outcome: 'pending', ingest: 'pending' })), true);
  });

  it('is false for failed and completed transfers', () => {
    assert.equal(
      isInProgress(record({ outcome: 'failed', ingest: 'failed: network timeout' })),
      false,
    );
    assert.equal(isInProgress(record({ outcome: 'uploaded' })), false);
  });
});

describe('sortTransfers', () => {
  it('orders newest first and breaks ties by fileId descending', () => {
    const sorted = sortTransfers([
      record({ fileId: 'aaaa000000000001', createdAt: NOW }),
      record({ fileId: 'aaaa000000000002', createdAt: NOW + 10 }),
      record({ fileId: 'bbbb000000000001', createdAt: NOW }),
    ]);
    assert.deepEqual(
      sorted.map((item) => item.fileId),
      ['aaaa000000000002', 'bbbb000000000001', 'aaaa000000000001'],
    );
  });
});

describe('pruneExpired', () => {
  const retentionMs = 24 * 60 * 60 * 1000;

  it('removes terminal records past retention', () => {
    const records = pruneExpired([record({ createdAt: NOW - retentionMs - 1 })], NOW, retentionMs);
    assert.equal(records.length, 0);
  });

  it('keeps recent terminal records', () => {
    const records = pruneExpired([record({ createdAt: NOW - 1000 })], NOW, retentionMs);
    assert.equal(records.length, 1);
  });

  it('never removes non-terminal records', () => {
    const records = pruneExpired(
      [record({ outcome: 'failed', createdAt: NOW - retentionMs - 1 })],
      NOW,
      retentionMs,
    );
    assert.equal(records.length, 1);
  });
});
