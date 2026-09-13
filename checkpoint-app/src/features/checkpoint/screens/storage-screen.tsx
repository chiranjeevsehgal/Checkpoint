import { useFocusEffect, useRouter } from 'expo-router';
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
import { Section } from '@/components/shared/section';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
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
    <Card className="flex-row items-center justify-between gap-2.5 p-3">
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
            className="active:bg-foreground/10 h-11 w-11 items-center justify-center border border-border"
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
          className="active:bg-foreground/10 h-11 w-11 items-center justify-center border border-border"
        >
          {deleting ? (
            <ActivityIndicator size="small" />
          ) : (
            <Icon as={Trash2} size={14} className="text-destructive" />
          )}
        </Pressable>
      </View>
    </Card>
  );
}

function StorageHeader({
  storage,
  pct,
  free,
  pageLabel,
  connected,
  onRefresh,
}: {
  storage: { used: number; total: number; files: number; pending: number } | null;
  pct: number;
  free: number;
  pageLabel: string;
  connected: boolean;
  onRefresh: () => void;
}) {
  return (
    <View className="gap-3">
      <Section title="Pendant storage">
        <Card>
          <View className="flex-row items-center justify-between gap-2">
            <Text className="font-display text-[15px]">
              {storage
                ? `${formatBytes(storage.used)} used of ${formatBytes(storage.total)}`
                : 'No storage info yet.'}
            </Text>
            <RefreshButton label="Refresh file list" onPress={onRefresh} disabled={!connected} />
          </View>
          <ProgressBar value={pct / 100} className="h-1.5" />
          <Text variant="muted" className="text-[11px]">
            {storage ? `${storage.files} files · ${storage.pending} pending` : '—'}
          </Text>
          {storage ? (
            <Text variant="muted" className="text-[11px]">
              {formatBytes(free)} available
            </Text>
          ) : null}
        </Card>
      </Section>
      <View className="flex-row items-baseline justify-between">
        <Text variant="kicker">Device files</Text>
        <Text variant="muted" className="text-[11px]">
          {pageLabel}
        </Text>
      </View>
    </View>
  );
}

function Pagination({
  page,
  pages,
  canPrev,
  canNext,
  onPrev,
  onNext,
}: {
  page: number;
  pages: number;
  canPrev: boolean;
  canNext: boolean;
  onPrev: () => void;
  onNext: () => void;
}) {
  return (
    <View className="flex-row items-center justify-between gap-2">
      <Button variant="ghost" size="sm" disabled={!canPrev} onPress={onPrev}>
        <Text>‹ Previous</Text>
      </Button>
      <Text variant="muted" className="text-[11px]">
        Page {page} of {pages}
      </Text>
      <Button variant="ghost" size="sm" disabled={!canNext} onPress={onNext}>
        <Text>Next ›</Text>
      </Button>
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
    preview,
    previewStorageFile,
    deleting,
    erasing,
  } = useCheckpoint();
  const { label: playingLabel, playing, paused } = usePlayback();
  const router = useRouter();

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
  const free = storage ? Math.max(0, storage.total - storage.used) : 0;
  const { refreshing, onRefresh } = useRefresh(refreshStorage);
  const pageLabel =
    listPage.total > 0
      ? `${listPage.start + 1}–${listPage.start + listPage.count} of ${listPage.total}`
      : '—';
  const canPrev = listPage.start > 0;
  const canNext = listPage.start + listPage.count < listPage.total;
  const count = Math.max(1, listPage.count);
  const page = Math.floor(listPage.start / count) + 1;
  const pages = Math.max(1, Math.ceil(listPage.total / count));
  const showPagination = listPage.count > 0 && listPage.total > listPage.count;
  const files = fileList?.entries ?? [];

  return (
    <CheckpointScreen>
      <AppHeader title="Storage" subtitle="Pendant SD card" />
      <FlatList
        className="flex-1"
        data={files}
        keyExtractor={(item) => item.name}
        refreshControl={<AppRefreshControl refreshing={refreshing} onRefresh={onRefresh} />}
        contentContainerStyle={{ gap: 12, paddingBottom: 24 }}
        showsVerticalScrollIndicator={false}
        ListHeaderComponent={
          connected ? (
            <StorageHeader
              storage={storage}
              pct={pct}
              free={free}
              pageLabel={pageLabel}
              connected={connected}
              onRefresh={() => void refreshStorage()}
            />
          ) : null
        }
        renderItem={({ item }) => {
          const fetching = preview?.path === item.name;
          const progress =
            fetching && preview.totalFrags > 0 ? preview.received / preview.totalFrags : 0;
          const isCurrent = playingLabel === item.name;
          const isPlaying = isCurrent && playing;
          const isPaused = isCurrent && paused;
          return (
            <FileRow
              item={item}
              onDelete={() => requestDelete(item.name)}
              onPlay={() => {
                if (isPlaying) playback.pause();
                else if (isPaused) playback.resume();
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
          !connected ? (
            <EmptyState
              title="Not connected"
              hint="Connect to a pendant to browse its recordings."
              action={
                <Button variant="outline" onPress={() => router.navigate('/(app)/(tabs)/connect')}>
                  <Text>Go to Connect</Text>
                </Button>
              }
            />
          ) : fileList === null ? (
            <Text variant="muted" className="py-12 text-center">
              Reading storage…
            </Text>
          ) : (
            <EmptyState
              title="No recordings found"
              hint="Refresh the file list to read recordings currently stored on the pendant."
              action={
                <Button variant="outline" onPress={() => void refreshStorage()}>
                  <Text>Refresh</Text>
                </Button>
              }
            />
          )
        }
        ListFooterComponent={
          showPagination ? (
            <Pagination
              page={page}
              pages={pages}
              canPrev={canPrev}
              canNext={canNext}
              onPrev={() => void listPrev()}
              onNext={() => void listNext()}
            />
          ) : null
        }
      />
    </CheckpointScreen>
  );
}
