import { useFocusEffect } from 'expo-router';
import { useCallback, useState } from 'react';
import { View } from 'react-native';

import { AppHeader } from '@/components/shared/app-header';
import { ConfirmDialog } from '@/components/shared/confirm-dialog';
import { Screen } from '@/components/shared/screen';
import {
  ItemEditSheet,
  type EditTarget,
  type EditUpdate,
} from '@/features/items/components/item-edit-sheet';
import {
  FeaturedInsight,
  FilterPills,
  InsightRow,
  ReminderRow,
  SectionedList,
  SubFilters,
  TodoList,
} from '@/features/items/components/notes-lists';
import { CATEGORIES, summaryLabel, type Category } from '@/features/items/itemsView';
import { usePagedItems, type PagedItems } from '@/features/items/usePagedItems';
import {
  deleteInsight,
  deleteReminder,
  deleteTodo,
  listInsights,
  listReminders,
  listTodos,
  setTodoDone,
  updateInsight,
  updateReminder,
  updateTodo,
  type Insight,
  type Reminder,
  type ReminderWindow,
  type Todo,
  type TodoStatus,
} from '@/lib/api/items-api';
import { getSessionToken } from '@/lib/session';
import { describeUserError } from '@/lib/user-errors';
import { useToast } from '@/providers/toast-provider';

type PendingDelete = { category: Category; id: number; text: string };

function useNotesActions({
  todos,
  reminders,
  insights,
  showToast,
}: {
  todos: PagedItems<Todo>;
  reminders: PagedItems<Reminder>;
  insights: PagedItems<Insight>;
  showToast: (message: string) => void;
}) {
  const [pendingDelete, setPendingDelete] = useState<PendingDelete | null>(null);
  const [editTarget, setEditTarget] = useState<EditTarget | null>(null);

  const replaceTodos = todos.replaceItems;
  const replaceReminders = reminders.replaceItems;
  const replaceInsights = insights.replaceItems;
  const reloadTodos = todos.reload;
  const reloadReminders = reminders.reload;
  const reloadInsights = insights.reload;

  const toggleTodo = useCallback(
    async (todo: Todo) => {
      const token = getSessionToken();
      if (!token) return;
      const next = !todo.is_done;
      replaceTodos((items) =>
        items.map((item) => (item.id === todo.id ? { ...item, is_done: next } : item)),
      );
      try {
        await setTodoDone(token, todo.id, next);
      } catch (err) {
        replaceTodos((items) =>
          items.map((item) => (item.id === todo.id ? { ...item, is_done: !next } : item)),
        );
        showToast(describeUserError(err, 'Could not update the to-do.'));
      }
    },
    [replaceTodos, showToast],
  );

  const saveEdit = useCallback(
    async (update: EditUpdate) => {
      const target = editTarget;
      const token = getSessionToken();
      if (!target || !token) return;
      if (target.category === 'todos') {
        const todo = await updateTodo(token, target.id, { text: update.text });
        replaceTodos((items) => items.map((item) => (item.id === todo.id ? todo : item)));
      } else if (target.category === 'reminders') {
        const reminder = await updateReminder(token, target.id, {
          text: update.text,
          remind_at: update.remindAt ?? null,
          important: update.important,
        });
        replaceReminders((items) =>
          items.map((item) => (item.id === reminder.id ? reminder : item)),
        );
      } else {
        const insight = await updateInsight(token, target.id, update.text);
        replaceInsights((items) => items.map((item) => (item.id === insight.id ? insight : item)));
      }
      setEditTarget(null);
      showToast('Saved');
    },
    [editTarget, replaceTodos, replaceReminders, replaceInsights, showToast],
  );

  const confirmDelete = useCallback(async () => {
    const target = pendingDelete;
    const token = getSessionToken();
    if (!target || !token) return;
    if (target.category === 'todos') {
      await deleteTodo(token, target.id);
      reloadTodos();
    } else if (target.category === 'reminders') {
      await deleteReminder(token, target.id);
      reloadReminders();
    } else {
      await deleteInsight(token, target.id);
      reloadInsights();
    }
    setPendingDelete(null);
    showToast('Deleted');
  }, [pendingDelete, reloadTodos, reloadReminders, reloadInsights, showToast]);

  const editTodo = useCallback(
    (todo: Todo) => setEditTarget({ category: 'todos', id: todo.id, text: todo.text }),
    [],
  );
  const editReminder = useCallback(
    (reminder: Reminder) =>
      setEditTarget({
        category: 'reminders',
        id: reminder.id,
        text: reminder.text,
        remindAt: reminder.remind_at,
        important: reminder.important,
      }),
    [],
  );
  const editInsight = useCallback(
    (insight: Insight) =>
      setEditTarget({ category: 'insights', id: insight.id, text: insight.text }),
    [],
  );

  return {
    pendingDelete,
    setPendingDelete,
    editTarget,
    setEditTarget,
    toggleTodo,
    saveEdit,
    confirmDelete,
    editTodo,
    editReminder,
    editInsight,
    reloadTodos,
    reloadReminders,
    reloadInsights,
  };
}

