import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import {
  CATEGORIES,
  emptyMessage,
  formatItemDate,
  formatReminderTime,
  itemDate,
  mergePage,
  REMINDER_WINDOWS,
  TODO_STATUSES,
} from '../itemsView.ts';

const RECORDED = '2026-09-20T06:30:00Z';
const CREATED = '2026-09-21T09:00:00Z';

describe('itemDate', () => {
  it('prefers the recorded time when the pendant clock was synced', () => {
    assert.equal(
      itemDate({
        id: 1,
        text: 'x',
        is_done: false,
        audio_id: 'a',
        recorded_at: RECORDED,
        created_at: CREATED,
      }),
      RECORDED,
    );
  });

  it('falls back to created_at for unsynced to-dos', () => {
    assert.equal(
      itemDate({
        id: 1,
        text: 'x',
        is_done: false,
        audio_id: 'a',
        recorded_at: null,
        created_at: CREATED,
      }),
      CREATED,
    );
  });

  it('uses created_at for reminders and insights', () => {
    assert.equal(
      itemDate({
        id: 2,
        text: 'y',
        remind_at: null,
        important: false,
        audio_id: 'a',
        created_at: CREATED,
      }),
      CREATED,
    );
    assert.equal(itemDate({ id: 3, text: 'z', audio_id: 'a', created_at: CREATED }), CREATED);
  });
});

describe('formatItemDate', () => {
  it('renders a dash for missing or invalid values', () => {
    assert.equal(formatItemDate(null), '—');
    assert.equal(formatItemDate('not-a-date'), '—');
  });

  it('renders a non-dash value for a valid timestamp', () => {
    assert.notEqual(formatItemDate(CREATED), '—');
  });
});

describe('formatReminderTime', () => {
  it('labels an unresolved reminder time', () => {
    assert.equal(formatReminderTime(null), 'No time set');
  });

  it('renders a valid due time', () => {
    assert.notEqual(formatReminderTime(RECORDED), 'No time set');
  });
});

describe('mergePage', () => {
  it('appends the next page to the current items', () => {
    const page = { items: [{ id: 2 }, { id: 3 }], next_offset: 4 };
    assert.deepEqual(mergePage([{ id: 1 }], page), [{ id: 1 }, { id: 2 }, { id: 3 }]);
  });
});

describe('filter catalogs', () => {
  it('exposes the expected categories and filters', () => {
    assert.deepEqual(
      CATEGORIES.map((option) => option.key),
      ['todos', 'reminders', 'insights'],
    );
    assert.deepEqual(
      TODO_STATUSES.map((option) => option.key),
      ['all', 'open', 'done'],
    );
    assert.deepEqual(
      REMINDER_WINDOWS.map((option) => option.key),
      ['upcoming', 'past', 'all'],
    );
  });

  it('describes an empty list for every category', () => {
    for (const { key } of CATEGORIES) {
      assert.ok(emptyMessage(key).length > 0);
    }
  });
});
