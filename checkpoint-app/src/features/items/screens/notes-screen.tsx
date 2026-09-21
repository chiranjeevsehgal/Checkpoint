import { useFocusEffect } from 'expo-router';
import { Check, Trash2 } from 'lucide-react-native';
import { useCallback, useState, type ReactElement } from 'react';
import { FlatList, Pressable, View } from 'react-native';

import { AppHeader } from '@/components/shared/app-header';
import { ConfirmDialog } from '@/components/shared/confirm-dialog';
import { EmptyState } from '@/components/shared/empty-state';
import { Screen } from '@/components/shared/screen';
import { Card } from '@/components/ui/card';
import { Icon } from '@/components/ui/icon';
import { AppRefreshControl } from '@/components/ui/refresh-control';
import { Text } from '@/components/ui/text';
import {
  CATEGORIES,
  emptyMessage,
  formatItemDate,
  formatReminderTime,
  itemDate,
  REMINDER_WINDOWS,
  TODO_STATUSES,
  type Category,
} from '@/features/items/itemsView';
import { usePagedItems, type PagedItems } from '@/features/items/usePagedItems';
import {
  deleteInsight,
  deleteReminder,
  deleteTodo,
  listInsights,
  listReminders,
  listTodos,
  setTodoDone,
  type Insight,
  type Reminder,
  type ReminderWindow,
  type Todo,
  type TodoStatus,
} from '@/lib/api/items-api';
import { getSessionToken } from '@/lib/session';
import { describeUserError } from '@/lib/user-errors';
import { cn } from '@/lib/utils';
import { useToast } from '@/providers/toast-provider';

type PendingDelete = { category: Category; id: number; text: string };

function FilterPills<T extends string>({
  options,
  selected,
  onSelect,
}: {
  options: { key: T; label: string }[];
  selected: T;
  onSelect: (key: T) => void;
}) {
  return (
    <View className="flex-row flex-wrap gap-1.5">
      {options.map((option) => (
        <Pressable
          key={option.key}
          onPress={() => onSelect(option.key)}
          accessibilityRole="button"
          accessibilityState={{ selected: selected === option.key }}
          className={cn(
            'border px-2.5 py-1 active:opacity-80',
            selected === option.key ? 'border-primary bg-primary' : 'border-border',
          )}
        >
          <Text
            className={cn(
              'text-[11px]',
              selected === option.key ? 'text-primary-foreground' : 'text-muted-foreground',
            )}
          >
            {option.label}
          </Text>
        </Pressable>
      ))}
    </View>
  );
}

function SubFilters({
  category,
  todoStatus,
  onTodoStatus,
  reminderWindow,
  onReminderWindow,
}: {
  category: Category;
  todoStatus: TodoStatus;
  onTodoStatus: (status: TodoStatus) => void;
  reminderWindow: ReminderWindow;
  onReminderWindow: (window: ReminderWindow) => void;
}) {
  if (category === 'todos') {
    return <FilterPills options={TODO_STATUSES} selected={todoStatus} onSelect={onTodoStatus} />;
  }
  if (category === 'reminders') {
    return (
      <FilterPills
        options={REMINDER_WINDOWS}
        selected={reminderWindow}
        onSelect={onReminderWindow}
      />
    );
  }
  return null;
}

function DeleteButton({ label, onPress }: { label: string; onPress: () => void }) {
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={label}
      className="h-8 w-8 items-center justify-center active:opacity-70"
    >
      <Icon as={Trash2} size={14} className="text-destructive" />
    </Pressable>
  );
}

function ItemList<T extends { id: number }>({
  feed,
  emptyTitle,
  emptyHint,
  renderItem,
}: {
  feed: PagedItems<T>;
  emptyTitle: string;
  emptyHint: string;
  renderItem: (item: T) => ReactElement;
}) {
  return (
    <FlatList
      className="flex-1"
      data={feed.items}
      keyExtractor={(item) => String(item.id)}
      refreshControl={<AppRefreshControl refreshing={feed.refreshing} onRefresh={feed.refresh} />}
      contentContainerStyle={{ gap: 12, paddingBottom: 24 }}
      showsVerticalScrollIndicator={false}
      onEndReached={feed.loadMore}
      onEndReachedThreshold={0.4}
      ListEmptyComponent={
        feed.loading ? (
          <Text variant="muted" className="py-12 text-center">
            Loading…
          </Text>
        ) : (
          <EmptyState title={emptyTitle} hint={emptyHint} />
        )
      }
      ListFooterComponent={
        feed.error ? <Text className="py-2 text-center text-destructive">{feed.error}</Text> : null
      }
      renderItem={({ item }) => renderItem(item)}
    />
  );
}

