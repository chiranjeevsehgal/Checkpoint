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

export const CATEGORY_ACCENT: Record<Category, string> = {
  todos: 'border-l-primary',
  reminders: 'border-l-warning',
  insights: 'border-l-success',
};

/** The date an item is shown by: the recording time when the pendant knew it. */
export function itemDate(item: Item): string {
  if ('recorded_at' in item && item.recorded_at) {
    return item.recorded_at;
  }
  return item.created_at;
}

/** The date an item is grouped by: a reminder's due time when it has one. */
export function groupDate(item: Item): string {
  if ('remind_at' in item && item.remind_at) {
    return item.remind_at;
  }
  return itemDate(item);
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

export interface DaySection<T> {
  title: string;
  data: T[];
}

function startOfDay(date: Date): number {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();
}

/** A day heading: Today, Yesterday, Tomorrow, or a short calendar date. */
export function sectionTitle(iso: string, now: Date = new Date()): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return 'Unknown date';
  const days = Math.round((startOfDay(now) - startOfDay(date)) / 86_400_000);
  if (days === 0) return 'Today';
  if (days === 1) return 'Yesterday';
  if (days === -1) return 'Tomorrow';
  return date.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' });
}

/**
 * Groups consecutive items sharing a day, preserving the incoming order so a
 * feed sorted by due time keeps its direction (soonest-first or newest-first).
 */
export function groupByDay<T>(
  items: T[],
  getDate: (item: T) => string,
  now: Date = new Date(),
): DaySection<T>[] {
  const sections: DaySection<T>[] = [];
  for (const item of items) {
    const title = sectionTitle(getDate(item), now);
    const last = sections[sections.length - 1];
    if (last?.title === title) {
      last.data.push(item);
    } else {
      sections.push({ title, data: [item] });
    }
  }
  return sections;
}

function plural(total: number, singular: string, pluralForm: string): string {
  return `${total} ${total === 1 ? singular : pluralForm}`;
}

/** The header line for the active category and its current filter. */
export function summaryLabel(category: Category, total: number, filter: string): string {
  switch (category) {
    case 'todos':
      return plural(total, 'to-do', 'to-dos');
    case 'reminders':
      if (filter === 'upcoming') return plural(total, 'upcoming reminder', 'upcoming reminders');
      if (filter === 'past') return plural(total, 'past reminder', 'past reminders');
      return plural(total, 'reminder', 'reminders');
    case 'insights':
      return plural(total, 'insight', 'insights');
  }
}

export function emptyTitle(category: Category): string {
  switch (category) {
    case 'todos':
      return 'No to-dos yet';
    case 'reminders':
      return 'No reminders yet';
    case 'insights':
      return 'No insights yet';
  }
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
