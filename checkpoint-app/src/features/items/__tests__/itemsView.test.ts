import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import {
  CATEGORIES,
  emptyMessage,
  emptyTitle,
  formatItemDate,
  formatReminderTime,
  groupByDay,
  groupDate,
  itemDate,
  mergePage,
  REMINDER_WINDOWS,
  sectionTitle,
  summaryLabel,
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

describe('groupDate', () => {
  it('groups a reminder by its due time when set', () => {
    assert.equal(
      groupDate({
        id: 1,
        text: 'call',
        remind_at: RECORDED,
        important: true,
        audio_id: 'a',
        created_at: CREATED,
      }),
      RECORDED,
    );
  });

  it('falls back to created_at when a reminder has no due time', () => {
    assert.equal(
      groupDate({
        id: 1,
        text: 'call',
        remind_at: null,
        important: true,
        audio_id: 'a',
        created_at: CREATED,
      }),
      CREATED,
    );
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

describe('sectionTitle', () => {
  const now = new Date('2026-09-22T12:00:00');

  it('names the nearby days', () => {
    assert.equal(sectionTitle('2026-09-22T06:00:00', now), 'Today');
    assert.equal(sectionTitle('2026-09-21T06:00:00', now), 'Yesterday');
    assert.equal(sectionTitle('2026-09-23T06:00:00', now), 'Tomorrow');
  });

  it('falls back to a calendar date for older days', () => {
    const title = sectionTitle('2026-09-18T06:00:00', now);
    assert.notEqual(title, 'Today');
    assert.notEqual(title, 'Yesterday');
    assert.notEqual(title, 'Tomorrow');
  });
});

describe('groupByDay', () => {
  it('groups consecutive items by day, preserving order', () => {
    const items = [
      { at: '2026-09-22T09:00:00' },
      { at: '2026-09-22T08:00:00' },
      { at: '2026-09-21T09:00:00' },
    ];
    const sections = groupByDay(items, (item) => item.at, new Date('2026-09-22T12:00:00'));
    assert.deepEqual(
      sections.map((section) => section.title),
      ['Today', 'Yesterday'],
    );
    assert.equal(sections[0]?.data.length, 2);
    assert.equal(sections[1]?.data.length, 1);
  });
});

describe('summaryLabel', () => {
  it('pluralizes each category', () => {
    assert.equal(summaryLabel('todos', 1, 'all'), '1 to-do');
    assert.equal(summaryLabel('todos', 4, 'open'), '4 to-dos');
    assert.equal(summaryLabel('insights', 7, 'all'), '7 insights');
  });

  it('names the reminder window', () => {
    assert.equal(summaryLabel('reminders', 1, 'upcoming'), '1 upcoming reminder');
    assert.equal(summaryLabel('reminders', 2, 'upcoming'), '2 upcoming reminders');
    assert.equal(summaryLabel('reminders', 3, 'past'), '3 past reminders');
    assert.equal(summaryLabel('reminders', 3, 'all'), '3 reminders');
  });
});

describe('mergePage', () => {
  it('appends the next page to the current items', () => {
    const page = { items: [{ id: 2 }, { id: 3 }], next_offset: 4, total: 3 };
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
      assert.ok(emptyTitle(key).length > 0);
      assert.ok(emptyMessage(key).length > 0);
    }
  });
});
