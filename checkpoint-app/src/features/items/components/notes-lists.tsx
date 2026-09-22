import { Bell, Check, ListChecks, Pencil, Sparkles, type LucideIcon } from 'lucide-react-native';
import type { ReactElement, ReactNode } from 'react';
import { FlatList, Pressable, SectionList, View } from 'react-native';

import { EmptyState } from '@/components/shared/empty-state';
import { Card } from '@/components/ui/card';
import { Icon } from '@/components/ui/icon';
import { AppRefreshControl } from '@/components/ui/refresh-control';
import { Text } from '@/components/ui/text';
import { SwipeToDelete } from '@/features/items/components/swipe-to-delete';
import {
  CATEGORY_ACCENT,
  emptyMessage,
  emptyTitle,
  formatItemDate,
  formatReminderTime,
  groupByDay,
  groupDate,
  itemDate,
  REMINDER_WINDOWS,
  TODO_STATUSES,
  type Category,
  type DaySection,
  type Item,
} from '@/features/items/itemsView';
import type { PagedItems } from '@/features/items/usePagedItems';
import type { Insight, Reminder, ReminderWindow, Todo, TodoStatus } from '@/lib/api/items-api';
import { cn } from '@/lib/utils';

const CATEGORY_ICON: Record<Category, LucideIcon> = {
  todos: ListChecks,
  reminders: Bell,
  insights: Sparkles,
};

export function FilterPills<T extends string>({
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

export function SubFilters({
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

function RowCard({ category, children }: { category: Category; children: ReactNode }) {
  return <Card className={cn('border-l-2', CATEGORY_ACCENT[category])}>{children}</Card>;
}

function RowAction({
  icon,
  label,
  onPress,
}: {
  icon: LucideIcon;
  label: string;
  onPress: () => void;
}) {
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={label}
      className="h-8 w-8 items-center justify-center active:opacity-70"
    >
      <Icon as={icon} size={14} />
    </Pressable>
  );
}

export function TodoRow({
  todo,
  onToggle,
  onEdit,
  onDelete,
}: {
  todo: Todo;
  onToggle: () => void;
  onEdit: () => void;
  onDelete: () => void;
}) {
  return (
    <SwipeToDelete onDelete={onDelete} label="Delete to-do">
      <RowCard category="todos">
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
            {todo.is_done ? (
              <Icon as={Check} size={13} className="text-primary-foreground" />
            ) : null}
          </Pressable>
          <View className="flex-1 gap-0.5">
            <Text
              className={cn('text-[14px]', todo.is_done && 'text-muted-foreground line-through')}
            >
              {todo.text}
            </Text>
            <Text variant="muted" className="text-[11px]">
              {formatItemDate(itemDate(todo))}
            </Text>
          </View>
          <RowAction icon={Pencil} label="Edit to-do" onPress={onEdit} />
        </View>
      </RowCard>
    </SwipeToDelete>
  );
}

export function ReminderRow({
  reminder,
  onEdit,
  onDelete,
}: {
  reminder: Reminder;
  onEdit: () => void;
  onDelete: () => void;
}) {
  return (
    <SwipeToDelete onDelete={onDelete} label="Delete reminder">
      <RowCard category="reminders">
        <View className="flex-row items-start gap-3">
          <Icon as={Bell} size={16} className="mt-0.5 text-warning" />
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
          <RowAction icon={Pencil} label="Edit reminder" onPress={onEdit} />
        </View>
      </RowCard>
    </SwipeToDelete>
  );
}

export function InsightRow({
  insight,
  onEdit,
  onDelete,
}: {
  insight: Insight;
  onEdit: () => void;
  onDelete: () => void;
}) {
  return (
    <SwipeToDelete onDelete={onDelete} label="Delete insight">
      <RowCard category="insights">
        <View className="flex-row items-start gap-2">
          <Text className="font-display text-[20px] leading-none text-success">“</Text>
          <View className="flex-1 gap-0.5">
            <Text className="text-[14px] italic">{insight.text}</Text>
            <Text variant="muted" className="text-[11px]">
              {formatItemDate(insight.created_at)}
            </Text>
          </View>
          <RowAction icon={Pencil} label="Edit insight" onPress={onEdit} />
        </View>
      </RowCard>
    </SwipeToDelete>
  );
}

export function FeaturedInsight({
  insight,
  onEdit,
  onDelete,
}: {
  insight: Insight;
  onEdit: () => void;
  onDelete: () => void;
}) {
  return (
    <View className="mb-3">
      <SwipeToDelete onDelete={onDelete} label="Delete insight">
        <Card className={cn('border-l-2', CATEGORY_ACCENT.insights)}>
          <Text variant="kicker">Latest insight</Text>
          <Text className="font-display text-[17px] italic leading-tight">{insight.text}</Text>
          <View className="flex-row items-center justify-between gap-2">
            <Text variant="muted" className="text-[11px]">
              {formatItemDate(insight.created_at)}
            </Text>
            <RowAction icon={Pencil} label="Edit insight" onPress={onEdit} />
          </View>
        </Card>
      </SwipeToDelete>
    </View>
  );
}

function EmptyList({
  category,
  loading,
  count,
}: {
  category: Category;
  loading: boolean;
  count: number;
}) {
  if (loading) {
    return (
      <Text variant="muted" className="py-12 text-center">
        Loading…
      </Text>
    );
  }
  if (count > 0) return null;
  return (
    <EmptyState
      icon={CATEGORY_ICON[category]}
      title={emptyTitle(category)}
      hint={emptyMessage(category)}
    />
  );
}

function ListError({ error }: { error: string | null }) {
  return error ? <Text className="py-2 text-center text-destructive">{error}</Text> : null;
}

export function TodoList({
  feed,
  onToggle,
  onEdit,
  onDelete,
}: {
  feed: PagedItems<Todo>;
  onToggle: (todo: Todo) => void;
  onEdit: (todo: Todo) => void;
  onDelete: (todo: Todo) => void;
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
        <EmptyList category="todos" loading={feed.loading} count={feed.items.length} />
      }
      ListFooterComponent={<ListError error={feed.error} />}
      renderItem={({ item }) => (
        <TodoRow
          todo={item}
          onToggle={() => onToggle(item)}
          onEdit={() => onEdit(item)}
          onDelete={() => onDelete(item)}
        />
      )}
    />
  );
}

export function SectionedList<T extends Item>({
  feed,
  category,
  skip = 0,
  header,
  renderItem,
}: {
  feed: PagedItems<T>;
  category: Category;
  skip?: number;
  header?: ReactElement | null;
  renderItem: (item: T) => ReactElement;
}) {
  const items = skip ? feed.items.slice(skip) : feed.items;
  const sections = groupByDay(items, groupDate);
  return (
    <SectionList<T, DaySection<T>>
      className="flex-1"
      sections={sections}
      keyExtractor={(item) => String(item.id)}
      stickySectionHeadersEnabled={false}
      refreshControl={<AppRefreshControl refreshing={feed.refreshing} onRefresh={feed.refresh} />}
      contentContainerStyle={{ gap: 12, paddingBottom: 24 }}
      showsVerticalScrollIndicator={false}
      onEndReached={feed.loadMore}
      onEndReachedThreshold={0.4}
      ListHeaderComponent={header}
      renderSectionHeader={({ section }) => (
        <Text variant="kicker" className="pt-1">
          {section.title}
        </Text>
      )}
      renderItem={({ item }) => renderItem(item)}
      ListEmptyComponent={
        <EmptyList category={category} loading={feed.loading} count={feed.items.length} />
      }
      ListFooterComponent={<ListError error={feed.error} />}
    />
  );
}
