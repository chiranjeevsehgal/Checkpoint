import type {
  Insight,
  ItemPage,
  Reminder,
  ReminderWindow,
  Todo,
  TodoStatus,
} from '@/lib/api/items-api';

export type Category = 'todos' | 'reminders' | 'insights';

export type Item = Todo | Reminder | Insight;

export const CATEGORIES: { key: Category; label: string }[] = [
  { key: 'todos', label: 'Todos' },
  { key: 'reminders', label: 'Reminders' },
  { key: 'insights', label: 'Insights' },
];

export const TODO_STATUSES: { key: TodoStatus; label: string }[] = [
  { key: 'all', label: 'All' },
  { key: 'open', label: 'Open' },
  { key: 'done', label: 'Done' },
];

export const REMINDER_WINDOWS: { key: ReminderWindow; label: string }[] = [
  { key: 'upcoming', label: 'Upcoming' },
  { key: 'past', label: 'Past' },
  { key: 'all', label: 'All' },
];

/** The date an item is shown by: the recording time when the pendant knew it. */
export function itemDate(item: Item): string {
  if ('recorded_at' in item && item.recorded_at) {
    return item.recorded_at;
  }
  return item.created_at;
}

export function formatItemDate(iso: string | null | undefined): string {
  if (!iso) return '—';
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString();
}

export function formatReminderTime(iso: string | null): string {
  return iso ? formatItemDate(iso) : 'No time set';
}

export function mergePage<T>(current: T[], page: ItemPage<T>): T[] {
  return [...current, ...page.items];
}

export function emptyMessage(category: Category): string {
  switch (category) {
    case 'todos':
      return 'To-dos appear here once a recording is processed.';
    case 'reminders':
      return 'Reminders appear here once a recording is processed.';
    case 'insights':
      return 'Insights appear here once a recording is processed.';
  }
}
