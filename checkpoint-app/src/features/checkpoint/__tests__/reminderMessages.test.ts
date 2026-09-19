import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { buildWebSocketUrl, parseReminderPush } from '../reminderMessages.ts';

describe('buildWebSocketUrl', () => {
  it('maps http to ws and https to wss', () => {
    assert.equal(
      buildWebSocketUrl('http://192.168.1.4:8085', 'cp-abc'),
      'ws://192.168.1.4:8085/cp-abc/ws',
    );
    assert.equal(
      buildWebSocketUrl('https://ntfy.example.com/', 'cp-abc'),
      'wss://ntfy.example.com/cp-abc/ws',
    );
  });
});

describe('parseReminderPush', () => {
  it('parses a message frame', () => {
    const push = parseReminderPush(
      JSON.stringify({
        event: 'message',
        id: 'abc123',
        title: 'Reminder',
        message: 'Call Dad',
        priority: 4,
      }),
    );
    assert.deepEqual(push, { id: 'abc123', title: 'Reminder', body: 'Call Dad', priority: 4 });
  });

  it('defaults the title and synthesizes an id when missing', () => {
    const push = parseReminderPush(JSON.stringify({ event: 'message', message: 'Stand up' }));
    assert.equal(push?.title, 'Reminder');
    assert.ok(push?.id);
  });

  it('ignores non-message and malformed frames', () => {
    assert.equal(parseReminderPush(JSON.stringify({ event: 'open' })), null);
    assert.equal(parseReminderPush(JSON.stringify({ event: 'message' })), null);
    assert.equal(parseReminderPush('not json'), null);
  });
});
