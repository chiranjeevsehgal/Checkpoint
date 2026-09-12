import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import {
  statusDescriptor,
  TONE_DOT,
  TONE_TEXT,
  type CheckpointStatus,
  type StatusTone,
} from '../status.ts';

describe('statusDescriptor', () => {
  it('uses the canonical label for each status', () => {
    const cases: Record<CheckpointStatus, string> = {
      ready: 'Ready',
      connected: 'Connected',
      disconnected: 'Disconnected',
      recording: 'Recording',
      receiving: 'Receiving',
      analyzing: 'Analyzing',
      uploading: 'Uploading',
      uploaded: 'Uploaded',
      filtered: 'Filtered',
      failed: 'Failed',
    };
    for (const [status, label] of Object.entries(cases)) {
      assert.equal(statusDescriptor(status as CheckpointStatus).label, label);
    }
  });

  it('maps failures to the danger tone and healthy states to success', () => {
    assert.equal(statusDescriptor('failed').tone, 'danger');
    assert.equal(statusDescriptor('uploaded').tone, 'success');
    assert.equal(statusDescriptor('ready').tone, 'success');
    assert.equal(statusDescriptor('connected').tone, 'success');
    assert.equal(statusDescriptor('disconnected').tone, 'muted');
  });

  it('keeps activity states visible', () => {
    assert.equal(statusDescriptor('recording').tone, 'primary');
    assert.equal(statusDescriptor('receiving').tone, 'primary');
    assert.equal(statusDescriptor('uploading').tone, 'primary');
    assert.equal(statusDescriptor('analyzing').tone, 'warning');
  });
});

describe('tone class maps', () => {
  it('defines text and dot classes for every tone', () => {
    const tones: StatusTone[] = ['success', 'primary', 'warning', 'danger', 'muted'];
    for (const tone of tones) {
      assert.ok(TONE_TEXT[tone].length > 0, `${tone} text`);
      assert.ok(TONE_DOT[tone].length > 0, `${tone} dot`);
    }
  });
});
