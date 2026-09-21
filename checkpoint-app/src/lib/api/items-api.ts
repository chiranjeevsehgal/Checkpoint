import { apiFetch } from './api-client';

export const ITEMS_PAGE_SIZE = 50;

export interface ItemPage<T> {
  items: T[];
  next_offset: number | null;
  total: number;
}

export interface Todo {
  id: number;
  text: string;
  is_done: boolean;
  audio_id: string;
  recorded_at: string | null;
  created_at: string;
}

export interface Reminder {
  id: number;
  text: string;
  remind_at: string | null;
  important: boolean;
  audio_id: string;
  created_at: string;
}

export interface Insight {
  id: number;
  text: string;
  audio_id: string;
  created_at: string;
}

export type TodoStatus = 'all' | 'open' | 'done';
export type ReminderWindow = 'upcoming' | 'past' | 'all';

function pageQuery(params: Record<string, string | number>): string {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    query.set(key, String(value));
  }
  return query.toString();
}

export function listTodos(
  token: string,
  status: TodoStatus,
  offset: number,
): Promise<ItemPage<Todo>> {
  const query = pageQuery({ status, limit: ITEMS_PAGE_SIZE, offset });
  return apiFetch<ItemPage<Todo>>(`/v1/me/todos?${query}`, {}, token);
}

export async function setTodoDone(token: string, id: number, isDone: boolean): Promise<void> {
  await apiFetch<{ is_done: boolean }>(
    `/v1/me/todos/${id}`,
    { method: 'PATCH', body: JSON.stringify({ is_done: isDone }) },
    token,
  );
}

export async function updateTodo(
  token: string,
  id: number,
  patch: { text?: string; is_done?: boolean },
): Promise<Todo> {
  return apiFetch<Todo>(
    `/v1/me/todos/${id}`,
    { method: 'PATCH', body: JSON.stringify(patch) },
    token,
  );
}

export async function deleteTodo(token: string, id: number): Promise<void> {
  await apiFetch<{ deleted: boolean }>(`/v1/me/todos/${id}`, { method: 'DELETE' }, token);
}

export function listReminders(
  token: string,
  window: ReminderWindow,
  offset: number,
): Promise<ItemPage<Reminder>> {
  const query = pageQuery({ window, limit: ITEMS_PAGE_SIZE, offset });
  return apiFetch<ItemPage<Reminder>>(`/v1/me/reminders?${query}`, {}, token);
}

export async function deleteReminder(token: string, id: number): Promise<void> {
  await apiFetch<{ deleted: boolean }>(`/v1/me/reminders/${id}`, { method: 'DELETE' }, token);
}

export async function updateReminder(
  token: string,
  id: number,
  patch: { text?: string; remind_at?: string | null; important?: boolean },
): Promise<Reminder> {
  return apiFetch<Reminder>(
    `/v1/me/reminders/${id}`,
    { method: 'PATCH', body: JSON.stringify(patch) },
    token,
  );
}

export function listInsights(token: string, offset: number): Promise<ItemPage<Insight>> {
  const query = pageQuery({ limit: ITEMS_PAGE_SIZE, offset });
  return apiFetch<ItemPage<Insight>>(`/v1/me/insights?${query}`, {}, token);
}

export async function deleteInsight(token: string, id: number): Promise<void> {
  await apiFetch<{ deleted: boolean }>(`/v1/me/insights/${id}`, { method: 'DELETE' }, token);
}

export async function updateInsight(token: string, id: number, text: string): Promise<Insight> {
  return apiFetch<Insight>(
    `/v1/me/insights/${id}`,
    { method: 'PATCH', body: JSON.stringify({ text }) },
    token,
  );
}
