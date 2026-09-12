import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import {
  classifyLog,
  filterLogs,
  formatLogTime,
  isErrorLog,
  type LogCategory,
} from '../logFilter.ts';
import type { LogEntry } from '../types.ts';

describe('classifyLog', () => {
  it('classifies real log prefixes', () => {
    const cases: [string, LogCategory][] = [
      ['[ble] scan failed: timeout', 'ble'],
      ['Found: Checkpoint [47:4F]', 'ble'],
      ['Connected to 47:4F', 'ble'],
      ['[ui] rec-start status=ok', 'recording'],
      ['  [vad] filtered ab12: no speech', 'vad'],
      ['  [ingest] uploading file_ab12.ogg (100B)', 'transfer'],
      ['  [!] ingest failed: no route', 'transfer'],
      ['[preview] fetch x rejected', 'transfer'],
      ['[net] reachable in 142ms', 'server'],
      ['[sync] engine started', 'sync'],
      ['[ui] permissions granted', 'other'],
    ];
    for (const [text, expected] of cases) {
      assert.equal(classifyLog(text), expected, text);
    }
  });
});

describe('filterLogs', () => {
  const logs: LogEntry[] = [
    { at: 1, text: '[ble] link disappeared' },
    { at: 2, text: '[net] reachable in 142ms' },
    { at: 3, text: '  [ingest] OK upload_id=abc status=SUBMITTED' },
  ];

  it('returns everything for the all filter', () => {
    assert.equal(filterLogs(logs, 'all').length, 3);
  });

  it('keeps only the matching category', () => {
    assert.deepEqual(
      filterLogs(logs, 'server').map((entry) => entry.at),
      [2],
    );
    assert.deepEqual(
      filterLogs(logs, 'transfer').map((entry) => entry.at),
      [3],
    );
  });
});

describe('isErrorLog', () => {
  it('flags explicit and failed entries', () => {
    assert.equal(isErrorLog('  [!] ingest failed: no route'), true);
    assert.equal(isErrorLog('[ble] setup failed: bad key'), true);
    assert.equal(isErrorLog('AUTH rejected with error 0x03'), true);
    assert.equal(isErrorLog('[ble] link disappeared'), false);
  });
});

describe('formatLogTime', () => {
  it('renders zero-padded local time', () => {
    const at = new Date(2026, 0, 1, 9, 5, 7).getTime();
    assert.equal(formatLogTime(at), '09:05:07');
  });
});