function useNotesFeeds(todoStatus: TodoStatus, reminderWindow: ReminderWindow) {
  const todos = usePagedItems(
    `todos:${todoStatus}`,
    useCallback(
      (offset: number) => {
        const token = getSessionToken();
        if (!token) throw new Error('No session');
        return listTodos(token, todoStatus, offset);
      },
      [todoStatus],
    ),
  );
  const reminders = usePagedItems(
    `reminders:${reminderWindow}`,
    useCallback(
      (offset: number) => {
        const token = getSessionToken();
        if (!token) throw new Error('No session');
        return listReminders(token, reminderWindow, offset);
      },
      [reminderWindow],
    ),
  );
  const insights = usePagedItems(
    'insights',
    useCallback((offset: number) => {
      const token = getSessionToken();
      if (!token) throw new Error('No session');
      return listInsights(token, offset);
    }, []),
  );
  return { todos, reminders, insights };
}

export function NotesScreen() {
  const { showToast } = useToast();
  const [category, setCategory] = useState<Category>('todos');
  const [todoStatus, setTodoStatus] = useState<TodoStatus>('all');
  const [reminderWindow, setReminderWindow] = useState<ReminderWindow>('upcoming');

  const {
    todos: todosFeed,
    reminders: remindersFeed,
    insights: insightsFeed,
  } = useNotesFeeds(todoStatus, reminderWindow);

  const {
    pendingDelete,
    setPendingDelete,
    editTarget,
    setEditTarget,
    toggleTodo,
    saveEdit,
    confirmDelete,
    editTodo,
    editReminder,
    editInsight,
    reloadTodos,
    reloadReminders,
    reloadInsights,
  } = useNotesActions({
    todos: todosFeed,
    reminders: remindersFeed,
    insights: insightsFeed,
    showToast,
  });

  const reloadActive =
    category === 'todos'
      ? reloadTodos
      : category === 'reminders'
        ? reloadReminders
        : reloadInsights;
  useFocusEffect(
    useCallback(() => {
      reloadActive();
    }, [reloadActive]),
  );

  const activeFeed =
    category === 'todos' ? todosFeed : category === 'reminders' ? remindersFeed : insightsFeed;
  const activeFilter =
    category === 'todos' ? todoStatus : category === 'reminders' ? reminderWindow : 'all';
  const subtitle = activeFeed.loading
    ? 'Todos, reminders & insights'
    : summaryLabel(category, activeFeed.total, activeFilter);

  const featured = insightsFeed.items[0];
  const featuredCard = featured ? (
    <FeaturedInsight
      insight={featured}
      onEdit={() => editInsight(featured)}
      onDelete={() =>
        setPendingDelete({ category: 'insights', id: featured.id, text: featured.text })
      }
    />
  ) : null;

  return (
    <Screen className="px-6">
      <AppHeader title="Notes" subtitle={subtitle} />
      <View className="gap-2 pb-3 pt-1">
        <FilterPills options={CATEGORIES} selected={category} onSelect={setCategory} />
        <SubFilters
          category={category}
          todoStatus={todoStatus}
          onTodoStatus={setTodoStatus}
          reminderWindow={reminderWindow}
          onReminderWindow={setReminderWindow}
        />
      </View>

      {category === 'todos' ? (
        <TodoList
          feed={todosFeed}
          onToggle={(todo) => void toggleTodo(todo)}
          onEdit={editTodo}
          onDelete={(todo) => setPendingDelete({ category: 'todos', id: todo.id, text: todo.text })}
        />
      ) : category === 'reminders' ? (
        <SectionedList
          feed={remindersFeed}
          category="reminders"
          renderItem={(reminder) => (
            <ReminderRow
              reminder={reminder}
              onEdit={() => editReminder(reminder)}
              onDelete={() =>
                setPendingDelete({
                  category: 'reminders',
                  id: reminder.id,
                  text: reminder.text,
                })
              }
            />
          )}
        />
      ) : (
        <SectionedList
          feed={insightsFeed}
          category="insights"
          skip={featured ? 1 : 0}
          header={featuredCard}
          renderItem={(insight) => (
            <InsightRow
              insight={insight}
              onEdit={() => editInsight(insight)}
              onDelete={() =>
                setPendingDelete({ category: 'insights', id: insight.id, text: insight.text })
              }
            />
          )}
        />
      )}

      <ConfirmDialog
        visible={pendingDelete !== null}
        title="Delete this item?"
        body={pendingDelete ? `"${pendingDelete.text}" will be removed from your notes.` : ''}
        confirmLabel="Delete"
        onCancel={() => setPendingDelete(null)}
        onConfirm={confirmDelete}
      />

      {editTarget ? (
        <ItemEditSheet
          key={`${editTarget.category}:${editTarget.id}`}
          target={editTarget}
          onCancel={() => setEditTarget(null)}
          onSave={saveEdit}
        />
      ) : null}
    </Screen>
  );
}