function TodoRow({
  todo,
  onToggle,
  onDelete,
}: {
  todo: Todo;
  onToggle: () => void;
  onDelete: () => void;
}) {
  return (
    <Card>
      <View className="flex-row items-start gap-3">
        <Pressable
          onPress={onToggle}
          accessibilityRole="checkbox"
          accessibilityState={{ checked: todo.is_done }}
          accessibilityLabel={todo.is_done ? 'Mark as not done' : 'Mark as done'}
          className={cn(
            'mt-0.5 h-5 w-5 items-center justify-center border',
            todo.is_done ? 'border-primary bg-primary' : 'border-border',
          )}
        >
          {todo.is_done ? <Icon as={Check} size={13} className="text-primary-foreground" /> : null}
        </Pressable>
        <View className="flex-1 gap-0.5">
          <Text className={cn('text-[14px]', todo.is_done && 'text-muted-foreground line-through')}>
            {todo.text}
          </Text>
          <Text variant="muted" className="text-[11px]">
            {formatItemDate(itemDate(todo))}
          </Text>
        </View>
        <DeleteButton label="Delete to-do" onPress={onDelete} />
      </View>
    </Card>
  );
}

function ReminderRow({ reminder, onDelete }: { reminder: Reminder; onDelete: () => void }) {
  return (
    <Card>
      <View className="flex-row items-start gap-3">
        <View className="flex-1 gap-0.5">
          <View className="flex-row items-center gap-2">
            <Text className="flex-1 text-[14px]">{reminder.text}</Text>
            {reminder.important ? (
              <Text className="text-[10px] text-warning">Important</Text>
            ) : null}
          </View>
          <Text variant="muted" className="text-[11px]">
            {formatReminderTime(reminder.remind_at)}
          </Text>
        </View>
        <DeleteButton label="Delete reminder" onPress={onDelete} />
      </View>
    </Card>
  );
}

function InsightRow({ insight, onDelete }: { insight: Insight; onDelete: () => void }) {
  return (
    <Card>
      <View className="flex-row items-start gap-3">
        <View className="flex-1 gap-0.5">
          <Text className="text-[14px]">{insight.text}</Text>
          <Text variant="muted" className="text-[11px]">
            {formatItemDate(insight.created_at)}
          </Text>
        </View>
        <DeleteButton label="Delete insight" onPress={onDelete} />
      </View>
    </Card>
  );
}

export function NotesScreen() {
  const { showToast } = useToast();
  const [category, setCategory] = useState<Category>('todos');
  const [todoStatus, setTodoStatus] = useState<TodoStatus>('all');
  const [reminderWindow, setReminderWindow] = useState<ReminderWindow>('upcoming');
  const [pendingDelete, setPendingDelete] = useState<PendingDelete | null>(null);

  const todosFeed = usePagedItems(
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
  const remindersFeed = usePagedItems(
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
  const insightsFeed = usePagedItems(
    'insights',
    useCallback((offset: number) => {
      const token = getSessionToken();
      if (!token) throw new Error('No session');
      return listInsights(token, offset);
    }, []),
  );

  const { replaceItems, reload: reloadTodos } = todosFeed;
  const reloadReminders = remindersFeed.reload;
  const reloadInsights = insightsFeed.reload;
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

  const toggleTodo = useCallback(
    async (todo: Todo) => {
      const token = getSessionToken();
      if (!token) return;
      const next = !todo.is_done;
      replaceItems((items) =>
        items.map((item) => (item.id === todo.id ? { ...item, is_done: next } : item)),
      );
      try {
        await setTodoDone(token, todo.id, next);
      } catch (err) {
        replaceItems((items) =>
          items.map((item) => (item.id === todo.id ? { ...item, is_done: !next } : item)),
        );
        showToast(describeUserError(err, 'Could not update the to-do.'));
      }
    },
    [replaceItems, showToast],
  );

  const confirmDelete = useCallback(async () => {
    const target = pendingDelete;
    const token = getSessionToken();
    if (!target || !token) return;
    if (target.category === 'todos') await deleteTodo(token, target.id);
    else if (target.category === 'reminders') await deleteReminder(token, target.id);
    else await deleteInsight(token, target.id);
    setPendingDelete(null);
    showToast('Deleted');
    reloadActive();
  }, [pendingDelete, reloadActive, showToast]);

  return (
    <Screen className="px-6">
      <AppHeader title="Notes" subtitle="Todos, reminders & insights" />
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
        <ItemList
          feed={todosFeed}
          emptyTitle="No to-dos"
          emptyHint={emptyMessage('todos')}
          renderItem={(todo) => (
            <TodoRow
              todo={todo}
              onToggle={() => void toggleTodo(todo)}
              onDelete={() => setPendingDelete({ category: 'todos', id: todo.id, text: todo.text })}
            />
          )}
        />
      ) : category === 'reminders' ? (
        <ItemList
          feed={remindersFeed}
          emptyTitle="No reminders"
          emptyHint={emptyMessage('reminders')}
          renderItem={(reminder) => (
            <ReminderRow
              reminder={reminder}
              onDelete={() =>
                setPendingDelete({ category: 'reminders', id: reminder.id, text: reminder.text })
              }
            />
          )}
        />
      ) : (
        <ItemList
          feed={insightsFeed}
          emptyTitle="No insights"
          emptyHint={emptyMessage('insights')}
          renderItem={(insight) => (
            <InsightRow
              insight={insight}
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
    </Screen>
  );
}
