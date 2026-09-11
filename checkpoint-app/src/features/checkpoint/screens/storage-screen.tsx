import { Trash2 } from 'lucide-react-native';
import { FlatList, Pressable, View } from 'react-native';

import { AppHeader } from '@/components/shared/app-header';
import { EmptyState } from '@/components/shared/empty-state';
import { Screen } from '@/components/shared/screen';
import { Button } from '@/components/ui/button';
import { Card, CardKicker } from '@/components/ui/card';
import { Icon } from '@/components/ui/icon';
import { ProgressBar } from '@/components/ui/progress-bar';
import { AppRefreshControl } from '@/components/ui/refresh-control';
import { Text } from '@/components/ui/text';
import { cn } from '@/lib/utils';
import { useRefresh } from '@/lib/use-refresh';

import { fileStateLabel, formatBytes } from '../parsers.ts';
import { useCheckpoint } from '../hooks/useCheckpoint.tsx';
import type { DeviceFileEntry } from '../types.ts';

const STATE_TAG: Record<string, string> = {
  recording: 'bg-primary',
  pending: 'bg-neutral-400',
  synced: 'bg-neutral-300 dark:bg-neutral-700',
};

const STATE_TAG_TEXT: Record<string, string> = {
  recording: 'text-primary-foreground',
  pending: 'text-background',
  synced: 'text-foreground',
};

function FileRow({ item, onDelete }: { item: DeviceFileEntry; onDelete: () => void }) {
  const state = fileStateLabel(item.flags);

  return (
    <View className="bg-surface flex-row items-center justify-between gap-2.5 p-3">
      <View className="min-w-0 flex-1">
        <Text className="font-mono text-[13px]">{item.name}</Text>
        <View className="mt-0.5 flex-row items-center gap-1.5">
          <Text variant="muted" className="text-[11px]">
            {formatBytes(item.size)}
          </Text>
          <Text className={cn('px-1.5 py-px text-[10.5px] capitalize', STATE_TAG[state], STATE_TAG_TEXT[state])}>
            {state}
          </Text>
        </View>
      </View>
      <Pressable
        onPress={onDelete}
        accessibilityRole="button"
        accessibilityLabel={`Delete ${item.name}`}
        className="border-border active:bg-foreground/10 border p-1.5"
      >
        <Icon as={Trash2} size={14} className="text-primary" />
      </Pressable>
    </View>
  );
}

export function StorageScreen() {
  const {
    connected,
    storage,
    fileList,
    listPage,
    refreshStorage,
    listPrev,
    listNext,
    requestDelete,
    requestErase,
  } = useCheckpoint();

  const pct =
    storage && storage.total > 0
      ? Math.min(100, Math.round((100 * storage.used) / storage.total))
      : 0;
  const { refreshing, onRefresh } = useRefresh(refreshStorage);
  const pageLabel =
    listPage.total > 0
      ? `${listPage.start + 1}–${listPage.start + listPage.count} of ${listPage.total}`
      : '—';
  const canPrev = listPage.start > 0;
  const canNext = listPage.start + listPage.count < listPage.total;
  const files = fileList?.entries ?? [];

  return (
    <Screen>
      <AppHeader title="Storage" subtitle="Pendant SD card" />
      <FlatList
        className="flex-1"
        data={files}
        keyExtractor={(item) => item.name}
        refreshControl={<AppRefreshControl refreshing={refreshing} onRefresh={onRefresh} />}
        contentContainerStyle={{ gap: 14, paddingBottom: 24 }}
        showsVerticalScrollIndicator={false}
        ItemSeparatorComponent={() => <View className="bg-divider h-px" />}
        ListHeaderComponent={
          <View className="gap-3.5">
            <Card>
              <View className="flex-row items-baseline justify-between">
                <Text className="font-display text-[15px]">
                  {storage ? `SD: ${formatBytes(storage.used)} / ${formatBytes(storage.total)}` : 'SD: —'}
                </Text>
                <Pressable
                  onPress={() => void refreshStorage()}
                  disabled={!connected}
                  accessibilityRole="button"
                  className="active:opacity-60"
                >
                  <Text variant="muted" className="text-[11px]">
                    Refresh
                  </Text>
                </Pressable>
              </View>
              <ProgressBar value={pct / 100} className="h-1.5" />
              <Text variant="muted" className="text-[11.5px]">
                {storage ? `${storage.files} files · ${storage.pending} pending` : 'No storage info yet.'}
              </Text>
            </Card>
            <View className="flex-row items-baseline justify-between">
              <CardKicker>Device files</CardKicker>
              <Text variant="muted" className="text-[11px]">
                {pageLabel}
              </Text>
            </View>
          </View>
        }
        renderItem={({ item }) => (
          <FileRow item={item} onDelete={() => requestDelete(item.name)} />
        )}
        ListEmptyComponent={
          <EmptyState title="No files listed" hint="Refresh to load the pendant file list." />
        }
        ListFooterComponent={
          <View className="gap-2">
            <View className="flex-row gap-2">
              <Button variant="outline" className="flex-1" disabled={!canPrev} onPress={() => void listPrev()}>
                <Text>‹ Prev</Text>
              </Button>
              <Button variant="outline" className="flex-1" disabled={!canNext} onPress={() => void listNext()}>
                <Text>Next ›</Text>
              </Button>
            </View>
            <Button variant="outline" className="border-primary" disabled={!connected} onPress={requestErase}>
              <Text className="text-primary">Erase all…</Text>
            </Button>
          </View>
        }
      />
    </Screen>
  );
}
