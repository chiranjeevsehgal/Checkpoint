import { useFocusEffect } from 'expo-router';
import { Pause, Play, Trash2 } from 'lucide-react-native';
import { useCallback, useEffect, useRef } from 'react';
import { ActivityIndicator, FlatList, Pressable, View } from 'react-native';

import { CheckpointScreen } from '../components/checkpoint-screen.tsx';
import { useCheckpoint } from '../hooks/useCheckpoint.tsx';
import { fileStateLabel, formatBytes } from '../parsers.ts';
import { playback, usePlayback } from '../playback.ts';
import type { DeviceFileEntry } from '../types.ts';

import { AppHeader } from '@/components/shared/app-header';
import { EmptyState } from '@/components/shared/empty-state';
import { RefreshButton } from '@/components/shared/refresh-button';
import { Button } from '@/components/ui/button';
import { Card, CardKicker } from '@/components/ui/card';
import { Icon } from '@/components/ui/icon';
import { ProgressBar } from '@/components/ui/progress-bar';
import { AppRefreshControl } from '@/components/ui/refresh-control';
import { Text } from '@/components/ui/text';
import { useRefresh } from '@/lib/use-refresh';
import { cn } from '@/lib/utils';

const STATE_TAG: Record<string, string> = {
  recording: 'bg-primary text-primary-foreground',
  pending: 'bg-warning text-background',
  synced: 'bg-success text-background',
};

const AUTO_REFRESH_MS = 5000;

function FileRow({
  item,
  onDelete,
  onPlay,
  disabled,
  playing,
  fetching,
  deleting,
  pct,
}: {
  item: DeviceFileEntry;
  onDelete: () => void;
  onPlay: () => void;
  disabled: boolean;
  playing: boolean;
  fetching: boolean;
  deleting: boolean;
  pct: number;
}) {
  const state = fileStateLabel(item.flags);
  const playable = state !== 'recording';

  return (
    <View className="flex-row items-center justify-between gap-2.5 bg-surface p-3">
      <View className="min-w-0 flex-1">
        <Text className="font-mono text-[13px]">{item.name}</Text>
        <View className="mt-0.5 flex-row items-center gap-1.5">
          <Text variant="muted" className="text-[11px]">
            {formatBytes(item.size)}
          </Text>
          <Text className={cn('px-1.5 py-px text-[10.5px] capitalize', STATE_TAG[state])}>
            {state}
          </Text>
        </View>
      </View>
      <View className="flex-row items-center gap-2">
        {playable ? (
          <Pressable
            onPress={onPlay}
            disabled={disabled || fetching || deleting}
            accessibilityRole="button"
            accessibilityLabel={playing ? `Pause ${item.name}` : `Play ${item.name}`}
            className="active:bg-foreground/10 border border-border p-1.5"
          >
            {fetching ? (
              <Text variant="muted" className="w-3.5 text-center text-[9px]">
                {pct > 0 ? `${Math.round(pct * 100)}` : '…'}
              </Text>
            ) : (
              <Icon as={playing ? Pause : Play} size={14} />
            )}
          </Pressable>
        ) : null}
        <Pressable
          onPress={onDelete}
          disabled={disabled || deleting}
          accessibilityRole="button"
          accessibilityLabel={`Delete ${item.name}`}
          className="active:bg-foreground/10 border border-border p-1.5"
        >
          {deleting ? (
            <ActivityIndicator size="small" />
          ) : (
            <Icon as={Trash2} size={14} className="text-destructive" />
          )}
        </Pressable>
      </View>
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
    preview,
    previewStorageFile,
    deleting,
    erasing,
  } = useCheckpoint();
  const { label: playingLabel, playing } = usePlayback();

  const busyRef = useRef(false);
  useEffect(() => {
    busyRef.current = deleting !== null || erasing || preview !== null;
  }, [deleting, erasing, preview]);
  useFocusEffect(
    useCallback(() => {
      if (!connected) return;
      void refreshStorage();
      const id = setInterval(() => {
        if (!busyRef.current) void refreshStorage();
      }, AUTO_REFRESH_MS);
      return () => clearInterval(id);
    }, [connected, refreshStorage]),
  );

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
    <CheckpointScreen>
      <AppHeader title="Storage" subtitle="Pendant SD card" />
      <FlatList
        className="flex-1"
        data={files}
        keyExtractor={(item) => item.name}
        refreshControl={<AppRefreshControl refreshing={refreshing} onRefresh={onRefresh} />}
        contentContainerStyle={{ gap: 14, paddingBottom: 24 }}
        showsVerticalScrollIndicator={false}
        ItemSeparatorComponent={() => <View className="h-px bg-divider" />}
        ListHeaderComponent={
          <View className="gap-3.5">
            <Card>
              <View className="flex-row items-baseline justify-between">
                <Text className="font-display text-[15px]">
                  {storage
                    ? `SD: ${formatBytes(storage.used)} / ${formatBytes(storage.total)}`
                    : 'SD: —'}
                </Text>
                <RefreshButton onPress={() => void refreshStorage()} disabled={!connected} />
              </View>
              <ProgressBar value={pct / 100} className="h-1.5" />
              <Text variant="muted" className="text-[11.5px]">
                {storage
                  ? `${storage.files} files · ${storage.pending} pending`
                  : 'No storage info yet.'}
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
        renderItem={({ item }) => {
          const fetching = preview?.path === item.name;
          const progress =
            fetching && preview.totalFrags > 0 ? preview.received / preview.totalFrags : 0;
          const isPlaying = playing && playingLabel === item.name;
          return (
            <FileRow
              item={item}
              onDelete={() => requestDelete(item.name)}
              onPlay={() => {
                if (isPlaying) playback.stop();
                else void previewStorageFile(item.name);
              }}
              disabled={!connected || erasing}
              playing={isPlaying}
              fetching={fetching}
              deleting={deleting === item.name}
              pct={progress}
            />
          );
        }}
        ListEmptyComponent={
          <EmptyState title="No files listed" hint="Refresh to load the pendant file list." />
        }
        ListFooterComponent={
          <View className="gap-2">
            <View className="flex-row gap-2">
              <Button
                variant="outline"
                className="flex-1"
                disabled={!canPrev}
                onPress={() => void listPrev()}
              >
                <Text>‹ Prev</Text>
              </Button>
              <Button
                variant="outline"
                className="flex-1"
                disabled={!canNext}
                onPress={() => void listNext()}
              >
                <Text>Next ›</Text>
              </Button>
            </View>
            <Button
              variant="outline"
              className="border-destructive"
              disabled={!connected || deleting !== null || erasing}
              onPress={requestErase}
            >
              {erasing ? (
                <ActivityIndicator size="small" />
              ) : (
                <Text className="text-destructive">Erase all…</Text>
              )}
            </Button>
          </View>
        }
      />
    </CheckpointScreen>
  );
}
